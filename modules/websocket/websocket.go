package websocket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"codeberg.org/oidq/s-ingress/pkg/proxy"
	"github.com/gorilla/websocket"
	netv1 "k8s.io/api/networking/v1"
)

type websocketModule struct {
	config.Module
}

func ModuleWebsocket(config *config.ControllerConf) (config.ModuleInstance, error) {
	return &websocketModule{}, nil
}

func (wm *websocketModule) IngressMiddleware(reconciler config.IngressReconciler, ingress *netv1.Ingress) (proxy.MiddlewareFunc, error) {
	return func(rCtx *proxy.RequestContext, next proxy.NextFunc) error {
		if isWebSocketConnection(rCtx) {
			return handleWebSocket(rCtx, rCtx.UpstreamRequest.URL)
		}

		return next(rCtx)
	}, nil
}

func isWebSocketConnection(rCtx *proxy.RequestContext) bool {
	return websocket.IsWebSocketUpgrade(rCtx.R)
}

func handleWebSocket(rCtx *proxy.RequestContext, upstreamUrl *url.URL) error {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
	}

	netDialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	dialer := &websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return netDialer.DialContext(ctx, network, rCtx.MatchedRoute.Endpoint.String())
		},
	}

	upstreamUrl.Scheme = "ws"
	upstreamConnection, resp, err := dialer.DialContext(rCtx.R.Context(), upstreamUrl.String(), constructWsHeaders(rCtx.R.Header))
	switch {
	case errors.Is(err, websocket.ErrBadHandshake):
		_ = resp
		// TODO: proxy the response back
		return err
	case err != nil:
		return fmt.Errorf("websocket upgrader dial failed: %w", err)
	}

	downstreamConnection, err := upgrader.Upgrade(rCtx.W, rCtx.R, nil)
	if err != nil {
		return fmt.Errorf("websocket upgrader failed: %w", err)
	}

	// pass ping/pong/close transparently from upstream
	upstreamConnection.SetPingHandler(func(appData string) error {
		return downstreamConnection.WriteMessage(websocket.PingMessage, []byte(appData))
	})
	upstreamConnection.SetPongHandler(func(appData string) error {
		return downstreamConnection.WriteMessage(websocket.PongMessage, []byte(appData))
	})
	upstreamConnection.SetCloseHandler(func(code int, text string) error {
		return downstreamConnection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
	})

	// pass ping/pong/close transparently from downstream
	downstreamConnection.SetPingHandler(func(appData string) error {
		return upstreamConnection.WriteMessage(websocket.PingMessage, []byte(appData))
	})
	downstreamConnection.SetPongHandler(func(appData string) error {
		return upstreamConnection.WriteMessage(websocket.PongMessage, []byte(appData))
	})
	downstreamConnection.SetCloseHandler(func(code int, text string) error {
		return upstreamConnection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
	})

	rCtx.Log.Info("websocket connection upgraded")

	var wg sync.WaitGroup
	connCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg.Go(func() {
		err := proxySocket(upstreamConnection, downstreamConnection)
		if err != nil {
			rCtx.Log.Error("websocket proxy from upstream failed", slog.String("err", err.Error()))
		}
		cancel()
	})
	wg.Go(func() {
		err := proxySocket(downstreamConnection, upstreamConnection)
		if err != nil {
			rCtx.Log.Error("websocket proxy from downstream failed", slog.String("err", err.Error()))
		}
		cancel()
	})

	select {
	case <-connCtx.Done(): // error, or one party closed connection
		rCtx.Log.Warn("websocket connection closed by remote host")
	case <-rCtx.ServerCtx.Done(): // server shutdown initialized
		// TODO - handle the close better
	}

	_ = upstreamConnection.Close()
	_ = downstreamConnection.Close()

	wg.Wait()

	return nil
}

func proxySocket(in *websocket.Conn, out *websocket.Conn) error {
	for {
		msgType, msg, err := in.ReadMessage()
		switch {
		case errors.Is(err, net.ErrClosed):
			return nil
		case err != nil:
			return fmt.Errorf("websocket read message failed: %w", err)
		}

		err = out.WriteMessage(msgType, msg)
		if err != nil {
			return fmt.Errorf("websocket write message failed: %w", err)
		}
	}
}

func constructWsHeaders(requestHeader http.Header) http.Header {
	var newHeaders = make(http.Header)

	for key, values := range requestHeader {
		lowerKey := strings.ToLower(key)
		if lowerKey == "upgrade" ||
			lowerKey == "connection" ||
			lowerKey == "sec-websocket-key" ||
			lowerKey == "sec-websocket-version" ||
			lowerKey == "sec-websocket-extensions" {

			continue
		}

		newHeaders[key] = values
	}

	return newHeaders
}
