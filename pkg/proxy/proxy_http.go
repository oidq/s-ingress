package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

func (p *Proxy) startQuic(ctx context.Context, errChan chan<- error) error {
	server := &http3.Server{
		Addr:    ":8444",
		Handler: p,
		TLSConfig: &tls.Config{
			GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				conf := p.routingConfig.Load()
				if conf == nil {
					return nil, fmt.Errorf("no routing configuration")
				}

				certificate, ok := conf.TlsCertificates[clientHello.ServerName]
				if ok {
					return certificate, nil
				}

				return conf.DefaultTlsCertificate, nil
			},
		},
		Logger: p.log,
	}

	go func() {
		serveErr := serveQuic(ctx, server, p.config.GracefulTimeout)
		errChan <- wrapErr("quic", serveErr)
	}()

	return nil
}

func (p *Proxy) startHttps(ctx context.Context, errChan chan<- error) error {
	server := &http.Server{
		Handler: p,
	}

	listener, err := tls.Listen("tcp", ":8443", &tls.Config{
		GetCertificate: func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			conf := p.routingConfig.Load()
			if conf == nil {
				return nil, fmt.Errorf("no routing configuration")
			}

			certificate, ok := conf.TlsCertificates[clientHello.ServerName]
			if ok {
				return certificate, nil
			}

			return conf.DefaultTlsCertificate, nil
		},
		NextProtos: []string{"h2"},
	})
	if err != nil {
		return fmt.Errorf("cannot listen for https: %w", err)
	}

	go func() {
		serveErr := serve(ctx, listener, server, p.config.GracefulTimeout)
		errChan <- wrapErr("https", serveErr)
	}()

	return nil
}

func (p *Proxy) startHttp(ctx context.Context, errChan chan<- error) error {
	server := &http.Server{
		Handler: p,
	}

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		return fmt.Errorf("cannot listen for http: %w", err)
	}

	go func() {
		serveErr := serve(ctx, listener, server, p.config.GracefulTimeout)
		errChan <- wrapErr("http", serveErr)
	}()

	return nil
}

func (p *Proxy) startStatus(ctx context.Context, errChan chan<- error) error {
	server := &http.Server{
		Handler: p.getStatusHandler(),
	}

	listener, err := net.Listen("tcp", ":8000")
	if err != nil {
		return fmt.Errorf("cannot listen for http: %w", err)
	}

	go func() {
		serveErr := serve(ctx, listener, server, p.config.GracefulTimeout)
		errChan <- wrapErr("status", serveErr)
	}()

	return nil
}

type httpServer[L any] interface {
	Serve(L) error
	Shutdown(ctx context.Context) error
}

func serve[L io.Closer](ctx context.Context, listener L, server httpServer[L], gracefulTimeout time.Duration) error {
	var err error

	errChan := make(chan error)
	go func() {
		err = server.Serve(listener)
		errChan <- err
	}()

	defer func() {
		_ = listener.Close()
	}()

	select {
	case <-ctx.Done(): // shutdown initialized
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulTimeout)
		err = server.Shutdown(shutdownCtx)
		cancel()
		serveErr := <-errChan // we should receive ErrServerClosed
		if serveErr != nil && !isServerClosedError(serveErr) {
			return fmt.Errorf("server returned error while closing: %w (%w)", serveErr, err)
		}
		if err != nil {
			return fmt.Errorf("server shutdown failed: %w", err)
		}
	case err = <-errChan: // start error received
		if err != nil {
			return fmt.Errorf("server start failed: %w", err)
		}
	}

	return nil
}

func isServerClosedError(err error) bool {
	return errors.Is(err, http.ErrServerClosed) || errors.As(err, &quic.ErrServerClosed)
}

func serveQuic(ctx context.Context, server *http3.Server, gracefulTimeout time.Duration) error {
	var err error

	errChan := make(chan error)
	go func() {
		err = server.ListenAndServe()
		errChan <- err
	}()

	select {
	case <-ctx.Done(): // shutdown initialized
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulTimeout)
		err = server.Shutdown(shutdownCtx)
		cancel()
		serveErr := <-errChan // we should receive ErrServerClosed
		if serveErr != nil && !isServerClosedError(serveErr) {
			return fmt.Errorf("server returned error while closing: %w (%w)", serveErr, err)
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("quic server shutdown failed: %w", err)
		}
	case err = <-errChan: // start error received
		if err != nil {
			return fmt.Errorf("server start failed: %w", err)
		}
	}

	return nil
}
