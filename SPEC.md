# artstudio — Spec for the implementing agent team
Owner: David Carbajosa (architect, reviewer). Implementers: agent team per docs/TEAM-PROTOCOL.md. Status: v1.0 approved (2026-10-14). Living document: §9 is the human feedback channel, §10 the as-built history.

## 1. Purpose
- **What:** a Go gateway in front of the Art Institute of Chicago API (`https://api.artic.edu/api/v1`, anonymous, 60 req/min). Search artworks, fetch one artwork, return the IIIF image URL `{iiif_url}/{image_id}/full/843,/0/default.jpg`.
- **For whom:** a reviewer for a Principal Infrastructure & SRE take-home who runs it locally from a clean clone.
- **Must prove:** the production patterns around a 3rd-party call: timeouts, retries, breaker, client rate limit, bulkhead, cache with stale, audit log, observability (metrics, traces, logs linked), hardening, TLS on every edge, CI as a gate.
- **Out of scope:** fetching images (Cloudflare challenge; hotlink only), auth/users, CD, TLS on internal links (api→postgres, alloy/grafana→backends; backlog), shared cache (Redis) and shared limiter (backlog, behind interfaces), HA/replicas.

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

**Walking skeleton (C, one coder, on main):** module `github.com/dctellya/artstudio`; `internal/ports` (§5 interfaces, verbatim); `internal/config` (all §5 keys, defaults, validation); `tools/gencert` (full: CA + api/grafana/collector certs, §5 SANs) and `make certs`; `.golangci.yml`; Makefile with every §6/§8 target (unimplemented ones exit 1 with "not yet"); `internal/skeleton` naive impls (plain http.Client, no cache, noop audit); `cmd/artstudio` serving `/health` and `/art/{query}` over TLS 1.3 on 8443, `healthcheck` subcommand; Dockerfile; `compose.yaml` (api + postgres, chaos profile with `stub`); `test/acceptance` (tag `acceptance`, all §6 acceptance checks, red except health + one search); govulncheck clean (bump x/net etc.); README, time log, transcript started.
- Acceptance: `make certs && make up && make test-acceptance` → health + search pass, rest red with readable reasons.

**Phase 1 — MVP (graded).** One loop per team: TESTER (§6 tests, red, TEST BRIEF) → CODER → REVIEWER (opus) → gate → report → PR.
| Team | Owns | Delivers | Gate |
|---|---|---|---|
| A | `internal/artic`, `internal/cache`, `internal/lookup` | `ports.Vendor` (timeouts, retries+jitter+Retry-After, breaker, limiter, bulkhead, no redirects, typed errors, prober), `ports.Cache` (LRU+TTL, stale, negative, singleflight), `ports.Lookup` (cache→vendor→stale, audit record per vendor call) | `go test -race ./internal/artic/... ./internal/cache/... ./internal/lookup/...` |
| B | `api/`, `internal/server`, `internal/httpapi` (generated) | contract, generated handlers, request validation, problem+json, middleware chain, `/health /ready /version /openapi.yaml /docs`, metrics listener handler | `go test -race ./internal/server/... ./api/...` + `vacuum lint -d api/openapi.yaml` |
| C | skeleton, `cmd/`, Dockerfile, compose, CI | G0; then wiring as A/B/D merge; `make ci`; `.github/workflows/ci.yml`; dependabot | G3, G4 |
| D | `internal/audit`, `internal/tlsconf`, `internal/artic/stubserver`, `cmd/stubserver` | audit writer (pgx pool, migration, async batched queue, pool metrics), TLS server/client config, vendor stub with fault control | `go test -race ./internal/audit/... ./internal/tlsconf/... ./internal/artic/stubserver/...` (+ `-tags integration` with postgres) |
| E | `compose.obs.yaml`, `deploy/obs/**` | Alloy, Mimir, Tempo, Loki, Grafana provisioning, dashboard, recording + alert rules; builds against §5 frozen names from G0; one live check after G3 | `make obs-validate` (promtool, alloy validate, jq dashboard, compose config) |

