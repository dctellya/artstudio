//go:build integration

package artic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic"
	"github.com/dctellya/artstudio/internal/artic/stubserver"
	"github.com/dctellya/artstudio/internal/cache"
	"github.com/dctellya/artstudio/internal/lookup"
	"github.com/dctellya/artstudio/internal/ports"
)

type itClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *itClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *itClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type nopAuditor struct{}

func (nopAuditor) Record(ports.VendorCall)     {}
func (nopAuditor) Close(context.Context) error { return nil }

func ctl(t *testing.T, base, fault string) {
	t.Helper()
	resp, err := http.Post(base+"/__ctl?fault="+fault, "", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.True(t, resp.StatusCode >= 200 && resp.StatusCode < 300, "ctl status %d", resp.StatusCode)
}

// Client against the stub server: 503 -> retries -> breaker open -> stale.
func TestVendorFaultRetriesBreakerOpenThenStale(t *testing.T) {
	srv := httptest.NewServer(stubserver.New())
	t.Cleanup(srv.Close)

	cfg := testConfig(t, srv.URL, nil)
	cfg.BreakerConsecutive = 5
	cfg.BreakerOpenTimeout = 200 * time.Millisecond
	cfg.CacheTTLSearch = 10 * time.Second
	cfg.CacheTTLArtwork = 10 * time.Second
	cfg.CacheTTLStale = 60 * time.Second

	reg := prometheus.NewRegistry()
	vendor, err := artic.New(cfg, reg)
	require.NoError(t, err)
	clk := &itClock{t: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	c := cache.New(cfg, prometheus.NewRegistry(), cache.WithClock(clk.Now))
	svc := lookup.New(vendor, c, nopAuditor{}, cfg, prometheus.NewRegistry())
	ctx := ports.WithRequestID(context.Background(), "it-1")

	// Healthy: miss, then hit.
	q := ports.SearchQuery{Q: "monet", Limit: 10, Page: 1}
	res, meta, err := svc.Search(ctx, q)
	require.NoError(t, err)
	require.NotEmpty(t, res.Items, "stub fixtures answer the search")
	assert.Equal(t, ports.CacheMiss, meta.Cache)
	_, meta, err = svc.Search(ctx, q)
	require.NoError(t, err)
	assert.Equal(t, ports.CacheHit, meta.Cache)

	// Vendor starts failing with 503: the client retries, then gives up.
	ctl(t, srv.URL, "503")
	_, call, err := vendor.Search(ctx, ports.SearchQuery{Q: "other", Limit: 10, Page: 1})
	require.ErrorIs(t, err, ports.ErrBadUpstream)
	assert.Equal(t, 3, call.Attempts, "1 try + 2 retries")
	assert.Equal(t, 503, call.Status)

	// Keep failing until the breaker opens (5 consecutive failures).
	for i := 0; i < 5 && vendor.BreakerState() != "open"; i++ {
		_, _, _ = vendor.Search(ctx, ports.SearchQuery{Q: "other", Limit: 10, Page: 1})
	}
	require.Equal(t, "open", vendor.BreakerState())
	_, _, err = vendor.Search(ctx, ports.SearchQuery{Q: "other", Limit: 10, Page: 1})
	require.ErrorIs(t, err, ports.ErrBreakerOpen)

	// The cached entry expires; with the breaker open the lookup serves it stale.
	clk.Advance(11 * time.Second)
	res2, meta, err := svc.Search(ctx, q)
	require.NoError(t, err)
	assert.Equal(t, ports.CacheStale, meta.Cache)
	assert.Equal(t, res, res2)

	// Recovery: fault cleared, breaker half-opens after its timeout and closes.
	ctl(t, srv.URL, "clear")
	require.Eventually(t, func() bool {
		_, _, err := vendor.Search(ctx, ports.SearchQuery{Q: "other", Limit: 10, Page: 1})
		return err == nil
	}, 3*time.Second, 50*time.Millisecond)
	// Half-open needs BreakerHalfOpenMax successful trials to close.
	for i := 0; i < cfg.BreakerHalfOpenMax; i++ {
		_, _, err := vendor.Search(ctx, ports.SearchQuery{Q: "other", Limit: 10, Page: 1})
		require.NoError(t, err)
	}
	assert.Equal(t, "closed", vendor.BreakerState())
}
