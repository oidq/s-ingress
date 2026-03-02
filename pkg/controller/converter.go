package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/netip"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type IngressEntry struct {
	Ingress *netv1.Ingress

	Configuration *IngressConfiguration
}

type IngressConfiguration struct {
	UsedSecrets    []types.NamespacedName
	UsedConfigMaps []types.NamespacedName
	UsedServices   []types.NamespacedName

	Hosts           map[string]*proxy.HostConfig
	TlsCertificates map[string]*tls.Certificate
}

type ingressReconciler struct {
	ctx context.Context

	ingressConf *IngressConfiguration
	k8sClient   client.Client
}

func (i *ingressReconciler) GetSecret(secret types.NamespacedName) (*v1.Secret, error) {
	s := &v1.Secret{}
	err := i.k8sClient.Get(i.ctx, secret, s)
	if err != nil {
		return nil, fmt.Errorf("error getting secret %s: %v", secret, err)
	}

	i.ingressConf.UsedSecrets = append(i.ingressConf.UsedSecrets, secret)
	return s, nil
}

var _ config.IngressReconciler = &ingressReconciler{}

// getProxyConfig aggregates resolved ingresses and produces [*proxy.RoutingConfig]. This function should not
// call any other k8s API calls and only merge existing, because it needs to acquire lock.
func (ic *IngressController) getProxyConfig() (*proxy.RoutingConfig, error) {
	var err error

	state := ic.k8sState
	state.RLock()
	defer state.RUnlock()

	proxyConfig := &proxy.RoutingConfig{
		Hosts:                 map[string]*proxy.HostConfig{},
		TlsCertificates:       map[string]*tls.Certificate{},
		MaxBodySize:           30 * 1024 * 1024,
		DefaultTlsCertificate: state.defaultTlsSecret,
		TrustedProxies:        state.config.GeneralProxy.TrustedProxies,
		ErrorPage:             state.config.GeneralProxy.ErrorPage,
		TcpProxy:              state.tcpProxies,
	}

	proxyConfig.RequestMiddlewares, err = getConnectionModules(state.config)
	if err != nil {
		return nil, err
	}

	for _, entry := range state.ingresses {
		if entry.Configuration == nil {
			continue
		}

		for hostname, host := range entry.Configuration.Hosts {
			for _, route := range host.Routes {
				if _, ok := proxyConfig.Hosts[hostname]; !ok {
					proxyConfig.Hosts[hostname] = &proxy.HostConfig{}
				}

				proxyConfig.Hosts[hostname].AddRoute(route)
			}
		}

		for host, certificate := range entry.Configuration.TlsCertificates {
			ic.log.Info("adding TLS", slog.String("host", host))
			_, ok := proxyConfig.TlsCertificates[host]
			if ok {
				ic.log.Warn("duplicated TLS certificates", slog.String("host", host))
				continue
			}

			proxyConfig.TlsCertificates[host] = certificate
		}
	}

	return proxyConfig, nil
}

func getConnectionModules(config *config.ControllerConf) ([]proxy.MiddlewareFunc, error) {
	var middlewares []proxy.MiddlewareFunc
	for _, module := range config.Modules {
		m, err := module.RequestMiddleware()
		if err != nil {
			return nil, fmt.Errorf("initialize middleware %T: %w", module, err)
		}
		if m != nil {
			middlewares = append(middlewares, m)
		}
	}

	return middlewares, nil
}

func (ic *IngressController) updateConfig(ctx context.Context) error {
	conf, err := ic.getControllerConfig(ctx)
	if err != nil {
		return err
	}

	state := ic.k8sState

	state.Lock()
	state.config = conf
	state.Unlock()

	return nil
}

func (ic *IngressController) updateTcpProxy(ctx context.Context) error {
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	var proxies []*proxy.TcpProxyConfig
	for _, tcp := range state.config.TcpProxy {
		endpoint, err := getServiceAddrPort(ctx, ic.k8sClient, tcp.Service, tcp.ServicePortName)
		if err != nil {
			return fmt.Errorf("could not get service endpoint for tcp proxy: %v", err)
		}

		proxies = append(proxies, &proxy.TcpProxyConfig{
			UseProxyProtocol: false,
			Port:             uint16(tcp.Port),
			EndpointAddr:     endpoint,
		})
	}

	state.tcpProxies = proxies

	return nil
}

