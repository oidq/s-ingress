package proxy

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// middleware that returns immediately and exposes rCtx.RealIp via response header for assertions
var echoRealIPMiddleware = func(rCtx *RequestContext, next NextFunc) error {
	rCtx.W.Header().Set("X-Real-IP", rCtx.RealIp.String())
	rCtx.W.WriteHeader(http.StatusOK)
	_, _ = rCtx.W.Write([]byte("ok"))
	return nil
}

func newTestProxyWithRoute(t *testing.T, cfg *RoutingConfig) *Proxy {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p := NewProxy(logger)
	// Ensure ErrorPage is non-nil to avoid panics in error paths in tests
	if cfg.ErrorPage == nil {
		cfg.ErrorPage = template.Must(template.New("err").Parse("{{.Status}}"))
	}
	// Inject routing config directly; we don't need to run servers for this unit test
	p.routingConfig.Store(cfg)
	return p
}

func makeBasicConfig(trusted []netip.Prefix) *RoutingConfig {
	return &RoutingConfig{
		Hosts: map[string]*HostConfig{
			"example.com": {
				Routes: []*RouteConfig{
					{
						Path:     "/",
						PathType: PathTypePrefix,
						Middlewares: []MiddlewareFunc{
							echoRealIPMiddleware,
						},
					},
				},
			},
		},
		TrustedProxies: trusted,
	}
}

func TestServeHTTP_ParsesXForwardedForFromTrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cfg := makeBasicConfig(trusted)
	p := newTestProxyWithRoute(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.2")
	// Simulate that the immediate peer is a trusted proxy
	req.RemoteAddr = "10.0.0.2:54321"

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusOK)
	}

	realIP := rec.Header().Get("X-Real-IP")
	if realIP != "203.0.113.5" {
		t.Fatalf("real ip not parsed from X-Forwarded-For: got %q, want %q", realIP, "203.0.113.5")
	}
}

func TestServeHTTP_IgnoresXForwardedForFromUntrustedProxy(t *testing.T) {
	// No trusted proxies configured
	cfg := makeBasicConfig(nil)
	p := newTestProxyWithRoute(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 192.0.2.10")
	// Immediate peer is untrusted; the header must be ignored
	req.RemoteAddr = "192.0.2.10:12345"

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusOK)
	}

	realIP := rec.Header().Get("X-Real-IP")
	// Should equal the RemoteAddr IP, because X-Forwarded-For is ignored
	if realIP != "192.0.2.10" {
		t.Fatalf("real ip should come from RemoteAddr when proxy untrusted: got %q, want %q", realIP, "192.0.2.10")
	}
}
