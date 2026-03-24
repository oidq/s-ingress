package security

import (
	"context"
	"fmt"
	"regexp"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
)

const denyRouteAnnotation = "s-ingress.oidq.dev/deny-route"

type denyRouteModule struct {
	config.Module
}

func ModuleDenyRoute(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	return &denyRouteModule{}, nil
}

func (dm *denyRouteModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	denyAnnotation := ingress.Annotations[denyRouteAnnotation]
	if denyAnnotation == "" {
		return nil, nil
	}

	denyRegexp, err := regexp.Compile(denyAnnotation)
	if err != nil {
		return nil, fmt.Errorf("error compiling deny route regexp: %w", err)
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		if denyRegexp.MatchString(rCtx.R.URL.Path) {
			return rCtx.Forbidden("denied")
		}

		return next(rCtx)
	}, nil
}
