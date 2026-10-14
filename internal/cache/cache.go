// Package cache is the in-memory byte cache: LRU bound, per-entry TTL, a stale
// window, negative entries and singleflight (SPEC §5 Cache keys).
package cache

import (
	"context"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/singleflight"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

type entry struct {
	val        []byte
	negative   bool
	expires    time.Time // fresh until
	staleUntil time.Time // servable as stale until
}

// LRU implements ports.Cache.
type LRU struct {
	cfg   config.Config
	now   func() time.Time
	items *lru.Cache[string, entry]
	group singleflight.Group

	requests  *prometheus.CounterVec
	evictions prometheus.Counter
	dedupes   prometheus.Counter
}

// Option customises an LRU.
type Option func(*LRU)

// WithClock replaces the time source (tests).
func WithClock(now func() time.Time) Option { return func(c *LRU) { c.now = now } }

var _ ports.Cache = (*LRU)(nil)

// New builds a cache bounded by cfg.CacheMaxEntries and registers its metrics.
func New(cfg config.Config, reg prometheus.Registerer, opts ...Option) *LRU {
	items, err := lru.New[string, entry](cfg.CacheMaxEntries)
	if err != nil { // only for size <= 0, which config validation rejects
		panic(err)
	}
	f := promauto.With(reg)
	c := &LRU{
		cfg:   cfg,
		now:   time.Now,
		items: items,
		requests: f.NewCounterVec(prometheus.CounterOpts{Namespace: "artstudio", Name: "cache_requests_total",
			Help: "Cache lookups by result."}, []string{"result"}),
		evictions: f.NewCounter(prometheus.CounterOpts{Namespace: "artstudio", Name: "cache_evictions_total",
			Help: "Entries evicted because the cache was full."}),
		dedupes: f.NewCounter(prometheus.CounterOpts{Namespace: "artstudio", Name: "cache_singleflight_dedupes_total",
			Help: "Callers that shared another caller's in-flight fetch."}),
	}
	f.NewGaugeFunc(prometheus.GaugeOpts{Namespace: "artstudio", Name: "cache_entries",
		Help: "Entries currently held."}, func() float64 { return float64(items.Len()) })
	for _, o := range opts {
		o(c)
	}
	return c
}

// Get returns the value and its status: hit (fresh), stale (expired, inside the
// stale window, value returned), negative (nil value) or miss.
func (c *LRU) Get(_ context.Context, key string) ([]byte, ports.CacheStatus) {
	v, st := c.get(key)
	c.requests.WithLabelValues(string(st)).Inc()
	return v, st
}

func (c *LRU) get(key string) ([]byte, ports.CacheStatus) {
	e, ok := c.items.Get(key)
	if !ok {
		return nil, ports.CacheMiss
	}
	now := c.now()
	switch {
	case e.negative && now.Before(e.expires):
		return nil, ports.CacheNegative
	case e.negative:
		c.items.Remove(key)
		return nil, ports.CacheMiss
	case now.Before(e.expires):
		return e.val, ports.CacheHit
	case now.Before(e.staleUntil):
		return e.val, ports.CacheStale
	default:
		c.items.Remove(key)
		return nil, ports.CacheMiss
	}
}

// Set stores v, fresh for ttl and servable as stale for CacheTTLStale after that.
func (c *LRU) Set(_ context.Context, key string, v []byte, ttl time.Duration) {
	exp := c.now().Add(ttl)
	c.add(key, entry{val: v, expires: exp, staleUntil: exp.Add(c.cfg.CacheTTLStale)})
}

// SetNegative records a not-found for CacheTTLNegative; it is never served stale.
func (c *LRU) SetNegative(_ context.Context, key string) {
	exp := c.now().Add(c.cfg.CacheTTLNegative)
	c.add(key, entry{negative: true, expires: exp, staleUntil: exp})
}

func (c *LRU) add(key string, e entry) {
	if c.items.Add(key, e) {
		c.evictions.Inc()
	}
}

// Do runs fn once per key among concurrent callers. shared is true for every
// caller that waited on another caller's fn.
func (c *LRU) Do(_ context.Context, key string, fn func() ([]byte, error)) ([]byte, bool, error) {
	leader := false
	v, err, _ := c.group.Do(key, func() (any, error) {
		leader = true
		return fn()
	})
	if !leader {
		c.dedupes.Inc()
	}
	b, _ := v.([]byte)
	return b, !leader, err
}

// Len is the number of entries held.
func (c *LRU) Len() int { return c.items.Len() }