**Phase 2 — demo (after G4):** E's stack merged; otel traces + logs over OTLP/https to Alloy (wired by C); live round trip: response `X-Request-ID` → Loki line → trace in Tempo → logs for span. CI `image` (buildx amd64+arm64, trivy, syft, cosign, ghcr) and `policy` (checkov, stack validation) jobs.

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
**Error mapping (B):** NotFound→404; BreakerOpen / BulkheadFull / LimiterRejected / UpstreamRateLimited (after retries) → 503 + Retry-After (only when nothing stale); BadUpstream (5xx/3xx/undecodable/403 after retries) → 502; BudgetExceeded→504; own per-client limit→429 + Retry-After; validation→400. Body `application/problem+json` {type,title,status,detail,instance,request_id}; vendor payload never echoed.

**HTTP contract** (`api/openapi.yaml`, OpenAPI 3.0.3 — kin-openapi validator target):
| Route | Notes |
|---|---|
| `GET /art/{query}` | query 1-100 chars; `limit` 1-50 (12), `page` 1-50 (1), `limit*page ≤ 1000` else 400; 200 `{data:[Artwork], pagination, meta}` |
| `GET /artworks/{id}` | id int ≥1; 200 `{data:Artwork, meta}`; 404 problem |
| `GET /health` | 200 `{status:"ok"}`, checks nothing |
| `GET /ready` | 200/503 `{status, checks:{config,db,cache,vendor}}`; vendor never 503s it |
| `GET /version` | `{version, commit, go_version, build_date}` (ldflags) |
| `GET /openapi.yaml`, `GET /docs` | embedded spec and Swagger UI |
| `GET /metrics` | only on :9090 listener |
`meta` = `{request_id, cache, upstream_latency_ms}`; headers `X-Request-ID`, `X-Cache`, `Server-Timing: upstream;dur=…`. Vendor fields requested: `id,title,artist_display,date_display,image_id`; `image_url` null when no `image_id`.

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
Reasons: timeouts/retries/breaker from the brief (2 retries × 1s cap fits a 5s budget); rate 1/s burst 5 stays under the vendor's 60/min; stale 60m because an old painting record is still correct.

**Cache keys:** `search:v1:<q>:<limit>:<page>`, `artwork:v1:<id>`. Stale = expired but within `CACHE_TTL_STALE`; served only on vendor error/breaker open, with `X-Cache: stale`. 404 cached negative 60s.
**Retries:** network errors, 429, 502, 503, 504 only; `Retry-After` honoured if it fits the budget, else stop. Each attempt a span `vendor.attempt` (attr `attempt`, `http.status_code`). 3xx never followed (`CheckRedirect` → `http.ErrUseLastResponse`), counted as upstream error.
**Vendor window:** the vendor rejects `limit*page > 1000` with 403 (verified live 2026-10-14). Ranges stay; handler returns 400 problem+json when `limit*page > 1000`, documented in the OpenAPI description; a vendor 403 that gets through → 502 (F-003).

**Metrics** (namespace `artstudio`, frozen for E): `http_requests_total{route,method,code}`, `http_request_duration_seconds{route,cache}` (buckets .005 .01 .02 .05 .1 .2 .4 .8 1.6 3.2 5), `http_in_flight`, `http_shed_total`, `http_rate_limited_total`; `upstream_requests_total{operation,status,attempt}`, `upstream_request_duration_seconds{operation}`, `upstream_retries_total{operation,reason}`, `upstream_breaker_state` (0 closed 1 half-open 2 open), `upstream_breaker_transitions_total{from,to}`, `upstream_limiter_wait_seconds`, `upstream_limiter_rejected_total`, `upstream_bulkhead_in_use`, `upstream_bulkhead_rejected_total`, `upstream_probe_up`, `upstream_probe_latency_seconds`; `cache_requests_total{result}`, `cache_entries`, `cache_evictions_total`, `cache_singleflight_dedupes_total`; `audit_queue_depth`, `audit_writes_total{result}`, `audit_dropped_total`, `audit_write_duration_seconds`; `db_pool_{total,idle,acquired,max}_conns`, `db_pool_acquire_total`, `db_pool_acquire_duration_seconds_total`; `build_info{version,commit,go_version}`. Each package registers on a `prometheus.Registerer` passed to its constructor.

