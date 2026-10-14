# D-003 Audit rows are sanitised before COPY

- **What:** text columns are made valid UTF-8 with NULs removed; `response` is stored as jsonb only when valid JSON, valid UTF-8 and free of `\u0000`, else null with `response_bytes` kept.
- **Why:** one rejected row fails the whole `CopyFrom` batch; client-controlled `q` reaches `cache_key`, so any client could erase other rows (review finding, security).
- **Alternatives:** per-row inserts (slower, more round trips); retry the batch row by row on error.
- **Consequences:** the audited text can differ from the raw input by replacement characters; the raw body is not kept when it is not storable JSON.
