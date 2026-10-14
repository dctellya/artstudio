package audit

import (
	"context"
	_ "embed" // migration SQL
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dctellya/artstudio/internal/ports"
)

//go:embed migrations/001_vendor_calls.sql
var migrationSQL string

// Sink persists one batch of audit rows.
type Sink interface {
	Insert(ctx context.Context, batch []ports.VendorCall) error
}

// Migrate creates the vendor_calls table and indexes; safe to run repeatedly.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, migrationSQL); err != nil {
		return fmt.Errorf("audit migrate: %w", err)
	}
	return nil
}

var columns = []string{
	"ts", "request_id", "trace_id", "operation", "cache_key", "cache_status",
	"upstream_status", "attempts", "latency_ms", "breaker_state", "error",
	"response", "response_bytes",
}

type pgSink struct{ pool *pgxpool.Pool }

// Insert copies the batch into vendor_calls. The response is stored as jsonb
// when it is valid JSON, otherwise null; response_bytes is always set.
func (s pgSink) Insert(ctx context.Context, batch []ports.VendorCall) error {
	rows := make([][]any, len(batch))
	for i, c := range batch {
		var resp any
		if json.Valid(c.Response) {
			resp = c.Response
		}
		rows[i] = []any{
			c.TS, c.RequestID, c.TraceID, c.Operation, c.CacheKey, string(c.CacheStatus),
			c.UpstreamStatus, c.Attempts, c.LatencyMS, c.Breaker, c.Error,
			resp, len(c.Response),
		}
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"vendor_calls"}, columns, pgx.CopyFromRows(rows))
	return err
}
