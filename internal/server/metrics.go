package server

import (
	"github.com/prometheus/client_golang/prometheus"
)

// httpBuckets are the SPEC §5 request-duration buckets, in seconds.
var httpBuckets = []float64{.005, .01, .02, .05, .1, .2, .4, .8, 1.6, 3.2, 5}

type httpMetrics struct {
	requests    *prometheus.CounterVec
	duration    *prometheus.HistogramVec
	inFlight    prometheus.Gauge
	shed        prometheus.Counter
	rateLimited prometheus.Counter
}

func newHTTPMetrics(reg prometheus.Registerer) (*httpMetrics, error) {
	m := &httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "artstudio", Name: "http_requests_total", Help: "HTTP requests by route, method and status code.",
		}, []string{"route", "method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "artstudio", Name: "http_request_duration_seconds", Help: "HTTP request duration by route and cache result.",
			Buckets: httpBuckets,
		}, []string{"route", "cache"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "artstudio", Name: "http_in_flight", Help: "Requests currently being served.",
		}),
		shed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "artstudio", Name: "http_shed_total", Help: "Requests rejected because MAX_IN_FLIGHT was reached.",
		}),
		rateLimited: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "artstudio", Name: "http_rate_limited_total", Help: "Requests rejected by the per-client limiter.",
		}),
	}
	for _, c := range []prometheus.Collector{m.requests, m.duration, m.inFlight, m.shed, m.rateLimited} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}
