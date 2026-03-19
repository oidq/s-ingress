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

var _ = Describe("Ingress", func() {
	Describe("Serving on HTTP", func() {
		t := common.GetHttpTransport(httpEndpoint)
		It("should redirect to https", func() {
			req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
			Expect(err).ToNot(HaveOccurred())
			resp, err := t.RoundTrip(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusMovedPermanently))
			Expect(resp.Header.Get("Location")).To(Equal("https://example.com/"))
		})
	})

	Describe("Serving on HTTPs", func() {
		ts := common.GetHttpsTransport(httpsEndpoint)
		It("should return 200 on example.com", func() {
			req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
			Expect(err).ToNot(HaveOccurred())
			resp, err := ts.RoundTrip(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
		It("should return 200 on test.com", func() {
			req, err := http.NewRequest(http.MethodGet, "https://test.com/", nil)
			Expect(err).ToNot(HaveOccurred())
			resp, err := ts.RoundTrip(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})

	Describe("Serving on QUIC", func() {
		q := common.GetQuicTransport(quicEndpoint)
		Context("with a existing host", func() {
			It("should return 200", func() {
				req, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
				Expect(err).ToNot(HaveOccurred())
				resp, err := q.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
		})
	})

	Describe("Limiting body size", func() {
		q := common.GetQuicTransport(quicEndpoint)
		Context("with an Ingress limit 1K", func() {
			It("should be ok for 900B", func() {
				buff := bytes.NewBuffer(make([]byte, 900))
				req, err := http.NewRequest(http.MethodGet, "https://example.com/limited", buff)
				Expect(err).ToNot(HaveOccurred())
				resp, err := q.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
			It("should not be ok for 2K", func() {
				buff := bytes.NewBuffer(make([]byte, 2000))
				req, err := http.NewRequest(http.MethodGet, "https://example.com/limited", buff)
				Expect(err).ToNot(HaveOccurred())
				resp, err := q.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Not(Equal(http.StatusOK)))
			})
		})
		Context("without an Ingress limit", func() {
			It("should be ok for 2K", func() {
				buff := bytes.NewBuffer(make([]byte, 2000))
				req, err := http.NewRequest(http.MethodGet, "https://example.com/", buff)
				Expect(err).ToNot(HaveOccurred())
				resp, err := q.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			})
			It("should not be ok for 5K", func() {
				buff := bytes.NewBuffer(make([]byte, 5000))
				req, err := http.NewRequest(http.MethodGet, "https://example.com/", buff)
				Expect(err).ToNot(HaveOccurred())
				resp, err := q.RoundTrip(req)
				Expect(err).ToNot(HaveOccurred())
				Expect(resp.StatusCode).To(Not(Equal(http.StatusOK)))
			})
		})
	})

	Describe("Logging", func() {
		ts := common.GetHttpsTransport(httpsEndpoint)
		It("should log basic information", func() {
			id := uuid.New()
			url := fmt.Sprintf("https://example.com/%s/", id)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			Expect(err).ToNot(HaveOccurred())
			resp, err := ts.RoundTrip(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			ExpectIngressLog("s-ingress", id.String()).To(And(
				HaveLogAttribute("http.request.method", "GET"),
				HaveLogAttributeSet("http.request.id"),
			))
		})
	})

})
