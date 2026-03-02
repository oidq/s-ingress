package proxy

import (
	"crypto/tls"
	"fmt"
	"html/template"
	"net/netip"
	"slices"
	"strings"
)

type PathType int

const (
	PathTypeExact PathType = iota
	PathTypePrefix
)

type RoutingConfig struct {
	// Hosts is the map of [HostConfig] based on the hostname key.
	//
	// The key have a required format based on their type:
	// * Fully specified hosts are required to be in the basic form without a trailing dot.
	// * Wildcard hosts are required to remove the wildcard part ("*") and preserve the
	//   prefixed dot. (e.g. ".domain.example")
	Hosts map[string]*HostConfig

	// TlsCertificates contains a map of TLS certificates. The key is based on
	// the matching hosts. Asterisk represents wildcard hosts as the single
	// name in the first DNS part (for example *.full.example).
	TlsCertificates map[string]*tls.Certificate

	// DefaultTlsCertificate is used if none of the [RoutingConfig.TlsCertificates]
	// matches the request.
	DefaultTlsCertificate *tls.Certificate

	TcpProxy []*TcpProxyConfig

	// RequestMiddlewares are called for each request served by the proxy. Note that the routing decision
	// has not been made yet and some fields of the context will not be initialized.
	RequestMiddlewares []MiddlewareFunc

	// MaxBodySize is the default maximum allowed size of body.
	MaxBodySize int64

	// TrustedProxies is the list of trusted proxies IP addresses in respect to X-Forwarded-For.
	TrustedProxies []netip.Prefix

	// ErrorPage is a template for rendering error pages.
	ErrorPage *template.Template
}

func (rc *RoutingConfig) AddHost(host string, config *HostConfig) error {
	normalizedHost, err := normalizeHost(host)
	if err != nil {
		return err
	}
	rc.Hosts[normalizedHost] = config
	return nil
}

func (rc *RoutingConfig) LookupHost(rawHost string) *HostConfig {
	host, ok := rc.Hosts[rawHost] // exact match
	if ok {
		return host
	}

	// remove the first part of the host "test.domain.example" -> ".domain.example"
	firstDotIndex := strings.Index(rawHost, ".")
	if firstDotIndex != -1 {
		cutHost := rawHost[firstDotIndex:]

		// lookup for wildcards
		host, ok = rc.Hosts[cutHost]
		if ok {
			return host
		}
	}

	// default ingress
	host, ok = rc.Hosts[""]
	if ok {
		return host
	}

	return nil
}

type HostConfig struct {
	Routes []*RouteConfig
}

// AddRoute adds a route to the [HostConfig]
func (hc *HostConfig) AddRoute(route *RouteConfig) {
	// exact paths need to be prepended to the array to prioritize them on lookup
	if route.PathType == PathTypeExact {
		hc.Routes = slices.Insert(hc.Routes, 0, route)
		return
	}

	for i, r := range hc.Routes {
		if r.PathType == PathTypeExact {
			continue // the exact paths are first
		}

		if len(r.Path) > len(route.Path) {
			continue // longer paths takes priority
		}

		hc.Routes = slices.Insert(hc.Routes, i, route)
		return
	}

	hc.Routes = append(hc.Routes, route)
}

func (hc *HostConfig) LookupRoute(path string) *RouteConfig {
	for _, route := range hc.Routes {
		if route.MatchesPath(path) {
			return route
		}
	}

	return nil
}

type RouteConfig struct {
	Path     string
	PathType PathType

	Endpoint netip.AddrPort

	Middlewares []MiddlewareFunc

	IngressName string
}

func (rc *RouteConfig) MatchesPath(rawPath string) bool {
	switch rc.PathType {
	case PathTypeExact:
		return rc.Path == rawPath
	case PathTypePrefix:
		if rc.Path[:len(rc.Path)-1] == rawPath {
			return true
		}
		return strings.HasPrefix(rawPath, rc.Path)
	default:
		panic("invalid path type")
	}
}

type TcpProxyConfig struct {
	UseProxyProtocol bool
	Port             uint16
	EndpointAddr     netip.AddrPort
}

func normalizeHost(host string) (string, error) {
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host, nil
	}

	for _, part := range parts {
		if len(part) < 1 {
			return "", fmt.Errorf("invalid host: %s", host)
		}
	}

	if parts[0] == "*" {
		return host[1:], nil
	}

	return host, nil
}