**Data:** `vendor_calls(id bigserial pk, ts timestamptz not null, request_id text, trace_id text, operation text, cache_key text, cache_status text, upstream_status int, attempts int, latency_ms int, breaker_state text, error text, response jsonb, response_bytes int)`, index on `ts`, `request_id`. Migration embedded, `create … if not exists`, run at start. Append-only (no update/delete in code). Insert via `CopyFrom` in batches.
**Observability:** otelhttp server span per request; child spans `cache.get`, `vendor.attempt`, `audit.write`; W3C tracecontext. slog JSON to stdout + otelslog bridge → OTLP logs; every line has `request_id`, `trace_id`, `span_id`; one access-log line per request (`msg="request"`, route, status, duration_ms, cache). SLOs: 99.9% non-5xx over 30d (stale = success), p95 < 20ms hits, < 400ms misses.
**Frozen names for E:** compose services `api`, `postgres`, `stub`, `alloy`, `mimir`, `tempo`, `loki`, `grafana`; networks `backend` (api, postgres, stub), `obs` (api, alloy, mimir, tempo, loki, grafana); certs `deploy/certs/{ca,api,api-key,grafana,grafana-key,alloy,alloy-key}.pem`; SANs api: `api.127.0.0.1.nip.io, api, localhost`; grafana: `grafana.127.0.0.1.nip.io, grafana, localhost`; alloy: `alloy`; published only `127.0.0.1:8443`, `127.0.0.1:9090`, `127.0.0.1:3000`; Loki label `service_name="artstudio"`; Tempo service name `artstudio`.
**Runtime limits:** image distroless static nonroot (uid 65532), `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`; healthcheck `["/artstudio","healthcheck"]`; stub plain http on `backend` only (chaos profile; decision file). Chaos: `make chaos-up` (sets `UPSTREAM_BASE_URL=http://stub:8080/api/v1`, `--profile chaos`), `make chaos FAULT=429|503|slow|timeout`, `make heal` → `docker compose exec stub /stubserver ctl <fault|clear>`.

## 6. Tests
| Suite | Team | Covers (each bullet ≥1 test) | Where | Command |
|---|---|---|---|---|
| artic unit | A | attempt timeout 2s; budget 5s → ErrBudgetExceeded; retries only on net/429/502/503/504, max 2; backoff full jitter ≤1s; Retry-After honoured/exceeds budget; 3xx not followed → ErrBadUpstream; breaker opens after 5 consecutive and at 50%/20; half-open 3 trials; limiter waits ≤1s then ErrLimiterRejected; bulkhead 11th → ErrBulkheadFull; 404→ErrNotFound; vendor 429 surviving → ErrUpstreamRateLimited with RetryAfter; AIC-User-Agent header sent; image URL built; prober updates state | local, CI | `go test -race ./internal/artic/...` |
| cache unit | A | LRU bound+eviction; TTL; stale within window, gone after; negative 60s; singleflight N callers → 1 fn call; key normalisation | local, CI | `go test -race ./internal/cache/...` |
| lookup unit | A | hit/miss/stale/negative statuses; stale on vendor error and on breaker open; error when nothing stale; one Auditor.Record per vendor call, none on hit | local, CI | `go test -race ./internal/lookup/...` |
| artic integration | A | client vs stubserver: 503 → retries → breaker open → stale | local, CI | `go test -race -tags integration ./internal/artic/...` |
| contract | B | vacuum lint; generated router rejects limit 0/51, page 51, id 0, limit=50&page=21 with 400 problem+json; responses validate against schema | local, CI | `go test ./api/... ./internal/server/...` |
| server unit | B | error→status map incl. Retry-After; problem+json never contains vendor body; X-Request-ID in/out + generated; per-IP 429 + Retry-After; shed at 200 → 503; panic → 500 problem; timeouts set from config; /health, /ready (vendor down ≠ 503), /version, /docs, /openapi.yaml; meta + X-Cache headers; access log line has request_id + trace_id | local, CI | `go test -race ./internal/server/...` |
| audit | D | Record never blocks; queue full → dropped++; batch flush by size and interval; Close flushes; migration idempotent (twice); pool metrics registered | unit local+CI; pg tests `-tags integration` | `go test -race ./internal/audit/...` |
| tlsconf | D | min TLS 1.3; client trusts CA only; HSTS middleware header | local, CI | `go test ./internal/tlsconf/...` |
| stub | D | serves fixtures for both endpoints; ctl 429 (Retry-After), 503, slow, timeout, clear | local, CI | `go test ./internal/artic/stubserver/...` |
| obs static | E | promtool rules, alloy validate, dashboard JSON parses, compose config | local, CI (phase 2) | `make obs-validate` |
| acceptance | C | health; TLS only (http fails, tls1.2 refused); search miss→hit; artwork + image_url; 404 problem + negative; 400 limit=51; X-Request-ID echo; per-IP 429; /ready vendor block; /metrics https on 9090 has every §5 family; audit rows in pg; healthcheck subcommand; chaos: breaker open + stale; graceful stop exit 0 | local only (needs stack) | `make test-acceptance` |
| obs live | E | log line request_id+trace_id → trace in Tempo → Loki derived field | local, after G3 | `make test-obs` |

