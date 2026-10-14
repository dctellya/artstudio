// Package audit writes every vendor call to an append-only Postgres table
// through a bounded, non-blocking, batched queue.
package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

// Writer is the ports.Auditor and the "db" ports.Checker.
type Writer struct {
	sink    Sink
	pool    *pgxpool.Pool // nil when built with NewWithSink
	batch   int
	flush   time.Duration
	queue   chan ports.VendorCall
	dropped prometheus.Counter
	writes  *prometheus.CounterVec
	latency prometheus.Histogram

	mu     sync.RWMutex // guards closed against Record's send
	closed bool
	cancel context.CancelFunc // aborts an in-flight insert when Close's ctx ends
	ctx    context.Context
	done   chan struct{}
}

var (
	_ ports.Auditor = (*Writer)(nil)
	_ ports.Checker = (*Writer)(nil)
)

// New opens the pool, runs the migration and starts the writer.
func New(ctx context.Context, cfg config.Config, reg prometheus.Registerer) (*Writer, error) {
	pc, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("audit: invalid DATABASE_URL")
	}
	pc.MaxConns = int32(min(max(cfg.DBMaxConns, 1), math.MaxInt32)) //nolint:gosec // clamped to int32 range
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("audit: open pool: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if err := reg.Register(newPoolCollector(pool)); err != nil {
		pool.Close()
		return nil, fmt.Errorf("audit: register pool metrics: %w", err)
	}
	w := NewWithSink(cfg, reg, pgSink{pool: pool})
	w.pool = pool
	return w, nil
}

// NewWithSink starts a writer that persists through sink.
func NewWithSink(cfg config.Config, reg prometheus.Registerer, sink Sink) *Writer {
	w := &Writer{
		sink:  sink,
		batch: max(cfg.AuditBatch, 1),
		flush: cfg.AuditFlush,
		queue: make(chan ports.VendorCall, max(cfg.AuditQueueSize, 1)),
		done:  make(chan struct{}),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "artstudio", Name: "audit_dropped_total", Help: "Audit rows dropped because the queue was full.",
		}),
		writes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "artstudio", Name: "audit_writes_total", Help: "Audit batch writes by result.",
		}, []string{"result"}),
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "artstudio", Name: "audit_write_duration_seconds", Help: "Audit batch write duration.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	depth := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "artstudio", Name: "audit_queue_depth", Help: "Audit rows waiting in the queue.",
	}, func() float64 { return float64(len(w.queue)) })
	for _, r := range []string{"ok", "error"} { // export both children from start
		w.writes.WithLabelValues(r)
	}
	reg.MustRegister(w.dropped, w.writes, w.latency, depth)
	go w.run()
	return w
}

// Record enqueues without ever blocking; a full queue drops and counts.
func (w *Writer) Record(c ports.VendorCall) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		w.dropped.Inc()
		return
	}
	select {
	case w.queue <- c:
	default:
		w.dropped.Inc()
	}
}

// Close stops intake and flushes what is queued, giving up when ctx ends.
func (w *Writer) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()

	var err error
	select {
	case <-w.done:
		w.cancel()
	case <-ctx.Done():
		w.cancel()
		<-w.done
		err = fmt.Errorf("audit close: %w", ctx.Err())
	}
	return err
}

// ClosePool releases the database pool; call it after Close and the last Check.
func (w *Writer) ClosePool() {
	if w.pool != nil {
		w.pool.Close()
	}
}

// Name implements ports.Checker.
func (w *Writer) Name() string { return "db" }

// Check pings the pool; a writer without a pool is always healthy.
func (w *Writer) Check(ctx context.Context) ports.CheckResult {
	if w.pool == nil {
		return ports.CheckResult{OK: true}
	}
	if err := w.pool.Ping(ctx); err != nil {
		return ports.CheckResult{Detail: map[string]any{"error": err.Error()}}
	}
	return ports.CheckResult{OK: true}
}

func (w *Writer) run() {
	defer close(w.done)
	tick := time.NewTicker(w.flush)
	defer tick.Stop()
	buf := make([]ports.VendorCall, 0, w.batch)
	flush := func() {
		if len(buf) > 0 {
			w.write(buf)
			buf = buf[:0]
		}
	}
	for {
		select {
		case c, ok := <-w.queue:
			if !ok {
				flush()
				return
			}
			buf = append(buf, c)
			if len(buf) >= w.batch {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

func (w *Writer) write(batch []ports.VendorCall) {
	ctx, span := otel.Tracer("artstudio/audit").Start(w.ctx, "audit.write")
	defer span.End()
	start := time.Now()
	err := w.sink.Insert(ctx, batch)
	w.latency.Observe(time.Since(start).Seconds())
	if err != nil {
		span.SetStatus(codes.Error, "insert failed")
		state := ""
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			state = pgErr.Code
		}
		slog.WarnContext(ctx, "audit batch insert failed", "sqlstate", state, "batch_size", len(batch))
		w.writes.WithLabelValues("error").Inc()
		return
	}
	w.writes.WithLabelValues("ok").Inc()
}

type noop struct{}

// Noop returns an Auditor that discards everything (audit off).
func Noop() ports.Auditor { return noop{} }

func (noop) Record(ports.VendorCall)     {}
func (noop) Close(context.Context) error { return nil }
