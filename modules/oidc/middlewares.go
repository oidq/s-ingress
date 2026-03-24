package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/websocket"
	netv1 "k8s.io/api/networking/v1"
)

const (
	ConfigAnnotationKey     = "s-ingress.oidq.dev/oidc"
	AllowGroupAnnotationKey = "s-ingress.oidq.dev/oidc-require-group"
	AllowEmailAnnotationKey = "s-ingress.oidq.dev/oidc-require-email"

	cookieState    = "_oidc_state"
	cookieNonce    = "_oidc_nonce"
	cookieRedirect = "_oidc_redirect"
)

type ingressConfig struct {
	allowedGroups []string
	allowedEmails []string
}

func (m *module) IngressMiddleware(ctx context.Context, reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	confName, ok := ingress.Annotations[ConfigAnnotationKey]
	if !ok || confName == "" {
		return nil, nil
	}

	client, ok := m.clients[confName]
	if !ok {
		return nil, fmt.Errorf("no configuration named %q found", confName)
	}

	ingressOidcConfig := parseIngressAnnotations(ingress.Annotations)

	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		info, err := extractAuthInfo(rCtx, client)
		switch {
		case errors.Is(err, errCookieNotFound) || errors.Is(err, errCookieInvalid):
			return handleUnauthorized(rCtx, client)
		case err != nil:
			return fmt.Errorf("extract info: %w", err)
		}

		info = handleRenewingAuthInfo(rCtx, client, info)

		if !validateInfo(rCtx, client, info) {
			return handleUnauthorized(rCtx, client)
		}

		// enhance logs with auth information
		rCtx.Log = rCtx.Log.With(
			slog.String("user.email", info.Email),
			slog.String("user.id", info.Subject),
		)

		if !checkAccess(rCtx, ingressOidcConfig, info) {
			return rCtx.Forbidden("no access to given resource")
		}

		return next(rCtx)
	}, nil
}

func (m *module) RequestMiddleware() (proxy.MiddlewareFunc, error) {
	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		client, ok := m.callbackDomains[rCtx.R.Host]

		if ok && rCtx.R.Method == http.MethodGet && rCtx.R.URL.Path == callbackPath {
			return handleCallback(rCtx, client)
		}

		return next(rCtx)
	}, nil
}

func parseIngressAnnotations(annotations map[string]string) *ingressConfig {
	return &ingressConfig{
		allowedGroups: parseAnnotationArray(annotations[AllowGroupAnnotationKey]),
		allowedEmails: parseAnnotationArray(annotations[AllowEmailAnnotationKey]),
	}
}

func parseAnnotationArray(rawValue string) []string {
	if rawValue == "" {
		return nil
	}

	values := strings.Split(rawValue, ",")

	var clearedValues []string
	for _, value := range values {
		clearedValues = append(clearedValues, strings.TrimSpace(value))
	}

	return clearedValues
}

func handleUnauthorized(rCtx *proxy.RequestContext, client *clientConfig) error {
	cookieName := getCookieName(client)
	if _, err := rCtx.R.Cookie(cookieName); err == nil {
		clearHttpCookie(rCtx, client, cookieName)
	}

	if (rCtx.R.Method == http.MethodGet || rCtx.R.Method == http.MethodHead) &&
		!websocket.IsWebSocketUpgrade(rCtx.R) {

		return startLogin(rCtx, client)
	}

	return rCtx.Unauthorized("OIDC authorization required")
}

func validateInfo(rCtx *proxy.RequestContext, client *clientConfig, info *authInfo) bool {
	if info == nil {
		return false
	}

	if info.ClientName != client.name {
		return false
	}

	t0 := time.Now()

	if client.expiration != 0 {
		authAt := time.Unix(info.AuthenticatedAt, 0)
		return authAt.Add(client.expiration).After(t0)
	}

	return time.Unix(info.TokenExpiry, 0).After(t0)
}

func checkAccess(rCtx *proxy.RequestContext, ingress *ingressConfig, info *authInfo) bool {
	if ingress.allowedEmails == nil && ingress.allowedGroups == nil {
		return true
	}

	if slices.Contains(ingress.allowedEmails, info.Email) {
		return true
	}

	for _, group := range info.Groups {
		if slices.Contains(ingress.allowedGroups, group) {
			return true
		}
	}

	return false
}

func handleCallback(rCtx *proxy.RequestContext, client *clientConfig) error {
	info, err := handleOidcCallback(rCtx, client)
	switch {
	case err != nil:
		return fmt.Errorf("handle oidc callback: %w", err)
	case info == nil:
		clearCallbackCookies(rCtx, client)
		return nil
	}

	cookie, err := generateJwtCookie(rCtx, client, info)
	if err != nil {
		clearCallbackCookies(rCtx, client)
		return fmt.Errorf("generate jwt cookie: %w", err)
	}

	redirect := client.callbackDomain
	redirectCookie, err := rCtx.R.Cookie(cookieRedirect)
	if err == nil {
		redirect = redirectCookie.Value
	}

	http.SetCookie(rCtx.W, cookie)
	clearCallbackCookies(rCtx, client)

	http.Redirect(rCtx.W, rCtx.R, redirect, http.StatusFound)
	return nil
}

func startLogin(rCtx *proxy.RequestContext, client *clientConfig) error {
	state, err := randString(16)
	if err != nil {
		return fmt.Errorf("could not generate state: %w", err)
	}
	nonce, err := randString(16)
	if err != nil {
		return fmt.Errorf("could not generate nonce: %w", err)
	}

	setHttpCookie(rCtx, client, cookieState, state)
	setHttpCookie(rCtx, client, cookieNonce, nonce)
	setHttpCookie(rCtx, client, cookieRedirect, getRequestUrl(rCtx))

	http.Redirect(rCtx.W, rCtx.R, client.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusFound)
	return nil
}

func getRequestUrl(rCtx *proxy.RequestContext) string {
	u := url.URL{
		Scheme:   "https",
		Host:     rCtx.R.Host,
		Path:     rCtx.R.URL.Path,
		Fragment: rCtx.R.URL.Fragment,
	}
	return u.String()
}

type claims struct {
	Email  string   `json:"email"`
	Groups []string `json:"groups"`
}

func randString(nByte int) (string, error) {
	b := make([]byte, nByte)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func clearCallbackCookies(rCtx *proxy.RequestContext, client *clientConfig) {
	clearHttpCookie(rCtx, client, cookieRedirect)
	clearHttpCookie(rCtx, client, cookieNonce)
	clearHttpCookie(rCtx, client, cookieState)
}

func setHttpCookie(rCtx *proxy.RequestContext, conf *clientConfig, name, value string) {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   int(time.Hour.Seconds()),
		Secure:   rCtx.R.TLS != nil,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Domain:   conf.cookieDomain,
	}
	http.SetCookie(rCtx.W, c)
}

func clearHttpCookie(rCtx *proxy.RequestContext, conf *clientConfig, name string) {
	if _, err := rCtx.R.Cookie(name); err != nil {
		return
	}

	c := &http.Cookie{
		Name:     name,
		MaxAge:   -1,
		Secure:   rCtx.R.TLS != nil,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Domain:   conf.cookieDomain,
	}
	http.SetCookie(rCtx.W, c)
}
