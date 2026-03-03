package common

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/netip"

	_ "embed"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

func GetHttpTransport(httpAddr netip.AddrPort) *http.Transport {
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, httpAddr.String())
		},
		DisableKeepAlives: true,
	}
	return transport
}

//go:embed certs/root.crt
var rootCaCertRaw []byte

func GetHttpsTransport(httpsAddr netip.AddrPort) *http.Transport {
	rootCas := x509.NewCertPool()
	rootCas.AppendCertsFromPEM(rootCaCertRaw)
	netDialer := &net.Dialer{}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := netDialer.DialContext(ctx, network, httpsAddr.String())
			if err != nil {
				return nil, fmt.Errorf("failed to dial https endpoint: %w", err)
			}
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("failed to split host and port: %v", err)
			}
			return tls.Client(conn, &tls.Config{
				RootCAs:    rootCas,
				ServerName: host,
			}), nil
		},
		TLSClientConfig: &tls.Config{},
	}
	return transport
}

func GetQuicTransport(quicAddr netip.AddrPort) *http3.Transport {
	rootCas := x509.NewCertPool()
	rootCas.AppendCertsFromPEM(rootCaCertRaw)

	h3tr := &http3.Transport{
		//TLSClientConfig: &tls.Config{},  // set a TLS client config, if desired
		//QUICConfig: &quic.Config{}, // QUIC connection options
		Dial: func(ctx context.Context, addr string, tlsConf *tls.Config, quicConf *quic.Config) (*quic.Conn, error) {
			addrUdp := &net.UDPAddr{IP: quicAddr.Addr().AsSlice(), Port: int(quicAddr.Port())}
			conn, err := net.ListenUDP("udp", nil)
			if err != nil {
				return nil, fmt.Errorf("failed to dial quic endpoint: %w", err)
			}

			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("failed to split host and port: %v", err)
			}

			return quic.Dial(
				ctx,
				conn,
				addrUdp,
				&tls.Config{
					ServerName: host,
					RootCAs:    rootCas,
					NextProtos: []string{"h3"},
				},
				&quic.Config{},
			)
		},
	}

	return h3tr
}
