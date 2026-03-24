package headers

import (
	"context"
	"strings"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
)

const headersCustomAnnotations = "s-ingress.oidq.dev/headers-response"

type customHeaderModule struct {
	config.Module
}

func ModuleCustomHeader(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	return &customHeaderModule{}, nil
}

func (wm *customHeaderModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	customHeadersRaw := ingress.Annotations[headersCustomAnnotations]
	if customHeadersRaw == "" {
		return nil, nil
	}

	headers := strings.Split(customHeadersRaw, "\n")

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		for _, header := range headers {
			splitHeader := strings.SplitN(header, ":", 2)
			if len(splitHeader) != 2 {
				continue
			}

			headerName := strings.TrimSpace(splitHeader[0])
			headerValue := strings.TrimSpace(splitHeader[1])
			if headerName == "" || headerValue == "" {
				continue
			}

			rCtx.ResponseHeader().Set(headerName, headerValue)
		}

		return next(rCtx)
	}, nil
}
