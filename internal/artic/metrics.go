package artic

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// durationBuckets cover a vendor call from a fast hit to the full budget.
var durationBuckets = []float64{.01, .025, .05, .1, .2, .4, .8, 1.6, 3.2, 5}

type metrics struct {
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	retries        *prometheus.CounterVec
	breakerState   prometheus.Gauge
	transitions    *prometheus.CounterVec
	limiterWait    prometheus.Histogram
	limiterReject  prometheus.Counter
	bulkheadInUse  prometheus.Gauge
	bulkheadReject prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) *metrics {
	f := promauto.With(reg)
	const ns = "artstudio"
	return &metrics{
		requests: f.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "upstream_requests_total",
			Help: "Vendor HTTP attempts."}, []string{"operation", "status", "attempt"}),
		duration: f.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "upstream_request_duration_seconds",
			Help: "Vendor call duration, all attempts.", Buckets: durationBuckets}, []string{"operation"}),
		retries: f.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "upstream_retries_total",
			Help: "Vendor retries by reason."}, []string{"operation", "reason"}),
		breakerState: f.NewGauge(prometheus.GaugeOpts{Namespace: ns, Name: "upstream_breaker_state",
			Help: "Breaker state: 0 closed, 1 half-open, 2 open."}),
		transitions: f.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "upstream_breaker_transitions_total",
			Help: "Breaker state transitions."}, []string{"from", "to"}),
		limiterWait: f.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Name: "upstream_limiter_wait_seconds",
			Help: "Time spent waiting for the rate limiter.", Buckets: durationBuckets}),
		limiterReject: f.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: "upstream_limiter_rejected_total",
			Help: "Calls rejected because the limiter wait exceeded the maximum."}),
		bulkheadInUse: f.NewGauge(prometheus.GaugeOpts{Namespace: ns, Name: "upstream_bulkhead_in_use",
			Help: "Concurrent vendor calls."}),
		bulkheadReject: f.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: "upstream_bulkhead_rejected_total",
			Help: "Calls rejected because the bulkhead was full."}),
	}
}
