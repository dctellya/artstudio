package artic_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic"
	"github.com/dctellya/artstudio/internal/ports"
)

type fakeVendor struct {
	probes  atomic.Int64
	probeFn func() (ports.Call, error)
	breaker string
}

func (f *fakeVendor) Search(context.Context, ports.SearchQuery) (ports.SearchResult, ports.Call, error) {
	return ports.SearchResult{}, ports.Call{}, nil
}

func (f *fakeVendor) Artwork(context.Context, int) (ports.Artwork, ports.Call, error) {
	return ports.Artwork{}, ports.Call{}, nil
}

func (f *fakeVendor) Probe(context.Context) (ports.Call, error) {
	f.probes.Add(1)
	return f.probeFn()
}
func (f *fakeVendor) BreakerState() string { return f.breaker }

func TestProberUpdatesState(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	fv := &fakeVendor{breaker: "closed", probeFn: func() (ports.Call, error) {
		if healthy.Load() {
			return ports.Call{Operation: "probe", Status: 200, Latency: 12 * time.Millisecond}, nil
		}
		return ports.Call{Operation: "probe", Status: 503, Err: ports.ErrBadUpstream}, errors.New("boom 503")
	}}
	reg := prometheus.NewRegistry()
	p := artic.NewProber(fv, testConfig(t, "http://unused", nil), reg)
	var _ ports.Checker = p
	assert.Equal(t, "vendor", p.Name())

	p.CheckOnce(bg)
	res := p.Check(bg)
	assert.True(t, res.OK)
	assert.Equal(t, "up", res.Detail["state"])
	assert.Equal(t, "closed", res.Detail["breaker"])
	for _, k := range []string{"latency_ms", "last_check", "last_error"} {
		assert.Contains(t, res.Detail, k)
	}
	assert.EqualValues(t, 1, metric(t, reg, "artstudio_upstream_probe_up", nil))

	healthy.Store(false)
	fv.breaker = "open"
	p.CheckOnce(bg)
	res = p.Check(bg)
	assert.True(t, res.OK, "the vendor never gates readiness")
	assert.Equal(t, "down", res.Detail["state"])
	assert.Equal(t, "open", res.Detail["breaker"])
	assert.Contains(t, res.Detail["last_error"], "boom")
	assert.EqualValues(t, 0, metric(t, reg, "artstudio_upstream_probe_up", nil))
}

func TestProberRunLoopsUntilCancelled(t *testing.T) {
	fv := &fakeVendor{breaker: "closed", probeFn: func() (ports.Call, error) { return ports.Call{Status: 200}, nil }}
	p := artic.NewProber(fv, testConfig(t, "http://unused", nil), prometheus.NewRegistry()) // ProbeInterval 20ms
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return fv.probes.Load() >= 3 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
