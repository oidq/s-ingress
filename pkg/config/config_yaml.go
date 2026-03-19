package config

import (
	"bytes"
	"fmt"
	"html/template"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	_ "embed"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
)

//go:embed error.tmpl
var defaultErrorPage []byte

//go:embed config.yaml
var defaultConfig []byte

// ControllerConf is the parsed configuration of the configuration.
type controllerConf struct {
	// UpstreamServiceName is the name of the service considered as "upstream". Status of this service is used
	// to report Ingress status (load balancer IPs).
	UpstreamServiceName *string `yaml:"upstreamServiceName"`

	// DisableStatusUpdate is the flag to disable updating Ingress status.
	DisableStatusUpdate bool `yaml:"disableStatusUpdate"`

	TcpProxy []tcpProxyConf `yaml:"tcpProxy"`

	// Tls is a configuration of the TLS negotiation logic.
	Tls controllerTlsConf `yaml:"tls"`

	// Proxy is a configuration of the http proxy behavior.
	Proxy proxyConf `yaml:"proxy"`

	// Module contains configuration of modules, see given module package for configuration.
	Module map[string]yaml.Node `yaml:"module"`
}

type proxyConf struct {
	MaxBodySize    string   `yaml:"maxBodySize"`
	TrustedProxies []string `yaml:"trustedProxies"`
	ErrorPage      string   `yaml:"errorPage"`
}

// ControllerTlsConf is configuration of TLS termination.
type controllerTlsConf struct {
	DefaultTlsSecret string `yaml:"defaultTlsSecret"`
}

func decode[T any](data []byte, target *T) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(target)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	return nil
}

func ParseYamlConfigWithDefault(data []byte) (*ControllerConf, error) {
	var conf controllerConf

	err := decode(defaultConfig, &conf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if len(data) > 0 {
		err = decode(data, &conf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	parsedConf, err := parseConfig(&conf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return parsedConf, nil
}

func ParseYamlConfig(data []byte) (*ControllerConf, error) {
	var conf controllerConf

	if len(data) > 0 {
		err := decode(data, &conf)
		if err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	parsedConf, err := parseConfig(&conf)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return parsedConf, nil
}

func parseConfig(conf *controllerConf) (*ControllerConf, error) {
	var err error
	result := &ControllerConf{
		UpstreamServiceName: conf.UpstreamServiceName,
		DisableStatusUpdate: conf.DisableStatusUpdate,
		ModuleConfigs:       conf.Module,
	}

	result.Tls, err = parseProxyTlsConf(conf.Tls)
	if err != nil {
		return nil, fmt.Errorf("tls config: %w", err)
	}

	result.GeneralProxy, err = parseGeneralProxyConf(conf.Proxy)
	if err != nil {
		return nil, fmt.Errorf("ingress proxy config: %w", err)
	}

	result.IngressProxy, err = parseProxyIngressConf(conf.Proxy)
	if err != nil {
		return nil, fmt.Errorf("ingress proxy config: %w", err)
	}

	result.TcpProxy, err = parseTcpProxyConf(conf.TcpProxy)
	if err != nil {
		return nil, fmt.Errorf("tcp proxy config: %w", err)
	}

	return result, nil
}

func parseProxyTlsConf(conf controllerTlsConf) (ControllerTlsConf, error) {
	result := ControllerTlsConf(conf)

	return result, nil
}

func parseProxyIngressConf(conf proxyConf) (IngressProxyConf, error) {
	var err error
	result := IngressProxyConf{}

	if conf.MaxBodySize != "" {
		result.MaxBodySize, err = ParseByteSize(conf.MaxBodySize)
		if err != nil {
			return result, fmt.Errorf("proxy config: %w", err)
		}
	} else {
		result.MaxBodySize = 4096
	}

	return result, nil
}

func parseGeneralProxyConf(conf proxyConf) (GeneralProxyConf, error) {
	result := GeneralProxyConf{}
	for _, trustedProxy := range conf.TrustedProxies {
		parseProxyIp, err := netip.ParsePrefix(trustedProxy)
		if err != nil {
			return result, fmt.Errorf("invalid trusted proxy ip %q: %w", trustedProxy, err)
		}

		result.TrustedProxies = append(result.TrustedProxies, parseProxyIp)
	}

	errorPage, err := template.New("error").Parse(defaultVal(conf.ErrorPage, string(defaultErrorPage)))
	if err != nil {
		return result, fmt.Errorf("failed to parse error page: %w", err)
	}
	result.ErrorPage = errorPage

	return result, nil
}

type tcpProxyConf struct {
	ServiceName      string `yaml:"serviceName"`
	ServiceNamespace string `yaml:"serviceNamespace"`
	ServicePortName  string `yaml:"servicePortName"`

	Port int `yaml:"port"`
}

func parseTcpProxyConf(conf []tcpProxyConf) ([]TcpProxyConf, error) {
	var result []TcpProxyConf
	for _, tcpConf := range conf {
		result = append(result, TcpProxyConf{
			Service: types.NamespacedName{
				Namespace: tcpConf.ServiceNamespace,
				Name:      tcpConf.ServiceName,
			},
			ServicePortName: tcpConf.ServicePortName,
			Port:            tcpConf.Port,
		})
	}

	return result, nil
}

var sizeMap = map[string]int64{
	"":    1,
	"k":   1000,
	"kb":  1000,
	"kib": 2 << 9,
	"m":   10_000_000,
	"mb":  10_000_000,
	"mib": 2 << 19,
	"g":   10_000_000_000,
	"gb":  10_000_000_000,
	"gib": 2 << 29,
}

func ParseByteSize(sizeRaw string) (int64, error) {
	sizeRegexp := regexp.MustCompile(`^(\d+)([A-z]*)$`)
	matches := sizeRegexp.FindStringSubmatch(sizeRaw)
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid size: %q", sizeRaw)
	}

	size, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || size < 0 {
		return 0, fmt.Errorf("invalid size: %q", sizeRaw)
	}

	multiplier, ok := sizeMap[strings.ToLower(matches[2])]
	if !ok {
		return 0, fmt.Errorf("invalid unit: %q", matches[2])
	}

	return size * multiplier, nil
}
