package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	netv1 "k8s.io/api/networking/v1"
)

const forwardAuthAnnotationKey = "s-ingress.oidq.dev/auth"
const forwardAuthUrlAnnotationKey = "s-ingress.oidq.dev/auth-url"
const forwardAuthSignInAnnotationKey = "s-ingress.oidq.dev/auth-signin"

type subrequestAuthModule struct {
	config.Module

	configs map[string]ForwardAuthConfig
}

type ModuleConfig struct {
	Configurations map[string]ForwardAuthConfig `yaml:"configurations"`
}

type ForwardAuthConfig struct {
	AuthUrl   string `yaml:"authUrl"`
	SignInUrl string `yaml:"signInUrl"`
}

func ModuleSubrequestAuth(ctx context.Context, reconciler config.ModuleReconciler, conf *config.ControllerConf) (config.ModuleInstance, error) {
	var moduleConf ModuleConfig
	err := conf.GetModuleConf("auth", &moduleConf)
	if err != nil {
		return nil, fmt.Errorf("error decoding module config: %w", err)
	}

	return &subrequestAuthModule{
		configs: moduleConf.Configurations,
	}, nil
}

func (sam *subrequestAuthModule) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {

	conf, err := getGlobalConfig(sam.configs, ingress)
	if err != nil {
		return nil, err
	}

	annotationConf := getAnnotationsConfig(ingress)
	if annotationConf != nil {
		conf = annotationConf
	}

	if conf == nil {
		return nil, nil
	}

	authUrl, err := url.Parse(conf.AuthUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse forward-auth url: %w", err)
	}

	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	transport := http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
		DialContext:           dialer.DialContext,
		IdleConnTimeout:       10 * time.Second,
	}

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		authRequest, err := http.NewRequestWithContext(rCtx, "GET", authUrl.String(), nil)
		if err != nil {
			return fmt.Errorf("forward-auth request creation failed: %w", err)
		}

		authRequest.Header = rCtx.R.Header.Clone()

		resp, err := transport.RoundTrip(authRequest)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden {
			return rCtx.Forbidden("")
		}

		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return next(rCtx)
		}

		redirectUrl := getRedirectUrl(rCtx, conf.SignInUrl)
		http.Redirect(rCtx.W, rCtx.R, redirectUrl, http.StatusFound)
		return nil
	}, nil
}

func getAnnotationsConfig(ingress *netv1.Ingress) *ForwardAuthConfig {
	authUrlRaw, ok := ingress.Annotations[forwardAuthUrlAnnotationKey]
	if !ok {
		return nil
	}

	signInRaw, ok := ingress.Annotations[forwardAuthSignInAnnotationKey]
	if !ok {
		return nil
	}

	return &ForwardAuthConfig{
		AuthUrl:   authUrlRaw,
		SignInUrl: signInRaw,
	}
}

func getGlobalConfig(auth map[string]ForwardAuthConfig, ingress *netv1.Ingress) (*ForwardAuthConfig, error) {
	authRaw, ok := ingress.Annotations[forwardAuthAnnotationKey]
	if !ok {
		return nil, nil
	}

	conf, ok := auth[authRaw]
	if !ok {
		return nil, fmt.Errorf("forward auth configuration %q not found", authRaw)
	}

	return &conf, nil
}

func getRedirectUrl(rCtx *proxy.RequestContext, signinUrl string) string {
	signinUrl = strings.Replace(signinUrl, "$escaped_uri", url.QueryEscape(rCtx.R.URL.String()), 1)
	signinUrl = strings.Replace(signinUrl, "$host", url.QueryEscape(rCtx.R.Host), 1)
	return signinUrl
}
