package proxy

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	labelIngress = "ingress"
	labelDomain  = "domain"
	labelStatus  = "status"
	labelPath    = "path"
)

type metrics struct {
	reg              *prometheus.Registry
	requestDurations *prometheus.HistogramVec
	requestTotals    *prometheus.CounterVec
}

func (p *Proxy) WithMetrics(reg *prometheus.Registry) {
	p.metrics = &metrics{
		reg: reg,
		requestDurations: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request durations per ingress",
		}, []string{labelIngress}),
		requestTotals: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "http_request_total",
			Help: "HTTP request totals",
		}, []string{labelIngress, labelDomain, labelPath, labelStatus}),
	}
}

func (m *metrics) recordRequest(rCtx *RequestContext, duration time.Duration) {
	if m == nil {
		return
	}

	ingressName := "-"
	path := "-"
	if rCtx.MatchedRoute != nil {
		ingressName = rCtx.MatchedRoute.IngressName
		path = rCtx.MatchedRoute.Path
	}

	m.requestTotals.With(prometheus.Labels{
		labelIngress: ingressName,
		labelDomain:  rCtx.R.Host,
		labelStatus:  strconv.Itoa(rCtx.writer.writtenStatusCode),
		labelPath:    path,
	}).Inc()

	m.requestDurations.With(prometheus.Labels{
		labelIngress: ingressName,
	}).Observe(duration.Seconds())
}
