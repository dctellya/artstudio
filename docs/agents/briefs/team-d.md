# SPEC.md excerpt for Team D (lead-curated; line numbers refer to SPEC.md v1.0)

# artstudio — Spec for the implementing agent team

## 2. Environment and commands
- Machine: macOS arm64, Go 1.27.x, Docker 28 + Compose v2. Shell sets `DOCKER_DEFAULT_PLATFORM=linux/amd64`: every Makefile docker call is prefixed `DOCKER_DEFAULT_PLATFORM=` (native build; Go TLS under emulation breaks the handshake).
- Gates use `/usr/bin/curl` (LibreSSL), never Homebrew curl.
- Tools on PATH: golangci-lint, govulncheck, vacuum, oapi-codegen (GOPATH/bin); gitleaks, hadolint via docker images.
- Allowed: git (branches, worktrees, PRs; never push to main), go, make, docker, gh. Never amend, reset, rebase or stash.
- Merge: one PR per team per phase against `main`; the lead merges once gate + review pass and tells the human (F-006); C's wiring last. The skeleton also lands through a PR. Worktrees `../artstudio-wt/<team>` on branch `team-<x>/phase-1`.
- **File ownership** (two branches never touch the same file): `go.mod`/`go.sum`, `Makefile`, `internal/config`, `internal/ports`, `cmd/artstudio`, `Dockerfile`, `compose.yaml`, `.golangci.yml`, `.github/` = C. All §4 modules are added to go.mod in the skeleton (`internal/deps/deps.go` blank imports, removed at wiring); a team needing a new module stops and asks.

## 3. Phases
Box t0 = 16:36Z (F-002). Clock: +10 16:46, +20 16:56, +55 17:31, +70 17:46, +80 17:56, +85 18:01, +90 18:06.

| Gate | By | What |
|---|---|---|
| G0 skeleton on main | +10 | C coder, alone |
| G1 tests red, all teams | +20 | five testers in parallel |
| G2 A, B, D green, reviewed, PR | +55 | not green → merge what is green, rest to BACKLOG |
| G3 wiring, compose up, acceptance green | +70 | C |
| G4 `make ci` + workflow green on first run | +80 | C; README gate from a clean clone |
| G5 deliverables, tag `v0.1.0` | +90 | lead |

Phase 2 merges only after G4. No pre-cut (F-004): cosign + ghcr and checkov go last, to BACKLOG only if not green by +85. **Cut order** if short: cosign + ghcr push, checkov, Loki, alert rules. Dashboard and traces always stay.

**Phase 1 — MVP (graded).** One loop per team: TESTER (§6 tests, red, TEST BRIEF) → CODER → REVIEWER (opus) → gate → report → PR.
| Team | Owns | Delivers | Gate |
|---|---|---|---|

| D | `internal/audit`, `internal/tlsconf`, `internal/artic/stubserver`, `cmd/stubserver` | audit writer (pgx pool, migration, async batched queue, pool metrics), TLS server/client config, vendor stub with fault control | `go test -race ./internal/audit/... ./internal/tlsconf/... ./internal/artic/stubserver/...` (+ `-tags integration` with postgres) |

## 4. Versions (verified 2026-10-14 against proxy.golang.org, GitHub releases, registries)
| Component | Version | Component | Version |
|---|---|---|---|
| Go (go.mod, golang image) | 1.27.2 / `golang:1.27.2-alpine` | distroless | `gcr.io/distroless/static-debian12:nonroot` |
| oapi-codegen/v2 | v2.8.0 | oapi-codegen/runtime | v1.7.0 |
| oapi-codegen/nethttp-middleware | v1.2.0 * | getkin/kin-openapi | v0.150.0 * |
| prometheus/client_golang | v1.25.0 | otel, otel/sdk, sdk/log, otlptracehttp | v1.47.0 |
| otlploghttp | v0.23.0 * | contrib otelhttp | v0.72.0 |
| contrib bridges/otelslog | v0.21.0 * | sony/gobreaker/v2 | v2.4.0 |
| x/time | v0.16.0 | x/sync | v0.24.0 |
| x/net (indirect, govulncheck) | v0.61.0 | hashicorp/golang-lru/v2 | v2.0.7 |
| jackc/pgx/v5 | v5.11.0 | stretchr/testify | v1.12.1 |
| swagger-ui-dist (embedded files) | v5.33.2 * | postgres | `postgres:18.6-alpine` |
| golangci-lint | v2.14.0 | govulncheck | v1.9.0 |
| vacuum | v0.33.0 | gitleaks / hadolint | `zricethezav/gitleaks:v8.30.1` / `hadolint/hadolint:v2.15.1` |
| grafana / mimir / tempo | 13.2.4 / 3.2.2 / 3.1.0 | loki / alloy | 3.7.8 / v1.20.1 |
| trivy / syft / cosign | v0.76.0 / v1.54.1 / v3.1.3 | checkov | 3.3.29 |
| actions: checkout / setup-go / cache | v7.0.1 / v7.0.0 / v6.1.0 | golangci-lint-action / govulncheck-action | v9.3.0 / v1.1.0 |
| setup-buildx / build-push / login / metadata | v4.4.1 / v7.4.0 / v4.6.0 / v6.2.0 | trivy-action / sbom-action / cosign-installer | v0.36.0 / v0.24.3 / v4.1.2 |
| upload-artifact / hadolint-action / checkov-action | v7.0.2 / v3.5.0 / v12.3134.0 | | |
`*` outside the allowed list: kin-openapi + nethttp-middleware = the request validator oapi-codegen generates for; otelslog + otlploghttp = the slog→OTLP bridge the brief requires; swagger-ui-dist = static files for `/docs` without CDN (go:embed, no Go dep).

