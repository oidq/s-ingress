package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func makeWildcardConfig() *RoutingConfig {
	return &RoutingConfig{
		Hosts: map[string]*HostConfig{
			".example.com": {
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
	}
}

func TestLookupHost_WildcardMatchesSubdomain(t *testing.T) {
	cfg := makeWildcardConfig()

	if cfg.LookupHost("foo.example.com") == nil {
		t.Fatal("expected wildcard to match foo.example.com, got nil")
	}
	if cfg.LookupHost("bar.example.com") == nil {
		t.Fatal("expected wildcard to match bar.example.com, got nil")
	}
}

func TestLookupHost_WildcardDoesNotMatchApex(t *testing.T) {
	cfg := makeWildcardConfig()

	if cfg.LookupHost("example.com") != nil {
		t.Fatal("wildcard should not match apex domain example.com")
	}
}

func TestLookupHost_WildcardDoesNotMatchDeepSubdomain(t *testing.T) {
	cfg := makeWildcardConfig()

	// *.example.com should not match a.b.example.com because only the first label is stripped
	if cfg.LookupHost("a.b.example.com") != nil {
		t.Fatal("wildcard should not match deep subdomain a.b.example.com")
	}
}

func TestNormalizeHost_WildcardStripsAsterisk(t *testing.T) {
	got, err := normalizeHost("*.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ".example.com" {
		t.Fatalf("normalizeHost(*.example.com) = %q, want %q", got, ".example.com")
	}
}

func TestNormalizeHost_FullyQualifiedUnchanged(t *testing.T) {
	got, err := normalizeHost("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "example.com" {
		t.Fatalf("normalizeHost(example.com) = %q, want %q", got, "example.com")
	}
}

func TestAddHost_WildcardNormalized(t *testing.T) {
	cfg := &RoutingConfig{Hosts: map[string]*HostConfig{}}
	hc := &HostConfig{}
	if err := cfg.AddHost("*.example.com", hc); err != nil {
		t.Fatalf("AddHost returned error: %v", err)
	}
	if _, ok := cfg.Hosts[".example.com"]; !ok {
		t.Fatal("expected key .example.com in Hosts after AddHost with wildcard")
	}
}

func TestServeHTTP_WildcardHostRouted(t *testing.T) {
	cfg := makeWildcardConfig()
	p := newTestProxyWithRoute(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "http://sub.example.com/", nil)
	req.RemoteAddr = "1.2.3.4:5678"

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for wildcard host, got %d", rec.Code)
	}
}

func TestServeHTTP_WildcardApexReturns404(t *testing.T) {
	cfg := makeWildcardConfig()
	p := newTestProxyWithRoute(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "1.2.3.4:5678"

	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for apex when only wildcard configured, got %d", rec.Code)
	}
}

func TestHostConfig_AddRoute(t *testing.T) {
	hc := HostConfig{}
	hc.AddRoute(&RouteConfig{
		Path:     "/",
		PathType: PathTypePrefix,
	})
	hc.AddRoute(&RouteConfig{
		Path:     "/test",
		PathType: PathTypePrefix,
	})

	fmt.Printf("%#v\n", hc.Routes)

	route := hc.LookupRoute("/test/a")
	require.NotNil(t, route)
	require.Equal(t, "/test", route.Path)
}
