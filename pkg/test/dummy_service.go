package test

import (
	"net"
	"net/http"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

type dummyService struct {
	handler http.Handler

	hostPortAddr netip.AddrPort
}

func (ds *dummyService) start(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}

	ds.hostPortAddr, err = netip.ParseAddrPort(listener.Addr().String())
	require.NoError(t, err)
	server := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ds.handler == nil {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
				return
			}

			ds.handler.ServeHTTP(w, r)
		}),
	}

	t.Cleanup(func() {
		_ = server.Shutdown(t.Context())
	})

	go func() {
		err = server.Serve(listener)
		require.ErrorIs(t, err, http.ErrServerClosed)
	}()
}

func (ds *dummyService) hostPort() netip.AddrPort {
	if !ds.hostPortAddr.IsValid() {
		panic("dummy service not started")
	}
	return ds.hostPortAddr
}
