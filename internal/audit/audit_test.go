package audit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/audit"
	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

const testWait = 2 * time.Second

// fakeSink records batches; a non-nil gate blocks Insert until released.
type fakeSink struct {
	mu      sync.Mutex
	batches [][]ports.VendorCall
	got     chan []ports.VendorCall
	gate    chan struct{}
	once    sync.Once
}

func newSink(gated bool) *fakeSink {
	s := &fakeSink{got: make(chan []ports.VendorCall, 1024)}
	if gated {
		s.gate = make(chan struct{})
	}
	return s
}

func (s *fakeSink) Insert(ctx context.Context, batch []ports.VendorCall) error {
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	cp := append([]ports.VendorCall(nil), batch...)
	s.mu.Lock()
	s.batches = append(s.batches, cp)
	s.mu.Unlock()
	s.got <- cp
	return nil
}

func (s *fakeSink) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.batches {
		n += len(b)
	}
	return n
}

func (s *fakeSink) release() {
	if s.gate != nil {
		s.once.Do(func() { close(s.gate) })
	}
}

func testConfig(t *testing.T, env map[string]string) config.Config {
	t.Helper()
	base := map[string]string{"CONTACT_EMAIL": "test@example.com"}
	for k, v := range env {
		base[k] = v
	}
	cfg, err := config.LoadFrom(func(k string) (string, bool) { v, ok := base[k]; return v, ok })
	require.NoError(t, err)
	return cfg
}

func call(i int) ports.VendorCall {
	return ports.VendorCall{TS: time.Now(), RequestID: "req", Operation: "search", UpstreamStatus: 200, Attempts: 1, LatencyMS: int64(i)}
}

func metricSum(t *testing.T, reg *prometheus.Registry, name string) (float64, bool) {
	t.Helper()
	fams, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		var sum float64
		for _, m := range f.GetMetric() {
			sum += m.GetCounter().GetValue() + m.GetGauge().GetValue() + float64(m.GetHistogram().GetSampleCount())
		}
		return sum, true
	}
	return 0, false
}

func TestRecordNeverBlocks(t *testing.T) {
	sink := newSink(true)
	t.Cleanup(sink.release)
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_QUEUE_SIZE": "10", "AUDIT_BATCH": "5"}), prometheus.NewRegistry(), sink)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			w.Record(call(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testWait):
		t.Fatal("Record blocked while the sink was stalled")
	}
}

func TestQueueFullIncrementsDropped(t *testing.T) {
	sink := newSink(true)
	t.Cleanup(sink.release)
	reg := prometheus.NewRegistry()
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_QUEUE_SIZE": "5", "AUDIT_BATCH": "1"}), reg, sink)

	for i := 0; i < 50; i++ {
		w.Record(call(i))
	}
	dropped, ok := metricSum(t, reg, "artstudio_audit_dropped_total")
	require.True(t, ok, "artstudio_audit_dropped_total not registered")
	// queue holds 5, the flusher holds at most one batch of 1: at least 44 dropped.
	assert.GreaterOrEqual(t, dropped, 40.0)
}

func TestFlushBySize(t *testing.T) {
	sink := newSink(false)
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_BATCH": "3", "AUDIT_FLUSH": "1h"}), prometheus.NewRegistry(), sink)
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	for i := 0; i < 3; i++ {
		w.Record(call(i))
	}
	select {
	case b := <-sink.got:
		assert.Len(t, b, 3)
	case <-time.After(testWait):
		t.Fatal("batch of 3 was not flushed by size")
	}
}

func TestFlushByInterval(t *testing.T) {
	sink := newSink(false)
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_BATCH": "100", "AUDIT_FLUSH": "50ms"}), prometheus.NewRegistry(), sink)
	t.Cleanup(func() { _ = w.Close(context.Background()) })

	w.Record(call(1))
	w.Record(call(2))
	select {
	case b := <-sink.got:
		assert.Len(t, b, 2)
	case <-time.After(testWait):
		t.Fatal("partial batch was not flushed by interval")
	}
}

func TestCloseFlushes(t *testing.T) {
	sink := newSink(false)
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_BATCH": "100", "AUDIT_FLUSH": "1h"}), prometheus.NewRegistry(), sink)

	for i := 0; i < 5; i++ {
		w.Record(call(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), testWait)
	defer cancel()
	require.NoError(t, w.Close(ctx))
	assert.Equal(t, 5, sink.total())
}

func TestWriterMetricsAndChecker(t *testing.T) {
	sink := newSink(false)
	reg := prometheus.NewRegistry()
	w := audit.NewWithSink(testConfig(t, map[string]string{"AUDIT_BATCH": "1", "AUDIT_FLUSH": "50ms"}), reg, sink)
	var _ ports.Auditor = w
	var _ ports.Checker = w
	assert.Equal(t, "db", w.Name())

	w.Record(call(1))
	select {
	case <-sink.got:
	case <-time.After(testWait):
		t.Fatal("no flush")
	}
	require.NoError(t, w.Close(context.Background()))
	for _, name := range []string{"artstudio_audit_queue_depth", "artstudio_audit_writes_total", "artstudio_audit_write_duration_seconds"} {
		_, ok := metricSum(t, reg, name)
		assert.True(t, ok, "%s not registered", name)
	}
}

func TestNoopAuditor(t *testing.T) {
	a := audit.Noop()
	require.NotNil(t, a)
	a.Record(call(1))
	assert.NoError(t, a.Close(context.Background()))
}

func TestWritesTotalExportedFromStart(t *testing.T) {
	reg := prometheus.NewRegistry()
	w := audit.NewWithSink(testConfig(t, nil), reg, newSink(false))
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	for _, name := range []string{
		"artstudio_audit_writes_total", "artstudio_audit_dropped_total",
		"artstudio_audit_queue_depth", "artstudio_audit_write_duration_seconds",
	} {
		_, ok := metricSum(t, reg, name)
		assert.True(t, ok, "%s not exported before any write", name)
	}
}
