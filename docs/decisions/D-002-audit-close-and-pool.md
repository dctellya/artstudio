# D-002 Audit writer: Close flushes, ClosePool releases

- **What:** `audit.Writer.Close(ctx)` drains and flushes the queue; `ClosePool()` releases the pgx pool afterwards. `audit.New` requires DATABASE_URL; with it empty the wiring uses `audit.Noop()`.
- **Why:** the shutdown sequence needs the pool for the final flush and the last readiness check; the spec says "empty = audit off".
- **Alternatives:** Close also closes the pool (breaks the post-flush check).
- **Consequences:** cmd/ must call Close then ClosePool during graceful shutdown.