## 5. Architecture
```
client ──TLS1.3 :8443──▶ api ─┬─ lookup ─ cache(LRU) ─▶ artic client ─https─▶ api.artic.edu (or stub:8080 in chaos)
reviewer ─TLS :9090 /metrics ─┤        └─ audit queue(1000) ─batch─▶ postgres:5432 (net backend, plain)
                              └─ OTLP https ─▶ alloy:4318 ─▶ mimir / tempo / loki (net obs, plain) ◀─ grafana :3000 TLS
alloy ─scrape https (CA mounted, server_name api)─▶ api:9090
```
**Interfaces** (`internal/ports`, frozen in G0; all methods take `ctx`):
```go
type Artwork struct{ ID int; Title, Artist, Date, ImageID, ImageURL string }
type SearchQuery struct{ Q string; Limit, Page int }      // Q normalised: trim, lower, collapse spaces
type SearchResult struct{ Items []Artwork; Total, Limit, Page int }
type CacheStatus string // "hit" | "miss" | "stale" | "negative" | "bypass"
type Meta struct{ RequestID string; Cache CacheStatus; UpstreamLatency time.Duration; Attempts int }
type Lookup interface { Search(ctx, SearchQuery) (SearchResult, Meta, error); Artwork(ctx, id int) (Artwork, Meta, error) }
type Vendor interface { Search(ctx, SearchQuery) (SearchResult, Call, error); Artwork(ctx, int) (Artwork, Call, error); Probe(ctx) (Call, error); BreakerState() string }
type Call struct{ Operation string; Status, Attempts int; Latency time.Duration; Breaker string; Body []byte; Err error }
type Cache interface { Get(ctx, key string) ([]byte, CacheStatus); Set(ctx, key string, v []byte, ttl time.Duration); SetNegative(ctx, key string); Do(ctx, key string, fn func() ([]byte, error)) ([]byte, bool, error); Len() int }
type Auditor interface { Record(VendorCall) /* never blocks; drops + counts when full */; Close(ctx) error }
type VendorCall struct{ TS time.Time; RequestID, TraceID, Operation, CacheKey string; CacheStatus CacheStatus; UpstreamStatus, Attempts int; LatencyMS int64; Breaker, Error string; Response []byte }
type Checker interface { Name() string; Check(ctx) CheckResult }   // /ready
type CheckResult struct{ OK bool; Detail map[string]any }       // vendor: state, latency_ms, last_check, last_error, breaker; never gates
var ErrNotFound, ErrUpstreamRateLimited, ErrBreakerOpen, ErrBulkheadFull, ErrLimiterRejected, ErrBadUpstream, ErrBudgetExceeded error
type RetryAfterError interface{ error; RetryAfter() time.Duration }
```

