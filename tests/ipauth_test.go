package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	test2 "codeberg.org/oidq/s-ingress/pkg/test"
	"github.com/stretchr/testify/require"
)

func TestIpAuthModule(t *testing.T) {
	tc := test2.SetupTest(
		t,
		"",
		test2.GetDummyServiceIngress(
			"test",
			"example.com",
			map[string]string{
				"s-ingress.oidq.dev/allow-ip": "203.0.0.0/8",
			},
		),
	)

	tc.WaitForHostnameConfigured("example.com")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "203.0.113.0:1234"

	resp := tc.Serve(req)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIpAuthModule_Rejected(t *testing.T) {
	tc := test2.SetupTest(
		t,
		"",
		test2.GetDummyServiceIngress(
			"test",
			"example.com",
			map[string]string{
				"s-ingress.oidq.dev/allow-ip": "203.0.0.0/8",
			},
		),
	)

	tc.WaitForHostnameConfigured("example.com")

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "204.0.113.0:1234"

	resp := tc.Serve(req)

	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
