package artic

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

// Prober probes the vendor every cfg.ProbeInterval and implements the "vendor"
// ports.Checker. It reports but never gates readiness.
type Prober struct {
	v        ports.Vendor
	interval time.Duration
	budget   time.Duration

	up      prometheus.Gauge
	latency prometheus.Gauge

	mu        sync.Mutex
	checked   bool
	healthy   bool
	latencyMS int64
	lastCheck time.Time
	lastError string
}

var _ ports.Checker = (*Prober)(nil)

// NewProber builds a prober for v.
func NewProber(v ports.Vendor, cfg config.Config, reg prometheus.Registerer) *Prober {
	f := promauto.With(reg)
	return &Prober{
		v: v, interval: cfg.ProbeInterval, budget: cfg.UpstreamBudget,
		up: f.NewGauge(prometheus.GaugeOpts{Namespace: "artstudio", Name: "upstream_probe_up",
			Help: "1 when the last vendor probe succeeded."}),
		latency: f.NewGauge(prometheus.GaugeOpts{Namespace: "artstudio", Name: "upstream_probe_latency_seconds",
			Help: "Latency of the last vendor probe."}),
	}
}

// Run probes immediately and then every interval until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		p.CheckOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CheckOnce probes the vendor and records the outcome.
func (p *Prober) CheckOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, p.budget)
	defer cancel()
	call, err := p.v.Probe(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	p.checked, p.healthy = true, err == nil
	p.latencyMS = call.Latency.Milliseconds()
	p.lastCheck = time.Now()
	p.lastError = ""
	p.up.Set(0)
	if err != nil {
		p.lastError = err.Error()
	} else {
		p.up.Set(1)
	}
	p.latency.Set(call.Latency.Seconds())
}

// Name is "vendor".
func (*Prober) Name() string { return "vendor" }

// Check reports the last probe; OK is always true because the vendor never gates /ready.
func (p *Prober) Check(context.Context) ports.CheckResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := "unknown"
	if p.checked {
		state = map[bool]string{true: "up", false: "down"}[p.healthy]
	}
	return ports.CheckResult{OK: true, Detail: map[string]any{
		"state":      state,
		"latency_ms": p.latencyMS,
		"last_check": p.lastCheck.UTC().Format(time.RFC3339),
		"last_error": p.lastError,
		"breaker":    p.v.BreakerState(),
	}}
}