**Config** (env, validated at start, defaults = brief; secrets `DATABASE_URL`, `GRAFANA_ADMIN_PASSWORD` never logged):
| Key | Default | Key | Default |
|---|---|---|---|
| `LISTEN_ADDR` / `METRICS_ADDR` | `:8443` / `:9090` | `TLS_CERT_FILE` / `TLS_KEY_FILE` / `TLS_CA_FILE` | `/certs/api.pem` / `/certs/api-key.pem` / `/certs/ca.pem` |
| `CONTACT_EMAIL` | required (header `AIC-User-Agent: artstudio (<email>)`) | `UPSTREAM_BASE_URL` | `https://api.artic.edu/api/v1` |
| `UPSTREAM_ATTEMPT_TIMEOUT` / `UPSTREAM_BUDGET` | 2s / 5s | `UPSTREAM_MAX_RETRIES` | 2 |
| `UPSTREAM_BACKOFF_BASE` / `_MAX` | 100ms / 1s (full jitter) | `UPSTREAM_RATE` / `_BURST` | 1 / 5 (per process) |
| `UPSTREAM_LIMITER_MAX_WAIT` | 1s (longer wait → reject) | `UPSTREAM_BULKHEAD` | 10 |
| `BREAKER_CONSECUTIVE` / `_RATIO` / `_MIN_REQUESTS` | 5 / 0.5 / 20 | `BREAKER_INTERVAL` / `_OPEN_TIMEOUT` / `_HALF_OPEN_MAX` | 60s / 30s / 3 |
| `PROBE_INTERVAL` | 30s (`GET /artworks?limit=1&fields=id`) | `CACHE_MAX_ENTRIES` | 10000 |
| `CACHE_TTL_SEARCH` / `_ARTWORK` / `_NEGATIVE` / `_STALE` | 10m / 60m / 60s / 60m | `HTTP_READ_HEADER_TIMEOUT` / `_READ` / `_WRITE` / `_IDLE` | 5s / 10s / 15s / 60s |
| `CLIENT_RATE` / `_BURST` | 10 / 20 per IP (RemoteAddr; no proxy trusted) | `MAX_IN_FLIGHT` | 200 |
| `SHUTDOWN_TIMEOUT` | 10s | `DATABASE_URL` | empty = audit off (noop) |
| `DB_MAX_CONNS` | 10 | `AUDIT_QUEUE_SIZE` / `_BATCH` / `_FLUSH` | 1000 / 100 / 1s |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty = off; compose `https://alloy:4318` | `OTEL_EXPORTER_OTLP_CERTIFICATE` / `OTEL_SERVICE_NAME` / `LOG_LEVEL` | `/certs/ca.pem` / `artstudio` / `info` |

**Metrics** (namespace `artstudio`, frozen for E): `http_requests_total{route,method,code}`, `http_request_duration_seconds{route,cache}` (buckets .005 .01 .02 .05 .1 .2 .4 .8 1.6 3.2 5), `http_in_flight`, `http_shed_total`, `http_rate_limited_total`; `upstream_requests_total{operation,status,attempt}`, `upstream_request_duration_seconds{operation}`, `upstream_retries_total{operation,reason}`, `upstream_breaker_state` (0 closed 1 half-open 2 open), `upstream_breaker_transitions_total{from,to}`, `upstream_limiter_wait_seconds`, `upstream_limiter_rejected_total`, `upstream_bulkhead_in_use`, `upstream_bulkhead_rejected_total`, `upstream_probe_up`, `upstream_probe_latency_seconds`; `cache_requests_total{result}`, `cache_entries`, `cache_evictions_total`, `cache_singleflight_dedupes_total`; `audit_queue_depth`, `audit_writes_total{result}`, `audit_dropped_total`, `audit_write_duration_seconds`; `db_pool_{total,idle,acquired,max}_conns`, `db_pool_acquire_total`, `db_pool_acquire_duration_seconds_total`; `build_info{version,commit,go_version}`. Each package registers on a `prometheus.Registerer` passed to its constructor.

**Data:** `vendor_calls(id bigserial pk, ts timestamptz not null, request_id text, trace_id text, operation text, cache_key text, cache_status text, upstream_status int, attempts int, latency_ms int, breaker_state text, error text, response jsonb, response_bytes int)`, index on `ts`, `request_id`. Migration embedded, `create … if not exists`, run at start. Append-only (no update/delete in code). Insert via `CopyFrom` in batches.
**Observability:** otelhttp server span per request; child spans `cache.get`, `vendor.attempt`, `audit.write`; W3C tracecontext. slog JSON to stdout + otelslog bridge → OTLP logs; every line has `request_id`, `trace_id`, `span_id`; one access-log line per request (`msg="request"`, route, status, duration_ms, cache). SLOs: 99.9% non-5xx over 30d (stale = success), p95 < 20ms hits, < 400ms misses.

**Runtime limits:** image distroless static nonroot (uid 65532), `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`; healthcheck `["/artstudio","healthcheck"]`; stub plain http on `backend` only (chaos profile; decision file). Chaos: `make chaos-up` (sets `UPSTREAM_BASE_URL=http://stub:8080/api/v1`, `--profile chaos`), `make chaos FAULT=429|503|slow|timeout`, `make heal` → `docker compose exec stub /stubserver ctl <fault|clear>`.

## 6. Tests
| Suite | Team | Covers (each bullet ≥1 test) | Where | Command |
|---|---|---|---|---|

| audit | D | Record never blocks; queue full → dropped++; batch flush by size and interval; Close flushes; migration idempotent (twice); pool metrics registered | unit local+CI; pg tests `-tags integration` | `go test -race ./internal/audit/...` |
| tlsconf | D | min TLS 1.3; client trusts CA only; HSTS middleware header | local, CI | `go test ./internal/tlsconf/...` |
| stub | D | serves fixtures for both endpoints; ctl 429 (Retry-After), 503, slow, timeout, clear | local, CI | `go test ./internal/artic/stubserver/...` |
