package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

type NextFunc func(rCtx *RequestContext) error
type MiddlewareFunc func(rCtx *RequestContext, next NextFunc) error

// RequestContext holds data of a single request while also serving as [context.Context].
type RequestContext struct {
	R *http.Request
	W http.ResponseWriter

	// Log is [slog.Logger] instance for the given request enhanced with variables as request id, method, or path.
	Log *slog.Logger

	// RequestId is the unique id of the request.
	RequestId string
	// RemoteIp is the address of the network client, see [RequestContext.RealIp].
	RemoteIp netip.AddrPort
	// RealIp is the address of the client after resolving any proxy headers (if applicable).
	RealIp netip.Addr
	// ServerCtx is closed on shutdown. It is expected that any long-running connections (websockets) will initiate
	// shutdown based on cancellation of this context.
	ServerCtx context.Context

	// All the properties below are initialized after the initial routing decision, which
	// means they are unavailable to ConnectionMiddlewares.

	// MatchedRoute is the RouteConfig based on which this request will be processed.
	// This information is not available to
	MatchedRoute *RouteConfig
	// UpstreamRequest is the request that would be sent by the default proxying behavior. It is available
	// to the middlewares to inspect the final destination and/or change the request parameters based on other
	// decisions.
	UpstreamRequest *http.Request
	// UpstreamAddress is the address of the upstream to which the request is going to be proxied.
	UpstreamAddress netip.AddrPort

	routingConfig  *RoutingConfig
	writer         *ResponseWriter
	originalWriter http.ResponseWriter
	responseHeader http.Header
}

func (r *RequestContext) Deadline() (deadline time.Time, ok bool) {
	return r.R.Context().Deadline()
}

func (r *RequestContext) Done() <-chan struct{} {
	return r.R.Context().Done()
}

func (r *RequestContext) Err() error {
	return r.R.Context().Err()
}

func (r *RequestContext) Value(key any) any {
	return r.R.Context().Value(key)
}

func (r *RequestContext) ResponseHeader() http.Header {
	if r.responseHeader == nil {
		r.responseHeader = make(http.Header)
	}

	return r.responseHeader
}

type ResponseWriter struct {
	http.ResponseWriter

	writtenStatusCode int
	wroteBody         bool
}

func (w *ResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *ResponseWriter) Write(data []byte) (int, error) {
	w.wroteBody = true
	return w.ResponseWriter.Write(data)
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.writtenStatusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}
