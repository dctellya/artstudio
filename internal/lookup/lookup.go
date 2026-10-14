// Package lookup is the read path: cache, then one singleflight vendor call,
// then stale on vendor failure, with an audit record per vendor call.
package lookup

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/dctellya/artstudio/internal/cache"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

// Service implements ports.Lookup.
type Service struct {
	vendor ports.Vendor
	cache  ports.Cache
	audit  ports.Auditor
	cfg    config.Config
	tracer trace.Tracer
	now    func() time.Time
}

var _ ports.Lookup = (*Service)(nil)

// New wires the lookup. It registers no metrics of its own (cache and vendor do).
func New(v ports.Vendor, c ports.Cache, a ports.Auditor, cfg config.Config, _ prometheus.Registerer) *Service {
	return &Service{vendor: v, cache: c, audit: a, cfg: cfg, tracer: otel.Tracer("artstudio/lookup"), now: time.Now}
}

// Search returns one page of results.
func (s *Service) Search(ctx context.Context, q ports.SearchQuery) (ports.SearchResult, ports.Meta, error) {
	q.Q = cache.NormalizeQuery(q.Q)
	return fetch(ctx, s, cache.SearchKey(q), s.cfg.CacheTTLSearch, func() (ports.SearchResult, ports.Call, error) {
		return s.vendor.Search(ctx, q)
	})
}

// Artwork returns one artwork.
func (s *Service) Artwork(ctx context.Context, id int) (ports.Artwork, ports.Meta, error) {
	return fetch(ctx, s, cache.ArtworkKey(id), s.cfg.CacheTTLArtwork, func() (ports.Artwork, ports.Call, error) {
		return s.vendor.Artwork(ctx, id)
	})
}

func fetch[T any](ctx context.Context, s *Service, key string, ttl time.Duration, call func() (T, ports.Call, error)) (T, ports.Meta, error) {
	var zero T
	meta := ports.Meta{RequestID: ports.RequestIDFrom(ctx)}

	_, span := s.tracer.Start(ctx, "cache.get")
	raw, status := s.cache.Get(ctx, key)
	span.End()

	switch status {
	case ports.CacheHit:
		var v T
		if err := json.Unmarshal(raw, &v); err == nil {
			meta.Cache = ports.CacheHit
			return v, meta, nil
		}
		status = ports.CacheMiss // undecodable entry: refetch
		raw = nil
	case ports.CacheNegative:
		meta.Cache = ports.CacheNegative
		return zero, meta, ports.ErrNotFound
	}

	var vc ports.Call
	b, _, err := s.cache.Do(ctx, key, func() ([]byte, error) {
		v, c, err := call()
		vc = c
		s.record(ctx, key, status, c, err)
		if err != nil {
			if errors.Is(err, ports.ErrNotFound) {
				s.cache.SetNegative(ctx, key)
			}
			return nil, err
		}
		enc, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		s.cache.Set(ctx, key, enc, ttl)
		return enc, nil
	})
	meta.Attempts, meta.UpstreamLatency = vc.Attempts, vc.Latency

	if err == nil {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return zero, meta, err
		}
		meta.Cache = ports.CacheMiss
		return v, meta, nil
	}
	if status == ports.CacheStale && !errors.Is(err, ports.ErrNotFound) {
		var v T
		if json.Unmarshal(raw, &v) == nil {
			meta.Cache = ports.CacheStale
			return v, meta, nil
		}
	}
	meta.Cache = ports.CacheMiss
	return zero, meta, err
}

func (s *Service) record(ctx context.Context, key string, status ports.CacheStatus, c ports.Call, err error) {
	rec := ports.VendorCall{
		TS:             s.now(),
		RequestID:      ports.RequestIDFrom(ctx),
		TraceID:        traceID(ctx),
		Operation:      c.Operation,
		CacheKey:       key,
		CacheStatus:    status,
		UpstreamStatus: c.Status,
		Attempts:       c.Attempts,
		LatencyMS:      c.Latency.Milliseconds(),
		Breaker:        c.Breaker,
		Response:       c.Body,
	}
	if err != nil {
		rec.Error = err.Error()
	}
	s.audit.Record(rec)
}

func traceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}