func (ic *IngressController) updateDefaultTls(ctx context.Context) error {
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	cert, err := ic.getDefaultTls(ctx, state.config)
	if err != nil {
		return err
	}

	state.defaultTlsSecret = cert

	return nil
}

func (ic *IngressController) updateUpstreamIpAddress(ctx context.Context) error {
	state := ic.k8sState
	state.Lock()
	defer state.Unlock()

	upstreamIp := ic.getUpstreamIpAddresses(ctx, state.config)

	state.ingressLoadBalancerStatus = upstreamIp

	return nil
}

func (ic *IngressController) reconcileIngress(ctx context.Context, ingress *netv1.Ingress) (*IngressConfiguration, error) {
	ingressConfig := &IngressConfiguration{
		Hosts:           map[string]*proxy.HostConfig{},
		TlsCertificates: map[string]*tls.Certificate{},
	}

	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			ic.log.Info("adding path", slog.String("path", path.Path), slog.String("host", rule.Host))
			// ignore empty service for now
			if path.Backend.Service == nil {
				continue
			}

			err := addPath(ctx, ic.k8sClient, *ic.k8sState.config, ingressConfig, ingress, rule.Host, path)
			if err != nil {
				return nil, err
			}
		}
	}

	ic.ensureActiveIngressStatus(ctx, ingress)

	err := ic.processIngressTls(ctx, ingressConfig, ingress)
	if err != nil {
		return nil, fmt.Errorf("error processing ingress tls: %w", err)
	}

	return ingressConfig, nil
}

// ensureActiveIngressStatus updates the ingress object to have load balancer status set.
func (ic *IngressController) ensureActiveIngressStatus(ctx context.Context, ingress *netv1.Ingress) {
	state := ic.k8sState
	state.RLock()
	defer state.RUnlock()

	if state.config.DisableStatusUpdate {
		return
	}

	if ingress.Status.LoadBalancer.Ingress != nil {
		return // TODO: check if ingress is set correctly
	}

	if state.ingressLoadBalancerStatus == nil {
		return
	}

	ic.log.Warn("patching", slog.Any("info", state.ingressLoadBalancerStatus))
	ingress.Status.LoadBalancer = *state.ingressLoadBalancerStatus
	err := ic.k8sClient.Status().Update(ctx, ingress)
	if err != nil {
		ic.log.Error("failed to update ingress status", slog.String("error", err.Error()))
	}
}

func addPath(
	ctx context.Context,
	client client.Client,
	controllerConf config.ControllerConf,
	ingressConf *IngressConfiguration,
	ingress *netv1.Ingress,
	hostname string,
	path netv1.HTTPIngressPath,
) error {
	host, ok := ingressConf.Hosts[hostname]
	if !ok {
		host = &proxy.HostConfig{}
	}

	middlewares, err := getMiddlewares(ctx, client, controllerConf, ingressConf, ingress)
	if err != nil {
		return err
	}

	ingressService := path.Backend.Service

	namespacedService := types.NamespacedName{
		Namespace: ingress.Namespace,
		Name:      ingressService.Name,
	}
	ingressConf.UsedServices = append(ingressConf.UsedServices, namespacedService)

	service := v1.Service{}
	err = client.Get(ctx, namespacedService, &service)
	if err != nil {
		return fmt.Errorf("failed to get service %s/%s: %v", ingress.Namespace, hostname, err)
	}

	ipNet, err := netip.ParseAddr(service.Spec.ClusterIP)
	if err != nil {
		return fmt.Errorf("failed to parse service ip %s/%s: %v", ingress.Namespace, hostname, err)
	}

	var port int32
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Name == ingressService.Port.Name {
			port = servicePort.Port
			break
		}
		if servicePort.Port == ingressService.Port.Number {
			port = servicePort.Port
			break
		}
	}

	pathType, err := mapPathType(path.PathType)
	if err != nil {
		return err
	}

	host.AddRoute(&proxy.RouteConfig{
		Path:        path.Path,
		PathType:    pathType,
		Endpoint:    netip.AddrPortFrom(ipNet, uint16(port)),
		Middlewares: middlewares,
		IngressName: objectMetaToNamespaced(ingress).String(),
	})

	ingressConf.Hosts[hostname] = host

	return nil
}

