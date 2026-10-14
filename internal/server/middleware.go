package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/dctellya/artstudio/internal/ports"
)

const (
	headerRequestID = "X-Request-ID"
	// maxRequestIDLen is the longest inbound request id that is accepted.
	maxRequestIDLen = 128
	// requestIDBytes is the entropy of a generated request id.
	requestIDBytes = 16

	routeUnrouted = "unrouted"
	cacheNone     = "none"
)

// reqInfo carries facts learned by inner layers back to the observing layer.
type reqInfo struct {
	traceID, spanID string
	route           string
	cache           string
}

type reqInfoKey struct{}

func infoFrom(ctx context.Context) *reqInfo {
	if i, ok := ctx.Value(reqInfoKey{}).(*reqInfo); ok {
		return i
	}
	return &reqInfo{}
}

// statusWriter records the status code written.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range id {
		if c <= ' ' || c >= 0x7f {
			return false
		}
	}
	return true
}

func newRequestID() string {
	b := make([]byte, requestIDBytes)
	_, _ = rand.Read(b) // crypto/rand.Read does not fail on supported platforms
	return hex.EncodeToString(b)
}

// requestID accepts a sane inbound X-Request-ID or generates one, echoes it and
// stores it in the context for ports.RequestIDFrom.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(headerRequestID, id)
		next.ServeHTTP(w, r.WithContext(ports.WithRequestID(r.Context(), id)))
	})
}

// observe records metrics and writes the one access-log line per request.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		info := &reqInfo{route: routeUnrouted, cache: cacheNone}
		sw := &statusWriter{ResponseWriter: w}
		ctx := context.WithValue(r.Context(), reqInfoKey{}, info)
		next.ServeHTTP(sw, r.WithContext(ctx))
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		elapsed := time.Since(start)
		s.metrics.requests.WithLabelValues(info.route, r.Method, strconv.Itoa(sw.status)).Inc()
		s.metrics.duration.WithLabelValues(info.route, info.cache).Observe(elapsed.Seconds())
		level := slog.LevelInfo
		if sw.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		s.log.LogAttrs(ctx, level, "request",
			slog.String("request_id", ports.RequestIDFrom(ctx)),
			slog.String("trace_id", info.traceID),
			slog.String("span_id", info.spanID),
			slog.String("method", r.Method),
			slog.String("route", info.route),
			slog.Int("status", sw.status),
			slog.Float64("duration_ms", float64(elapsed)/float64(time.Millisecond)),
			slog.String("cache", info.cache),
		)
	})
}

// recoverPanic turns a panic into a 500 problem; the panic value is logged, never returned.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity
				panic(rec)
			}
			info := infoFrom(r.Context())
			s.log.ErrorContext(r.Context(), "panic recovered",
				slog.String("request_id", ports.RequestIDFrom(r.Context())),
				slog.String("trace_id", info.traceID),
				slog.String("span_id", info.spanID),
				slog.Any("panic", rec),
				slog.String("stack", string(debug.Stack())),
			)
			writeProblem(w, r, http.StatusInternalServerError, "internal error")
		}()
		next.ServeHTTP(w, r)
	})
}

// shed rejects requests beyond MaxInFlight with 503 + Retry-After.
func (s *Server) shed(next http.Handler) http.Handler {
	var inFlight atomic.Int64
	limit := int64(s.cfg.MaxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inFlight.Add(1) > limit {
			inFlight.Add(-1)
			s.metrics.shed.Inc()
			setRetryAfter(w, 0)
			writeProblem(w, r, http.StatusServiceUnavailable, "the server is at capacity, retry shortly")
			return
		}
		s.metrics.inFlight.Inc()
		defer func() {
			inFlight.Add(-1)
			s.metrics.inFlight.Dec()
		}()
		next.ServeHTTP(w, r)
	})
}

// limitClients applies the per-IP token bucket.
func (s *Server) limitClients(next http.Handler) http.Handler {
	lim := newClientLimiter(s.cfg.ClientRate, s.cfg.ClientBurst)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := lim.allow(clientKey(r)); !ok {
			s.metrics.rateLimited.Inc()
			setRetryAfter(w, wait)
			writeProblem(w, r, http.StatusTooManyRequests, "client rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// traced runs otelhttp and records the trace ids for the layers above.
func traced(next http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sc := trace.SpanFromContext(r.Context()).SpanContext(); sc.IsValid() {
			info := infoFrom(r.Context())
			info.traceID, info.spanID = sc.TraceID().String(), sc.SpanID().String()
		}
		next.ServeHTTP(w, r)
	})
	return otelhttp.NewHandler(inner, "http.server")
}

// matchedRoute runs inside the generated router, where r.Pattern is known: it
// records the route for metrics and logs and names the span after it.
func matchedRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := routeOf(r.Pattern)
		infoFrom(r.Context()).route = route
		span := trace.SpanFromContext(r.Context())
		span.SetName(r.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route))
		next.ServeHTTP(w, r)
	})
}

// routeOf strips the method from a ServeMux pattern: "GET /art/{query}" -> "/art/{query}".
func routeOf(pattern string) string {
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}
