package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/controller"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	slogHandler := initSlogHandler()
	l := slog.New(slogHandler)

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		l.Error("POD_NAMESPACE environment variable not set")
		os.Exit(1)
	}

	controllerConfigMap := os.Getenv("CONTROLLER_CONFIGMAP")
	if controllerConfigMap == "" {
		l.Error("CONTROLLER_CONFIGMAP environment variable not set")
		os.Exit(1)
	}

	controllerName := os.Getenv("CONTROLLER_NAME")
	if controllerName == "" {
		controllerName = "oidq.dev/s-ingress"
	}

	reg := initPrometheus()

	mgr, err := initManager(slogHandler)
	if err != nil {
		l.Error("unable to initialize manager", slog.String("error", err.Error()))
		os.Exit(1)
	}

	p := proxy.NewProxy(l.With("event.source", "proxy"))
	p.WithMetrics(reg)

	c := controller.NewProxyController(
		l.With("event.source", "controller"),
		mgr.GetClient(),
		config.ControllerEnvConf{
			Namespace:           namespace,
			ControllerName:      controllerName,
			ControllerConfigMap: types.NamespacedName{Namespace: namespace, Name: controllerConfigMap},
		},
	)
	c.SetReconfigureChan(p.ConfigChan())

	ctx, cancel := context.WithCancel(context.Background())

	var sigChan = make(chan os.Signal, 1)
	var wg sync.WaitGroup
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	err = c.SetupWithManager(mgr, "s-ingress")
	if err != nil {
		l.Error("unable to setup controller", slog.String("error", err.Error()))
		os.Exit(1)
	}

	go func() {
		err = mgr.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	wg.Go(func() {
		c.Run(ctx)
	})

	wg.Go(func() {
		err := p.Start(ctx)
		if err != nil {
			l.Error("proxy failed", slog.String("error", err.Error()))
			cancel()
		}
	})

	<-sigChan
	cancel()
	wg.Wait()

	l.Info("shutdown complete")
}

func initSlogHandler() slog.Handler {
	opts := slog.HandlerOptions{
		AddSource: false,
		Level:     slog.Level(-1),
	}
	return slog.NewJSONHandler(os.Stderr, &opts)
}

func initPrometheus() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

func initManager(slogHandler slog.Handler) (manager.Manager, error) {
	logrLogger := logr.FromSlogHandler(slogHandler)
	log.SetLogger(
		logrLogger.WithValues("event.source", "controller"),
	)

	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	mgr, err := manager.New(conf, manager.Options{
		Metrics: server.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		return nil, err
	}

	return mgr, nil
}
