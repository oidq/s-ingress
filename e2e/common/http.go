package common

import (
	"io"
	"net/http"
	"net/netip"

	. "github.com/onsi/gomega"
)

var httpTransport http.RoundTripper
var httpsTransport http.RoundTripper
var quicTransport http.RoundTripper

func SetupTransports(httpEndpoint, httpsEndpoint, quicEndpoint netip.AddrPort) {
	httpTransport = GetHttpTransport(httpEndpoint)
	httpsTransport = GetHttpsTransport(httpsEndpoint)
	quicTransport = GetQuicTransport(quicEndpoint)
}

func NewRequest(method, url string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, url, body)
	Expect(err).ToNot(HaveOccurred(), "failed to create request")
	return req
}

func RoundTripHttp(req *http.Request, optionalDescription ...any) *http.Response {
	resp, err := httpTransport.RoundTrip(req)
	Expect(err).ToNot(HaveOccurred(), optionalDescription...)

	return resp
}

func RoundTripHttps(req *http.Request, optionalDescription ...any) *http.Response {
	resp, err := httpsTransport.RoundTrip(req)
	Expect(err).ToNot(HaveOccurred(), optionalDescription...)

	return resp
}

func RoundTripQuic(req *http.Request, optionalDescription ...any) *http.Response {
	resp, err := quicTransport.RoundTrip(req)
	Expect(err).ToNot(HaveOccurred(), optionalDescription...)

	return resp
}
