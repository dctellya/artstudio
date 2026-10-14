# Backlog

| Id | Item | Reason | From |
|---|---|---|---|
| B-001 | Pin GitHub Actions by commit SHA (now by tag) | time box; tags verified 2026-10-14 | C coder |
| B-002 | audit: log failed batch inserts (slog.Warn with SQLSTATE, never DSN) | non-blocking review D#2 | D review |
| B-003 | audit: release writer ctx after clean Close (defer cancel) | review D#3 | D review |
| B-004 | audit: migration under pg_advisory_xact_lock for N replicas | review D#6, harmless at 1 replica | D review |
| B-005 | audit_write_duration_seconds buckets: state in spec or reuse HTTP buckets | review D#7 | D review |
| B-006 | Document audit queue memory bound (1000 × max vendor body) | review D#8 | D review |
| B-007 | HSTS max-age and stub constants (slow 2500ms, ports) to config/docs; ctl port from STUB_ADDR | review D#9-10 | D review |
| B-008 | stub /__ctl unauthenticated: keep on backend network only | review D#12 | D review |
| B-009 | Access-log lines of 429/shed 503 lack trace_id (limiter/shed outside otelhttp) | review B#2; reject-early order kept | B review |
| B-010 | Validator 400/404 counted as route="unrouted" | review B#3 | B review |
| B-011 | Per-IP limiter: IPv6 /64 keying and max entries from config | review B#4 | B review |
| B-012 | Whitespace-only query → 400 instead of empty vendor search | review B#5 | B review |
| B-013 | Bare context.DeadlineExceeded from Lookup → 504 not 500 | review B#6 | B review |
| B-014 | /ready: per-check timeout and concurrent checks | review B#7 | B review |
| B-015 | OpenAPI: X-Request-ID maxLength 128 + pattern; examples (vacuum warnings) | review B#8, #11 | B review |
| B-016 | MaxHeaderBytes from config | review B#9 | B review |
| B-017 | Vendor call can last limiter max wait + budget (6s); start budget before limiter wait | review A#2 | A review |
| B-018 | Upstream max body (8 MiB) constant → UPSTREAM_MAX_BODY_BYTES | review A#3, RULES 4 | A review |
| B-019 | Local rejections carry RetryAfter 0 (server defaults from config cover the header) | review A#4 | A review |
| B-020 | Upstream URL in error text reaches audit rows; reduce to error class | review A#5 | A review |
| B-021 | iiif_url from vendor unchecked; fallback to configured https base | review A#6 | A review |
| B-022 | Local rejections audited as vendor calls: document or skip | review A#7 | A review |
| B-023 | vendor.attempt span not marked Error for non-2xx | review A#8 | A review |
| B-024 | WithHTTPClient mutates caller's client | review A#9 | A review |
| B-025 | Latency SLO alerts (hits < 20ms, misses < 400ms) + panel thresholds | review E#4 | E review |
| B-026 | Confirm probe latency metric type matches dashboard (histogram) | review E#5 | E review |
| B-027 | Justify/comment Mimir limits, Loki retention (enable compactor retention), rule/scrape intervals | review E#6 | E review |
| B-028 | 30d SLO ratio from 5m recording via avg_over_time | review E#7 | E review |
| B-029 | Tempo metrics_generator (tracesToMetrics/serviceMap have no data yet) | review E#9 | E review |
| B-030 | Obs services: healthchecks, read_only, Alertmanager | review E#10 / coder deviations | E review |
| B-031 | TLS on internal links (postgres, obs backends) | D-001 | lead |
| B-032 | api container receives POSTGRES_PASSWORD/GRAFANA_ADMIN_PASSWORD via env_file; pass only what it needs | review C#6 | C review |
| B-033 | `${POSTGRES_PASSWORD:?…}` required form in compose | review C#7 | C review |
| B-034 | make ci vs workflow: -cover, integration step, hadolint version parity | review C#8 | C review |
| B-035 | vacuum image uncached in CI | review C#10 | C review |
| B-036 | CI build job passes no VERSION/COMMIT/BUILD_DATE build args | review C#11 | C review |
| B-037 | stubserver ships in the production image; separate target | review C#12 | C review |
| B-038 | Dockerfile `--platform=$BUILDPLATFORM` + TARGETOS/TARGETARCH for fast multi-arch | review C#13 | C review |
| B-039 | healthcheck defaults duplicate config; 3s timeout unconfigured | review C#15 | C review |
| B-040 | acceptance docker calls without DOCKER_DEFAULT_PLATFORM= prefix | review C#16 | C review |
| B-041 | p95 for cache hits measured ~72 ms in the live run vs the 20 ms SLO: investigate (bucket interpolation on few samples vs real latency) | phase 2 live check | E coder |
| B-042 | Alloy runs as root with cap_drop ALL and a tmpfs WAL: run as the alloy user with a writable volume | phase 2 live check | E coder |
