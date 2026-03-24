package auth

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
)

const authIpAllowlist = "s-ingress.oidq.dev/allow-ip"

type ipAuthModule struct {
	config.Module
}

func ModuleIpAuth(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	return &ipAuthModule{}, nil
}

func (ipm *ipAuthModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	authUrlRaw, ok := ingress.Annotations[authIpAllowlist]
	if !ok {
		return nil, nil
	}

	var allowlist []netip.Prefix
	for _, v := range strings.Split(authUrlRaw, ",") {
		v = strings.TrimSpace(v)
		ipNet, err := netip.ParsePrefix(v)
		if err != nil {
			return nil, fmt.Errorf("invalid ip auth url: %s: %w", v, err)
		}

		allowlist = append(allowlist, ipNet)
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		for _, ipNet := range allowlist {
			if ipNet.Contains(rCtx.RealIp) {
				return next(rCtx)
			}
		}

		return rCtx.Forbidden("forbidden")
	}, nil
}