func getServiceAddrPort(ctx context.Context, k8sClient client.Client, serviceName types.NamespacedName, portName string) (netip.AddrPort, error) {
	service := v1.Service{}
	err := k8sClient.Get(ctx, serviceName, &service)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("failed to get service %s: %w", serviceName.String(), err)
	}

	ipNet, err := netip.ParseAddr(service.Spec.ClusterIP)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("failed to parse service ip %s: %w", serviceName.String(), err)
	}

	for _, servicePort := range service.Spec.Ports {
		if servicePort.Name == portName {
			return netip.AddrPortFrom(ipNet, uint16(servicePort.Port)), nil
		}
	}

	return netip.AddrPort{}, fmt.Errorf("failed to find port %s for service %s", portName, serviceName.String())
}

func getMiddlewares(
	ctx context.Context,
	k8sClient client.Client,
	conf config.ControllerConf,
	ingressConfig *IngressConfiguration,
	ingress *netv1.Ingress,
) ([]proxy.MiddlewareFunc, error) {
	var middlewares []proxy.MiddlewareFunc

	reconciler := ingressReconciler{
		ctx:         ctx,
		ingressConf: ingressConfig,
		k8sClient:   k8sClient,
	}

	for _, m := range conf.Modules {
		middleware, err := m.IngressMiddleware(&reconciler, ingress)
		if err != nil {
			return nil, err
		}

		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}

	return middlewares, nil
}

func mapPathType(pathType *netv1.PathType) (proxy.PathType, error) {
	if pathType == nil {
		return proxy.PathTypeExact, nil
	}

	switch *pathType {
	case netv1.PathTypePrefix:
		return proxy.PathTypePrefix, nil
	case netv1.PathTypeExact:
		return proxy.PathTypeExact, nil
	case netv1.PathTypeImplementationSpecific:
		return proxy.PathTypePrefix, nil
	default:
		return 0, fmt.Errorf("invalid path type: %v", pathType)
	}
}

func (ic *IngressController) getDefaultTls(ctx context.Context, conf *config.ControllerConf) (*tls.Certificate, error) {
	if conf.Tls.DefaultTlsSecret == "" {
		return nil, nil
	}

	tlsSecret, err := ic.getCertificateFromSecret(ctx, types.NamespacedName{Name: conf.Tls.DefaultTlsSecret, Namespace: ic.envConfig.Namespace})
	if err != nil {
		return nil, err
	}

	return tlsSecret, nil
}

func (ic *IngressController) processIngressTls(ctx context.Context, ingressConf *IngressConfiguration, ingress *netv1.Ingress) error {
	ingTls := ingress.Spec.TLS
	namespace := ingress.Namespace

	for _, tlsItem := range ingTls {
		if tlsItem.SecretName == "" {
			continue
		}

		namespacedSecret := types.NamespacedName{Namespace: namespace, Name: tlsItem.SecretName}
		ingressConf.UsedSecrets = append(ingressConf.UsedSecrets, namespacedSecret)

		tlsSecret, err := ic.getCertificateFromSecret(ctx, namespacedSecret)
		if err != nil {
			return fmt.Errorf("error getting tls secret %s/%s: %v", namespace, tlsItem.SecretName, err)
		}

		for _, host := range tlsItem.Hosts {
			if _, ok := ingressConf.TlsCertificates[host]; ok {
				ic.log.Warn("ingress specifies duplicated TLS rule, ignoring",
					slog.Any("ingress", types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Name}),
					slog.String("host", host),
				)

				continue
			}
			ingressConf.TlsCertificates[host] = tlsSecret
		}
	}

	return nil
}

func (ic *IngressController) getCertificateFromSecret(ctx context.Context, secretName types.NamespacedName) (*tls.Certificate, error) {
	tlsSecret := v1.Secret{}
	err := ic.k8sClient.Get(ctx, secretName, &tlsSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS secret %s: %w", secretName.String(), err)
	}

	cert, err := tls.X509KeyPair(tlsSecret.Data["tls.crt"], tlsSecret.Data["tls.key"])
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &cert, nil
}
