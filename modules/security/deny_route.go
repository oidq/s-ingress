package security

import (
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

func ModuleDenyRoute(config *config.ControllerConf) (config.ModuleInstance, error) {
	var moduleConf ModuleConfig
	err := config.GetModuleConf("security", &moduleConf)
	if err != nil {
		return nil, fmt.Errorf("error decoding module config: %w", err)
	}

	return &denyRouteModule{}, nil
}

func (dm *denyRouteModule) IngressMiddleware(reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
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
