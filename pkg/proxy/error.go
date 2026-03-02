package proxy

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
)

// ErrorPageData holds the data used to render custom error pages.
type ErrorPageData struct {
	Status     int
	StatusText string
	Message    string

	URL       *url.URL
	Host      string
	RequestID string

	RealIP netip.Addr
}

func (r *RequestContext) HandleErrorf(status int, format string, args ...any) error {
	errStr := fmt.Sprintf(format, args...)
	r.Log.Error("proxy error", "status", status, "error", errStr)
	return r.handleErrorPage(status, "")
}

func (r *RequestContext) handleErrorPage(status int, msg string) error {
	r.W.WriteHeader(status)

	errPage := r.routingConfig.ErrorPage
	if errPage == nil {
		r.Log.Error("missing error page config")
		_, err := r.W.Write([]byte(msg))
		return err
	}

	return r.routingConfig.ErrorPage.Execute(r.W, ErrorPageData{
		Status:     status,
		StatusText: http.StatusText(status),
		Message:    msg,
		URL:        r.R.URL,
		Host:       r.R.Host,
		RequestID:  r.RequestId,
		RealIP:     r.RealIp,
	})
}

func (r *RequestContext) BadGateway(msg string) error {
	return r.handleErrorPage(http.StatusBadGateway, msg)
}

func (r *RequestContext) BadRequest(msg string) error {
	return r.handleErrorPage(http.StatusBadRequest, msg)
}

func (r *RequestContext) Forbidden(msg string) error {
	return r.handleErrorPage(http.StatusForbidden, msg)
}

func (r *RequestContext) Unauthorized(msg string) error {
	return r.handleErrorPage(http.StatusUnauthorized, msg)
}

func (r *RequestContext) NotFound(msg string) error {
	return r.handleErrorPage(http.StatusNotFound, msg)
}
