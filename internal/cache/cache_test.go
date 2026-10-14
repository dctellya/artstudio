package cache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/cache"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

var bg = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newCache(t *testing.T, maxEntries int) (*cache.LRU, *clock, *prometheus.Registry) {
	t.Helper()
	env := map[string]string{"CONTACT_EMAIL": "ops@example.com"}
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	require.NoError(t, err)
	cfg.CacheMaxEntries = maxEntries
	cfg.CacheTTLNegative = 5 * time.Second
	cfg.CacheTTLStale = 60 * time.Second
	clk := &clock{t: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	reg := prometheus.NewRegistry()
	return cache.New(cfg, reg, cache.WithClock(clk.Now)), clk, reg
}

func metric(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	var sum float64
	for _, mf := range mfs {
		if mf.GetName() == name {
			for _, m := range mf.GetMetric() {
				sum += m.GetCounter().GetValue() + m.GetGauge().GetValue()
			}
		}
	}
	return sum
}

func TestImplementsPorts(t *testing.T) {
	var _ ports.Cache = (*cache.LRU)(nil)
}

func TestLRUBoundAndEviction(t *testing.T) {
	c, _, reg := newCache(t, 3)
	for _, k := range []string{"a", "b", "c"} {
		c.Set(bg, k, []byte(k), time.Minute)
	}
	_, st := c.Get(bg, "a") // touch a: b is now least recently used
	require.Equal(t, ports.CacheHit, st)
	c.Set(bg, "d", []byte("d"), time.Minute)

	assert.Equal(t, 3, c.Len(), "never grows past CACHE_MAX_ENTRIES")
	_, st = c.Get(bg, "b")
	assert.Equal(t, ports.CacheMiss, st, "least recently used entry evicted")
	for _, k := range []string{"a", "c", "d"} {
		v, st := c.Get(bg, k)
		assert.Equal(t, ports.CacheHit, st, k)
		assert.Equal(t, []byte(k), v)
	}
	assert.EqualValues(t, 1, metric(t, reg, "artstudio_cache_evictions_total"))
	assert.EqualValues(t, 3, metric(t, reg, "artstudio_cache_entries"))
}

func TestTTLHitThenStale(t *testing.T) {
	c, clk, _ := newCache(t, 10)
	c.Set(bg, "k", []byte("v"), 10*time.Second)
	v, st := c.Get(bg, "k")
	assert.Equal(t, ports.CacheHit, st)
	assert.Equal(t, []byte("v"), v)

	clk.Advance(9 * time.Second)
	_, st = c.Get(bg, "k")
	assert.Equal(t, ports.CacheHit, st, "still fresh just before TTL")

	clk.Advance(2 * time.Second) // 11s: expired
	v, st = c.Get(bg, "k")
	assert.Equal(t, ports.CacheStale, st, "expired entry is reported stale, not a hit")
	assert.Equal(t, []byte("v"), v, "stale value is still returned")
}

func TestStaleGoneAfterWindow(t *testing.T) {
	c, clk, _ := newCache(t, 10) // stale window 60s
	c.Set(bg, "k", []byte("v"), 10*time.Second)
	clk.Advance(10*time.Second + 59*time.Second)
	_, st := c.Get(bg, "k")
	assert.Equal(t, ports.CacheStale, st, "inside the stale window")
	clk.Advance(2 * time.Second) // 71s after Set, ttl+stale exceeded
	v, st := c.Get(bg, "k")
	assert.Equal(t, ports.CacheMiss, st)
	assert.Nil(t, v)
	assert.Equal(t, 0, c.Len(), "entry beyond the stale window is dropped")
}

func TestNegativeEntryExpiresAfterNegativeTTL(t *testing.T) {
	c, clk, _ := newCache(t, 10) // negative TTL 5s in test config
	c.SetNegative(bg, "artwork:v1:999")
	v, st := c.Get(bg, "artwork:v1:999")
	assert.Equal(t, ports.CacheNegative, st)
	assert.Nil(t, v)

	clk.Advance(4 * time.Second)
	_, st = c.Get(bg, "artwork:v1:999")
	assert.Equal(t, ports.CacheNegative, st)

	clk.Advance(2 * time.Second)
	_, st = c.Get(bg, "artwork:v1:999")
	assert.Equal(t, ports.CacheMiss, st, "a not-found is never served stale")
}

func TestSingleflightCollapsesConcurrentCalls(t *testing.T) {
	c, _, reg := newCache(t, 10)
	const n = 20
	var fnCalls atomic.Int64
	release := make(chan struct{})
	fn := func() ([]byte, error) {
		fnCalls.Add(1)
		<-release
		return []byte("payload"), nil
	}
	type out struct {
		v      []byte
		shared bool
		err    error
	}
	outs := make(chan out, n)
	var started sync.WaitGroup
	started.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			started.Done()
			v, shared, err := c.Do(bg, "k", fn)
			outs <- out{v, shared, err}
		}()
	}
	started.Wait()
	time.Sleep(100 * time.Millisecond) // let every caller reach Do while fn is blocked
	close(release)

	shared := 0
	for i := 0; i < n; i++ {
		o := <-outs
		require.NoError(t, o.err)
		assert.Equal(t, []byte("payload"), o.v)
		if o.shared {
			shared++
		}
	}
	assert.EqualValues(t, 1, fnCalls.Load(), "N callers, one fn call")
	assert.Equal(t, n-1, shared, "all but the leader report shared=true")
	assert.EqualValues(t, n-1, metric(t, reg, "artstudio_cache_singleflight_dedupes_total"))
}

func TestSingleflightPropagatesError(t *testing.T) {
	c, _, _ := newCache(t, 10)
	boom := errors.New("boom")
	_, _, err := c.Do(bg, "k", func() ([]byte, error) { return nil, boom })
	assert.ErrorIs(t, err, boom)
	v, shared, err := c.Do(bg, "k", func() ([]byte, error) { return []byte("ok"), nil })
	require.NoError(t, err)
	assert.False(t, shared)
	assert.Equal(t, []byte("ok"), v, "a failed flight leaves no state behind")
}

func TestKeyNormalisation(t *testing.T) {
	assert.Equal(t, "monet water lilies", cache.NormalizeQuery("  Monet   WATER\tLilies \n"))
	assert.Equal(t, "", cache.NormalizeQuery("   "))
	assert.Equal(t, "search:v1:monet:10:2", cache.SearchKey(ports.SearchQuery{Q: "  Monet ", Limit: 10, Page: 2}))
	assert.Equal(t,
		cache.SearchKey(ports.SearchQuery{Q: "water   lilies", Limit: 5, Page: 1}),
		cache.SearchKey(ports.SearchQuery{Q: " WATER lilies ", Limit: 5, Page: 1}))
	assert.NotEqual(t,
		cache.SearchKey(ports.SearchQuery{Q: "monet", Limit: 5, Page: 1}),
		cache.SearchKey(ports.SearchQuery{Q: "monet", Limit: 5, Page: 2}))
	assert.Equal(t, "artwork:v1:27992", cache.ArtworkKey(27992))
}