## 7. CI (`.github/workflows/ci.yml`, ubuntu-latest, on push + PR)
| Job | Gates | Target |
|---|---|---|
| test | vet, golangci-lint (gosec, errcheck, bodyclose), `go test -race -cover`, vacuum lint, contract tests, integration (pg service container) | < 2 min warm |
| scan | gitleaks (docker image), govulncheck-action, hadolint-action | < 1 min |
| build | buildx build, no push, gha cache, cache mounts | < 2 min warm |
| image (P2, after light jobs on main/tags) | buildx amd64+arm64, trivy fail on CRITICAL (cached db), syft SBOM artifact, push ghcr (GITHUB_TOKEN), cosign keyless; anonymous `docker pull` check | < 5 min |
| policy (P2) | checkov (compose, workflows), `make obs-validate` | < 2 min |
No `needs` among light jobs; prebuilt actions only; CI never starts the obs stack. Required checks: test, scan, build. Dependabot: gomod, github-actions, docker weekly. `make ci` runs test+scan+build locally with the same commands.

## 8. Deliverables
- **README.md** per `docs/README-SKELETON.md`, every placeholder filled, Verify table with a command per claim; started at G0, extended at every merge; README gate at +80 from a clean clone into a temp dir. Says only: take-home for a Principal Infrastructure & SRE role. Warns `DOCKER_DEFAULT_PLATFORM` must be unset; states limiter is per process (N replicas → shared limiter or 1/N each).
- **docs/decisions/D-NNN-slug.md** one per decision (ids from the lead); `DECISIONS.md` via `make decisions`.
- **BACKLOG.md**, **reports/<team>-phase-N.md** and `<team>-final.md`, **docs/agents/logs/** (`lead.md`, `<team>/<role>-phase-N.md`, timestamps from `date -u`), **docs/transcript.md**, time log in README.
- `docs/assignment.md` stays git-ignored, never quoted.

## 9. Changelog and human feedback
| Id | Date | Feedback | Applies | Status |
|---|---|---|---|---|
| F-001 | 2026-10-14 | Initial brief | all | done (v0.1) |
| F-002 | 2026-10-14 | Box t0 stays 16:36Z | §3 | applied |
| F-003 | 2026-10-14 | Keep ranges; 400 when limit×page>1000, in OpenAPI description; vendor 403 → 502 | §5, §6 B | applied |
| F-004 | 2026-10-14 | gencert in skeleton; `make chaos-up`; no phase-2 pre-cut, cosign+ghcr and checkov last, BACKLOG only if not green by +85 | §3, §5 | applied |
| F-005 | 2026-10-14 | Approved libs: kin-openapi, nethttp-middleware, otelslog, otlploghttp, swagger-ui-dist embedded | §4 | applied |
| F-006 | 2026-10-14 | Lead merges each PR once gate + review pass, tells the human, does not wait | §2 | applied |

## 10. Evolution log
| Phase | Merged | Shipped | Tests green | Deviations |
|---|---|---|---|---|
| C-0 skeleton | 16:54 PR #1, #2 | module, ports (+ request-id ctx helpers), config, gencert, TLS stub cmd, Dockerfile, compose, Makefile, acceptance suite | config unit; acceptance 5/15 | oapi-codegen as go.mod tool; USER 65532; .gitleaks.toml allowlists git-ignored certs |
| D-1 | 17:06 PR #3 | audit writer + migration + pool metrics, tlsconf, vendor stub + ctl | audit 7+4 integ, tlsconf 5, stub | Close/ClosePool split (D-002); row sanitisation (D-003) |
| A-1 | 17:10 PR #4 | vendor client, prober, cache, lookup | artic, cache, lookup + integration vs stub | breaker per call (D-004); detached singleflight (D-005); stale/audit scope (D-006) |
| B-1 | 17:11 PR #5 | contract, generated router, validation, server chain, problem+json, Swagger UI | server/contract | middleware order (D-008); servers "/", /docs/{file} (D-007) |
