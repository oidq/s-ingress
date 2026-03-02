package controller

import (
	"context"
	"log/slog"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IngressController struct {
	k8sClient client.Client
	k8sState  *k8sState

	log *slog.Logger

	reconfigureChan            chan<- *proxy.RoutingConfig
	requestReconfigureChan     chan struct{}
	requestReconcileCommonChan chan struct{}

	eventRecorder events.EventRecorder

	envConfig config.ControllerEnvConf
}

func NewProxyController(log *slog.Logger, client client.Client, configuration config.ControllerEnvConf) *IngressController {
	return &IngressController{
		k8sState: &k8sState{
			controllerName:    configuration.ControllerName,
			controllerClasses: make(map[string]*netv1.IngressClass),
			ingresses:         make(map[types.NamespacedName]*IngressEntry),
		},
		k8sClient:                  client,
		requestReconfigureChan:     make(chan struct{}, 1),
		requestReconcileCommonChan: make(chan struct{}, 1),
		log:                        log,
		envConfig:                  configuration,
	}
}

func (ic *IngressController) SetReconfigureChan(c chan<- *proxy.RoutingConfig) {
	ic.reconfigureChan = c
}

func (ic *IngressController) RequestReconfigure() {
	select {
	case ic.requestReconfigureChan <- struct{}{}:
	default:

	}
}

func (ic *IngressController) RequestReconcileCommon() {
	select {
	case ic.requestReconcileCommonChan <- struct{}{}:
	default:

	}
}

func (ic *IngressController) RequestReconfigureWhenRelevant(relevant bool) {
	if relevant {
		ic.RequestReconfigure()
	}
}

func (ic *IngressController) Run(ctx context.Context) {
	ic.reconfigureControllerCommonConfig(ctx)

	go func() {
		ticker := time.NewTicker(10 * time.Second)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ic.requestReconcileCommonChan:
				ic.log.Info("reconcile common")
				ic.reconfigureControllerCommonConfig(ctx)
			case <-ic.requestReconfigureChan:
				ic.log.Info("reconfigure")
				conf, err := ic.getProxyConfig()
				if err != nil {
					ic.log.Error("failed to get proxy config", slog.String("err", err.Error()))
				}

				if ic.reconfigureChan != nil {
					ic.reconfigureChan <- conf
				}
			case <-ticker.C:
			}
		}
	}()
}

func (ic *IngressController) requestReconfigure() {
	select {
	case ic.requestReconfigureChan <- struct{}{}:
	default:
	}
}

func (ic *IngressController) reconfigureControllerCommonConfig(ctx context.Context) {
	err := ic.updateConfig(ctx)
	if err != nil {
		ic.log.Error("unable to update controller config", slog.String("error", err.Error()))
		// TODO: this is fatal on startup
	}

	err = ic.updateDefaultTls(ctx)
	if err != nil {
		ic.log.Error("unable to update default tls", slog.String("error", err.Error()))
	}

	err = ic.updateUpstreamIpAddress(ctx)
	if err != nil {
		ic.log.Error("unable to update upstream ip address", slog.String("error", err.Error()))
	}

	err = ic.updateTcpProxy(ctx)
	if err != nil {
		ic.log.Error("unable to update tcp proxies", slog.String("error", err.Error()))
	}

	ic.requestReconfigure()
}
