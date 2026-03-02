package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

const ShutdownPropagationTimeout = 5 * time.Second

type Proxy struct {
	// serverCtx is canceled on shutdown
	serverCtx context.Context

	// defaultCtx is used as a base for the request
	defaultCtx context.Context
	log        *slog.Logger

	configChan chan *RoutingConfig

	ready atomic.Bool

	config        config
	routingConfig atomic.Pointer[RoutingConfig]
}

type config struct {
	GracefulTimeout time.Duration
}

func NewProxy(log *slog.Logger) *Proxy {
	return &Proxy{
		log:        log,
		defaultCtx: context.Background(),
		config: config{
			GracefulTimeout: 5 * time.Second, // TODO: change
		},
		configChan: make(chan *RoutingConfig, 5),
	}
}

func (p *Proxy) ConfigChan() chan<- *RoutingConfig {
	return p.configChan
}

// SetInitialConfig can be used to initialize the proxy on startup. Calling
// this method on [Proxy] with initialized config will lead to panic.
//
// It is mainly intended for tests.
func (p *Proxy) SetInitialConfig(conf *RoutingConfig) {
	swapped := p.routingConfig.CompareAndSwap(nil, conf)
	if !swapped {
		panic("routing config already initialized")
	}
}

// IsConfiguredForHostname can be used for tests to verify that a proxy is configured
// for a given hostname.
func (p *Proxy) IsConfiguredForHostname(hostname string) bool {
	conf := p.routingConfig.Load()
	host := conf.LookupHost(hostname)
	return host != nil
}

func wrapErr(prefix string, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return nil
}

func (p *Proxy) StartDummy(ctx context.Context) error {
	startCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	p.serverCtx = startCtx

	p.ready.Store(true)

	var shutdownInitiated bool
	for {
		select {
		case <-ctx.Done(): // received a signal to shut down the server
			if shutdownInitiated {
				continue
			}
			p.log.Info("received shutdown signal, shutting down")
			return ctx.Err()
		case routingConfig := <-p.configChan: // configuration changed
			p.routingConfig.Store(routingConfig)
			p.log.Info("proxy reconfigured")
		}
	}
}

func (p *Proxy) waitForConfig(ctx context.Context) error {
	select {
	case routingConfig := <-p.configChan:
		p.routingConfig.Store(routingConfig)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Proxy) startListeners(startCtx context.Context, errChan chan<- error) (int, error) {
	var err error
	var listenerCount = 4
	cfg := p.routingConfig.Load()
	if cfg == nil {
		panic("routing config has not been initialized")
	}

	err = p.startHttps(startCtx, errChan)
	if err != nil {
		return 0, fmt.Errorf("start https: %w", err)
	}
	err = p.startHttp(startCtx, errChan)
	if err != nil {
		return 0, fmt.Errorf("start http: %w", err)
	}
	err = p.startQuic(startCtx, errChan)
	if err != nil {
		return 0, fmt.Errorf("start quic: %w", err)
	}
	err = p.startStatus(startCtx, errChan)
	if err != nil {
		return 0, fmt.Errorf("start status: %w", err)
	}

	for _, proxyConf := range cfg.TcpProxy {
		listenerCount++
		err = p.startTcpProxy(startCtx, errChan, proxyConf)
		if err != nil {
			return 0, fmt.Errorf("start tcp proxy on port %d: %w", proxyConf.Port, err)
		}
	}

	return listenerCount, nil
}

func (p *Proxy) Start(ctx context.Context) error {
	startCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	p.serverCtx = startCtx
	err := p.waitForConfig(ctx)
	if err != nil {
		return err
	}

	errChan := make(chan error)
	handlerCount, err := p.startListeners(startCtx, errChan)
	if err != nil {
		return err
	}

	p.ready.Store(true)

	var handlerErrors []error
	var shutdownInitiated bool
	for len(handlerErrors) < handlerCount {
		select {
		case routingConfig := <-p.configChan: // configuration changed
			p.routingConfig.Store(routingConfig)
			p.log.Info("proxy reconfigured")
		case err := <-errChan: // something ended
			p.ready.Store(false) // in case something fails - signal not-ready
			handlerErrors = append(handlerErrors, err)
			cancel()
		case <-ctx.Done(): // received a signal to shut down the server
			if shutdownInitiated {
				continue
			}
			p.log.Info("received shutdown signal, signalling not ready")
			p.ready.Store(false)
			shutdownInitiated = true
			time.Sleep(ShutdownPropagationTimeout)
			cancel()
			p.log.Info("shutdown initiated")
		}
	}

	var groupedError error
	for _, handlerError := range handlerErrors {
		if handlerError != nil {
			groupedError = fmt.Errorf("%w %w", groupedError, handlerError)
		}
	}

	cancel()

	return groupedError
}

func (p *Proxy) getStatusHandler() http.Handler {
	server := http.NewServeMux()
	server.Handle("/readyz", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if p.ready.Load() && p.routingConfig.Load() != nil {
			writer.WriteHeader(http.StatusNoContent)
		} else {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	server.Handle("/livez", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		writer.WriteHeader(http.StatusNoContent)
	}))

	return server
}
