package proxy

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/idna"
)

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	requestId := getRequestId(r)
	remoteIp := getRemoteIp(r)

	l := p.log.With(
		slog.String("network.peer.address", remoteIp.Addr().String()),
		slog.Uint64("network.peer.port", uint64(remoteIp.Port())),

		slog.String("http.request.method", r.Method),
		slog.String("http.request.id", requestId),

		slog.String("url.domain", r.Host),
		slog.String("url.path", r.URL.Path),

		slog.String("network.protocol.name", "http"),
		slog.String("network.protocol.version", strconv.Itoa(r.ProtoMajor)),
	)

	routingConfig := p.routingConfig.Load()
	if routingConfig == nil { // unavailable before first reconfigure
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	writer := &ResponseWriter{ResponseWriter: w}
	rCtx := &RequestContext{
		R:              r,
		W:              writer,
		ServerCtx:      p.serverCtx,
		RequestId:      requestId,
		Log:            l,
		RemoteIp:       remoteIp,
		writer:         writer,
		originalWriter: w,
		routingConfig:  routingConfig,
	}

	fillRealIp(rCtx)
	rCtx.Log = rCtx.Log.With(
		slog.String("client.address", rCtx.RealIp.String()),
	)

	err := callMiddleware(rCtx, routingConfig.RequestMiddlewares, handleHttp)
	if err != nil {
		l.Log(rCtx, slog.LevelError, "failed to proxy", slog.String("err", err.Error()))
	}

	duration := time.Since(t0)
	p.metrics.recordRequest(rCtx, duration)
	rCtx.Log.Log(rCtx, slog.LevelInfo,
		fmt.Sprintf("%s %s - %d (%s)", r.Method, r.URL.Path, writer.writtenStatusCode, duration),
		slog.Int("http.response.status_code", writer.writtenStatusCode),
	)
}

func getRequestId(r *http.Request) string {
	headersReqId := r.Header.Get("X-Request-Id")
	if headersReqId != "" {
		return headersReqId
	}

	return uuid.New().String()
}

func handleHttp(rCtx *RequestContext) error {
	escapedHost, err := idna.Punycode.ToASCII(rCtx.R.Host)
	if err != nil {
		return rCtx.BadRequest("invalid hostname")
	}

	host := rCtx.routingConfig.LookupHost(escapedHost)
	if host != nil {
		return handleHost(rCtx, host)
	}

	return rCtx.NotFound("host not found")
}

func handleHost(rCtx *RequestContext, host *HostConfig) error {
	route := host.LookupRoute(rCtx.R.URL.Path)
	if route != nil {
		return proxyRoute(rCtx, route)
	}

	return rCtx.NotFound("route not found")
}

// proxyRoute handles the proxying to the endpoint and returns an error.
// The response is always written after calling this function, so there is no need to try.
func proxyRoute(rCtx *RequestContext, endpoint *RouteConfig) error {
	rCtx.Log = rCtx.Log.With(
		slog.String("http.route", endpoint.Path),
		slog.String("http.ingress", endpoint.IngressName),
	)

	u, err := getUpstreamUrl(rCtx)
	if err != nil {
		return err
	}

	var writer io.ReadCloser
	if rCtx.routingConfig.MaxBodySize > 0 {
		writer = http.MaxBytesReader(rCtx.originalWriter, rCtx.R.Body, rCtx.routingConfig.MaxBodySize)
	} else {
		writer = rCtx.R.Body
	}
	defer writer.Close()

	proxiedR, err := http.NewRequestWithContext(rCtx.R.Context(), rCtx.R.Method, u.String(), writer)
	if err != nil {
		return rCtx.HandleErrorf(http.StatusInternalServerError, "failed to create upstream request: %v", err)
	}

	proxiedR.ContentLength = rCtx.R.ContentLength
	proxiedR.Header = rCtx.R.Header.Clone()
	proxiedR.Header.Set("X-Request-Id", rCtx.RequestId)
	proxiedR.Header.Set("X-Forwarded-For", rCtx.RemoteIp.String())
	proxiedR.Header.Set("X-Forwarded-Host", rCtx.R.Host)
	if rCtx.R.TLS != nil {
		proxiedR.Header.Set("X-Forwarded-Proto", "https")
	} else {
		proxiedR.Header.Set("X-Forwarded-Proto", "https")
	}

	rCtx.UpstreamAddress = endpoint.Endpoint
	rCtx.UpstreamRequest = proxiedR
	rCtx.MatchedRoute = endpoint

	return callMiddleware(rCtx, rCtx.MatchedRoute.Middlewares, proxy)
}

func callMiddleware(rCtx *RequestContext, middlewares []MiddlewareFunc, fallbackF func(ctx *RequestContext) error) error {
	if len(middlewares) == 0 {
		return fallbackF(rCtx)
	}

	middleware := middlewares[0]
	return middleware(rCtx, func(rCtx *RequestContext) error {
		return callMiddleware(rCtx, middlewares[1:], fallbackF)
	})
}

func proxy(rCtx *RequestContext) error {
	var maxBytesError *http.MaxBytesError

	if rCtx.UpstreamRequest == nil {
		return fmt.Errorf("no upstream request to proxy")
	}

	proto := http.Protocols{}
	proto.SetHTTP1(true)
	transport := http.Transport{
		DisableKeepAlives: true,
		Protocols:         &proto,
	}
	conn, err := transport.NewClientConn(rCtx, "http", rCtx.MatchedRoute.Endpoint.String())
	if err != nil {
		return rCtx.HandleErrorf(http.StatusBadGateway, "new client conn: %s", err)
	}
	defer conn.Close()

	resp, err := conn.RoundTrip(rCtx.UpstreamRequest)
	switch {
	case errors.As(err, &maxBytesError):
		rCtx.W.WriteHeader(http.StatusRequestEntityTooLarge)
		return nil
	case err != nil:
		return rCtx.HandleErrorf(http.StatusBadGateway, "failed to proxy")
	}

	defer resp.Body.Close()

	setResponseHeaders(rCtx, resp.Header)
	rCtx.W.WriteHeader(resp.StatusCode)
	_, err = io.Copy(rCtx.W, resp.Body)
	switch {
	case errors.Is(err, syscall.EPIPE):
		rCtx.Log.Warn("broken pipe", slog.String("err", err.Error()))
		return nil
	case errors.Is(err, syscall.ECONNRESET):
		return nil
	case err != nil:
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}

func setResponseHeaders(rCtx *RequestContext, respHeader http.Header) {
	rCtx.W.Header().Set("X-Request-Id", rCtx.RequestId)
	rCtx.W.Header().Set("Alt-Svc", "h3=\":443\"; ma=2592000")
	for header := range respHeader {
		if header == http.CanonicalHeaderKey("keep-alive") {
			continue
		}
		rCtx.W.Header().Del(header)
		values := respHeader.Values(header)
		for _, value := range values {
			rCtx.W.Header().Add(header, value)
		}
	}

	for header := range rCtx.responseHeader {
		rCtx.W.Header().Set(header, rCtx.responseHeader.Get(header))
	}
}

func getUpstreamUrl(rCtx *RequestContext) (*url.URL, error) {
	upstreamUrl := *rCtx.R.URL
	upstreamUrl.Host = rCtx.R.Host
	upstreamUrl.Scheme = "http"

	return &upstreamUrl, nil
}
