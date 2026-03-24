package security

import (
	"context"
	"fmt"
	"net/http"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
)

const enforceHttpsAnnotation = "s-ingress.oidq.dev/enforce-https"

type enforceHttpsModule struct {
	config.Module

	defaultEnforceHttps bool
}

func ModuleEnforceHttps(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	var moduleConf ModuleConfig
	err := conf.GetModuleConf("security", &moduleConf)
	if err != nil {
		return &enforceHttpsModule{}, fmt.Errorf("error decoding module config: %w", err)
	}

	return &enforceHttpsModule{
		defaultEnforceHttps: moduleConf.DefaultEnforceHttps,
	}, nil
}

func (wm *enforceHttpsModule) RequestMiddleware() (proxy.MiddlewareFunc, error) {
	if !wm.defaultEnforceHttps {
		return nil, nil
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		if rCtx.R.TLS == nil {
			return redirectHttps(rCtx)
		}

		rCtx.ResponseHeader().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		return next(rCtx)
	}, nil
}

func (wm *enforceHttpsModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	doRedirect := wm.defaultEnforceHttps
	if ingress.Annotations[enforceHttpsAnnotation] == "true" {
		doRedirect = true
	}

	if !doRedirect {
		return nil, nil
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		if rCtx.R.TLS == nil {
			return redirectHttps(rCtx)
		}

		return next(rCtx)
	}, nil
}

func redirectHttps(rCtx *proxy.RequestContext) error {
	newUrl := *rCtx.R.URL
	newUrl.Scheme = "https"
	newUrl.Host = rCtx.R.Host
	http.Redirect(rCtx.W, rCtx.R, newUrl.String(), http.StatusMovedPermanently)
	return nil
}
