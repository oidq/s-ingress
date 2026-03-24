package auth

import (
	"bytes"
	"context"
	"fmt"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/tg123/go-htpasswd"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
)

const authBasicAuthSecret = "s-ingress.oidq.dev/basic-auth-secret"
const authBasicAuthRealm = "s-ingress.oidq.dev/basic-auth-realm"

type basicAuthModule struct {
	config.Module
}

func ModuleBasicAuth(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	return &basicAuthModule{}, nil
}

func (ipm *basicAuthModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	realm, ok := ingress.Annotations[authBasicAuthRealm]
	if !ok {
		return nil, nil
	}

	secretRaw, ok := ingress.Annotations[authBasicAuthSecret]
	if !ok {
		return nil, nil
	}

	secret, err := reconciler.GetSecret(ctx, types.NamespacedName{Name: secretRaw, Namespace: ingress.Namespace})
	if err != nil {
		return nil, fmt.Errorf("error getting secret %s: %v", secretRaw, err)
	}

	htpasswdRaw, ok := secret.Data["auth"]
	if !ok {
		return nil, fmt.Errorf("error getting secret %s: secret does not contain auth", secretRaw)
	}

	auth, err := htpasswd.NewFromReader(bytes.NewReader(htpasswdRaw), htpasswd.DefaultSystems, nil)
	if err != nil {
		return nil, fmt.Errorf("error parsing htpasswd secret %s: %v", secret, err)
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		user, passwd, ok := rCtx.R.BasicAuth()
		if !ok {
			rCtx.W.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s", charset="UTF-8"`, realm))
			return rCtx.Unauthorized("")
		}
		if !auth.Match(user, passwd) {
			rCtx.W.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s", charset="UTF-8"`, realm))
			return rCtx.Forbidden("forbidden")
		}

		return next(rCtx)
	}, nil
}
