//go:build integration

package audit_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/audit"
	"github.com/dctellya/artstudio/internal/ports"
)

func dbURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return u
}

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, audit.Migrate(ctx, pool))
	require.NoError(t, audit.Migrate(ctx, pool))
}

func TestBatchInsertLandsRows(t *testing.T) {
	ctx := context.Background()
	url := dbURL(t)
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url, "AUDIT_BATCH": "10", "AUDIT_FLUSH": "100ms"})
	reg := prometheus.NewRegistry()
	w, err := audit.New(ctx, cfg, reg)
	require.NoError(t, err)

	reqID := "it-" + time.Now().UTC().Format("20060102T150405.000000000")
	for i := 0; i < 25; i++ {
		w.Record(ports.VendorCall{TS: time.Now(), RequestID: reqID, Operation: "search", CacheStatus: ports.CacheMiss,
			UpstreamStatus: 200, Attempts: 1, LatencyMS: int64(i), Breaker: "closed", Response: []byte(`{"ok":true}`)})
	}
	require.NoError(t, w.Close(ctx))

	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	var n int
	require.NoError(t, pool.QueryRow(ctx, `select count(*) from vendor_calls where request_id = $1`, reqID).Scan(&n))
	assert.Equal(t, 25, n)

	assert.True(t, w.Check(ctx).OK)
}

func TestPoolMetricsRegistered(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t, map[string]string{"DATABASE_URL": dbURL(t)})
	reg := prometheus.NewRegistry()
	w, err := audit.New(ctx, cfg, reg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close(ctx) })

	for _, name := range []string{
		"artstudio_db_pool_total_conns", "artstudio_db_pool_idle_conns", "artstudio_db_pool_acquired_conns",
		"artstudio_db_pool_max_conns", "artstudio_db_pool_acquire_total", "artstudio_db_pool_acquire_duration_seconds_total",
	} {
		_, ok := metricSum(t, reg, name)
		assert.True(t, ok, "%s not registered", name)
	}
}

func TestBadRowDoesNotLoseBatch(t *testing.T) {
	ctx := context.Background()
	url := dbURL(t)
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url, "AUDIT_BATCH": "10", "AUDIT_FLUSH": "1h"})
	w, err := audit.New(ctx, cfg, prometheus.NewRegistry())
	require.NoError(t, err)

	reqID := "bad-" + time.Now().UTC().Format("20060102T150405.000000000")
	row := func(op string) ports.VendorCall {
		return ports.VendorCall{TS: time.Now(), RequestID: reqID, Operation: op, CacheStatus: ports.CacheMiss,
			UpstreamStatus: 200, Attempts: 1, Breaker: "closed", Response: []byte(`{"ok":true}`)}
	}
	badUTF8, badNUL, badJSON := row("bad-utf8"), row("bad-nul"), row("bad-json")
	badUTF8.CacheKey = "k\xff"
	badNUL.Error = "e\x00x"
	badJSON.Response = []byte(`{"a":"\u0000"}`)

	for _, c := range []ports.VendorCall{row("good"), badUTF8, badNUL, badJSON} {
		w.Record(c)
	}
	require.NoError(t, w.Close(ctx))

	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	var n int
	require.NoError(t, pool.QueryRow(ctx, `select count(*) from vendor_calls where request_id = $1`, reqID).Scan(&n))
	require.Equal(t, 4, n, "good row and all bad rows must land")

	var respNull bool
	var respBytes int
	require.NoError(t, pool.QueryRow(ctx,
		`select response is null, response_bytes from vendor_calls where request_id = $1 and operation = 'bad-json'`, reqID).Scan(&respNull, &respBytes))
	assert.True(t, respNull, "response with a \\u0000 escape is stored as null")
	assert.Equal(t, len(`{"a":"\u0000"}`), respBytes, "response_bytes still set")

	var key, errText string
	require.NoError(t, pool.QueryRow(ctx, `select cache_key from vendor_calls where request_id = $1 and operation = 'bad-utf8'`, reqID).Scan(&key))
	require.NoError(t, pool.QueryRow(ctx, `select error from vendor_calls where request_id = $1 and operation = 'bad-nul'`, reqID).Scan(&errText))
	assert.NotContains(t, key, "\xff")
	assert.NotContains(t, errText, "\x00")
}
