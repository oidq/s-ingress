package basic_cluster_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/netip"
	"testing"

	_ "embed"

	"codeberg.org/oidq/s-ingress/e2e/common"
	. "codeberg.org/oidq/s-ingress/e2e/common/assertions"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	httpEndpoint  = netip.MustParseAddrPort("127.0.0.1:8080")
	httpsEndpoint = netip.MustParseAddrPort("127.0.0.1:8443")
	quicEndpoint  = netip.MustParseAddrPort("127.0.0.1:8443")
)

func TestE2e(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var _ = BeforeSuite(func() {
	common.SetupTransports(httpEndpoint, httpsEndpoint, quicEndpoint)
	common.SetupClient()
})

var _ = Describe("Ingress controller", func() {
	Describe("Serving on HTTP", func() {
		It("should redirect to https", func() {
			req := common.NewRequest(http.MethodGet, "http://example.com", nil)
			resp := common.RoundTripHttp(req)
			Expect(resp.StatusCode).To(Equal(http.StatusMovedPermanently))
			Expect(resp.Header.Get("Location")).To(Equal("https://example.com/"))
		})
	})

	Describe("Serving on HTTPs", func() {
		It("should return 200 on example.com", func() {
			req := common.NewRequest(http.MethodGet, "https://example.com", nil)
			resp := common.RoundTripHttps(req)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
		It("should return 200 on test.com", func() {
			req := common.NewRequest(http.MethodGet, "https://test.com", nil)
			resp := common.RoundTripHttps(req)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Serving on QUIC", func() {
		Context("with a existing host", func() {
			It("should return 200", func() {
				req := common.NewRequest(http.MethodGet, "https://example.com/", nil)
				resp := common.RoundTripQuic(req)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
		})
	})

	Describe("Limiting body size", func() {
		Context("with an Ingress limit 1K", func() {
			It("should be ok for 900B", func() {
				buff := bytes.NewBuffer(make([]byte, 900))
				req := common.NewRequest(http.MethodGet, "https://example.com/limited", buff)
				resp := common.RoundTripHttps(req)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
			It("should not be ok for 2K", func() {
				buff := bytes.NewBuffer(make([]byte, 2000))
				req := common.NewRequest(http.MethodGet, "https://example.com/limited", buff)
				resp := common.RoundTripHttps(req)
				Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			})
		})
		Context("without an Ingress limit", func() {
			It("should be ok for 2K", func() {
				buff := bytes.NewBuffer(make([]byte, 2000))
				req := common.NewRequest(http.MethodGet, "https://example.com/", buff)
				resp := common.RoundTripHttps(req)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
			It("should not be ok for 5K", func() {
				buff := bytes.NewBuffer(make([]byte, 5000))
				req := common.NewRequest(http.MethodGet, "https://example.com/", buff)
				resp := common.RoundTripHttps(req)
				Expect(resp.StatusCode).To(Equal(http.StatusRequestEntityTooLarge))
			})
		})
	})

	Describe("Logging", func() {
		It("should log basic information", func() {
			id := uuid.New()
			url := fmt.Sprintf("https://example.com/%s/", id)
			req := common.NewRequest(http.MethodGet, url, nil)
			resp := common.RoundTripHttps(req)

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			ExpectIngressLog("s-ingress", id.String()).To(And(
				HaveLogAttribute("http.request.method", "GET"),
				HaveLogAttributeSet("http.request.id"),
			))
		})
	})

	Describe("reconciling annotations", func() {
		BeforeEach(func() {
			common.ApplyIngress(`
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: deny-route-test
spec:
  ingressClassName: s-ingress
  rules:
    - http:
        paths:
          - pathType: Prefix
            path: /deny-route
            backend:
              service:
                name: example-service
                port:
                  number: 8080
---
			`)
		})
		It("should reconcile on deny-route change", func() {
			Eventually(func(g Gomega) {
				req := common.NewRequest(http.MethodGet, "https://example.com/deny-route", nil)
				resp := common.RoundTripQuic(req)
				g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}).Should(Succeed(), "deny-route without annotations")

			common.PatchIngress(
				"deny-route-test",
				`{"metadata": {"annotations": {"s-ingress.oidq.dev/deny-route": "^/"}}}`,
			)

			Eventually(func(g Gomega) {
				req := common.NewRequest(http.MethodGet, "https://example.com/deny-route", nil)
				resp := common.RoundTripQuic(req)
				g.Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			}).Should(Succeed(), "deny-route with annotations")
		})
	})
})
