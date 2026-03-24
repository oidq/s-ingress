package config

import (
	"fmt"
	"html/template"
	"net/netip"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
)

// ControllerConf is the parsed configuration of the configuration.
type ControllerConf struct {
	// UpstreamServiceName is the name of the service considered as "upstream". Status of this service is used
	// to report Ingress status (load balancer IPs).
	UpstreamServiceName *string

	// DisableStatusUpdate is the flag to disable updating Ingress status.
	DisableStatusUpdate bool

	TcpProxy []TcpProxyConf

	// Tls is a configuration of the TLS negotiation logic.
	Tls ControllerTlsConf

	// GeneralProxy
	GeneralProxy GeneralProxyConf

	// Proxy is the defaul configuration of the proxy logic. Most of the values can be overridden by
	// annotations or modules.
	//
	// Most of the settings are passed to IngressProxyConf // TODO
	IngressProxy IngressProxyConf

	// Modules is a list of modules that are used to process the request.
	Modules []ModuleInstance

	// ModuleConfigs is a map of module configurations.
	ModuleConfigs map[string]yaml.Node

	// UsedSecrets is an array of secrets used in a creation of the [ControllerConf].
	UsedSecrets []types.NamespacedName

	// ErrorPage is the template used to render custom error pages for HTTP responses.
	ErrorPage template.Template
}

func (c *ControllerConf) GetModuleConf(name string, dest any) error {
	conf, ok := c.ModuleConfigs[name]
	if !ok {
		return nil
	}

	err := conf.Decode(dest)
	if err != nil {
		return fmt.Errorf("error decoding module config: %w", err)
	}

	return nil
}

// GeneralProxyConf is the general configuration used in proxy logic.
type GeneralProxyConf struct {
	TrustedProxies []netip.Prefix
	ErrorPage      *template.Template
}

// IngressProxyConf is the configuration related to the proxy logic, which can be customized per-ingress.
type IngressProxyConf struct {
	MaxBodySize int64
}

// Clone safely clones the configuration so it can be modified by annotation/modules or reconfigure.
func (c IngressProxyConf) Clone() IngressProxyConf {
	return IngressProxyConf{
		MaxBodySize: c.MaxBodySize,
	}
}

// ControllerTlsConf is a configuration of TLS termination.
type ControllerTlsConf struct {
	DefaultTlsSecret string
}

type TcpProxyConf struct {
	Port            int
	Service         types.NamespacedName
	ServicePortName string
}

func defaultVal[T comparable](value T, defaultValue T) T {
	var emptyValue T
	if value == emptyValue {
		return defaultValue
	}

	return value
}
