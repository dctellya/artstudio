# D-008 Middleware order and Retry-After defaults

- **What:** request id → observe (metrics, access log) → panic recovery → shed (MAX_IN_FLIGHT) → per-IP limiter → otelhttp → validator → router. When an error carries no Retry-After, 503s use `BREAKER_OPEN_TIMEOUT` (breaker open) or `UPSTREAM_LIMITER_MAX_WAIT` (other local refusals).
- **Why:** panics still produce a logged, counted 500 with a request id; shedding and limiting reject before any expensive work; Retry-After is always present on 503/429.
- **Alternatives:** recovery outermost (loses request id on panics); tracing before limiting (traces for rejected requests).
- **Consequences:** 429 and shed 503 log lines have no trace_id (B-009).
