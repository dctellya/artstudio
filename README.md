# artstudio

artstudio is a gateway in front of a third-party API (the Art Institute of Chicago), built to show what belongs around any vendor call in production. It was built as a take-home for a Principal Infrastructure & SRE role.

- **Resilience**: per-attempt timeouts and a total budget, retries with jitter honouring Retry-After, circuit breaker, client-side rate limit, bulkhead; typed errors mapped to honest status codes.
- **Cache** with stale serving: when the vendor fails or the breaker is open, a stale answer beats a 502.
- **Audit log** in Postgres: one row per vendor call with what the vendor returned, written off the request path through a bounded queue.
- **TLS everywhere on the edges**, a local CA you generate, everything published on loopback only.
- **Operability**: `/health`, `/ready` (reports the vendor and the breaker, never gates on them), `/metrics`, `/version`, and the API described in `/docs` as an OpenAPI contract that also validates requests.
- **Observability** on the Grafana stack: metrics, traces and logs, linked by trace id, one dashboard with the SLOs and the error budget, alert rules with runbooks.
- **Hardening**: per-client rate limit, load shedding, request ids, panic recovery, graceful shutdown, config from the environment, no secrets in logs; a distroless, non-root, multi-arch image.
- **CI** as a gate: lint, unit and contract tests, vulnerability scan, secrets scan, image build; acceptance tests end to end against the running stack.

Images are never fetched: the service builds the IIIF URL and the client hotlinks it.

## Requirements

Docker with Compose v2, Go 1.27.2, make, curl.

## Start

```sh
git clone https://github.com/dctellya/artstudio && cd artstudio
cp .env.example .env    # set CONTACT_EMAIL and GRAFANA_ADMIN_PASSWORD
make certs              # local CA + TLS certs in deploy/certs
DOCKER_DEFAULT_PLATFORM= docker compose -f compose.yaml -f compose.obs.yaml up -d --build --wait
```

`compose.obs.yaml` is an overlay on `compose.yaml` (Alloy, Mimir, Tempo, Loki, Grafana), so both files are needed, in that order; `make up` runs the same command and stamps version, commit and build date. `DOCKER_DEFAULT_PLATFORM=` forces a native build; harmless if the variable is not set, required if your shell sets it (Go's TLS under emulation breaks the handshake).


Everything is HTTPS on loopback, signed by the local CA:

| What | URL |
|---|---|
| API | https://api.127.0.0.1.nip.io:8443 (or https://localhost:8443) |
| Metrics | https://api.127.0.0.1.nip.io:9090/metrics (or https://localhost:9090/metrics) |
| Grafana | https://grafana.127.0.0.1.nip.io:3000 (or https://localhost:3000) |

## Try it

```sh
CA=deploy/certs/ca.pem
API=https://api.127.0.0.1.nip.io:8443

curl -si --cacert "$CA" "$API/art/monet?limit=3" | grep -i x-cache   # x-cache: miss
curl -si --cacert "$CA" "$API/art/monet?limit=3" | grep -i x-cache   # x-cache: hit
curl -s  --cacert "$CA" "$API/artworks/27992"                          # one artwork with its image URL
curl -s  --cacert "$CA" "$API/artworks/999999999"                      # 404 problem+json with request id
curl -s  --cacert "$CA" "$API/art/monet?limit=51"                      # 400 from the OpenAPI contract
curl -s  --cacert "$CA" "$API/ready"                                   # vendor probe and breaker state
curl -s  --cacert "$CA" https://api.127.0.0.1.nip.io:9090/metrics | grep ^artstudio_
```

API docs (Swagger UI): https://api.127.0.0.1.nip.io:8443/docs. The browser warns because the CA is local; trust `deploy/certs/ca.pem` or click through.

## Verify each pattern

One command per claim. `CA` and `API` as above.

| Claim | How to see it |
|---|---|
| Request ids | `curl -si --cacert "$CA" "$API/art/monet" \| grep -i x-request-id`; send your own with `-H 'X-Request-ID: test-1'` and it comes back |
| Contract validation | `curl -s --cacert "$CA" "$API/art/monet?limit=51"` → 400 problem+json naming the field; `?limit=50&page=21` → 400 (the vendor caps its window at 1000 results) |
| Cache miss, hit, negative | the two `x-cache` lines above; `curl -si --cacert "$CA" "$API/artworks/999999999" \| grep -i x-cache` twice → `negative` on the second |
| Per-client rate limit | `seq 40 \| xargs -P 40 -I{} curl -s -o /dev/null -w '%{http_code}\n' --cacert "$CA" "$API/art/cat"` → some 429 with `Retry-After` |
| Client limiter and bulkhead | after the burst: `curl -s --cacert "$CA" https://api.127.0.0.1.nip.io:9090/metrics \| grep -E 'artstudio_upstream_(limiter\|bulkhead)'` |
| Retries and attempts | `curl -s --cacert "$CA" https://api.127.0.0.1.nip.io:9090/metrics \| grep artstudio_upstream_requests_total` (label `attempt`); with the obs stack, a trace in Tempo shows one `vendor.attempt` span per attempt |
| Breaker, stale, timeouts, Retry-After | with the chaos profile (below): `make chaos FAULT=503` then a few new searches → `/ready` shows the breaker open, a known query answers with `x-cache: stale`, a new one 503 with `Retry-After`; `make heal` closes it |
| Audit log off the request path | the Database section; `artstudio_audit_queue_depth` and `artstudio_audit_dropped_total` on the metrics endpoint |
| Graceful shutdown | `docker compose stop api` then `docker compose logs api \| tail` → `draining` and `audit flushed` lines, exit 0 (`docker inspect -f '{{.State.ExitCode}}' $(docker compose ps -aq api)`) |
| Load shedding | `artstudio_http_shed_total` in metrics (needs a load tool to trigger; the unit test covers it) |
| TLS only | `curl -s http://localhost:8443/health` gets no service (400 "HTTP request to an HTTPS server"); `curl --cacert "$CA" https://localhost:8443/health` works; `openssl s_client -connect localhost:8443 -tls1_2 </dev/null` is refused |
| Logs ↔ traces | `GRAFANA_ADMIN_PASSWORD=... make test-obs`; in Grafana, a Loki line's `trace_id` opens Tempo and a span's "Logs for this span" opens Loki |
| Signed image, SBOM | `docker pull ghcr.io/dctellya/artstudio:main` works anonymously; `cosign verify ghcr.io/dctellya/artstudio:main --certificate-identity-regexp 'github.com/dctellya/artstudio' --certificate-oidc-issuer https://token.actions.githubusercontent.com`; the SPDX SBOM is the `sbom-spdx` artifact of the CI `image` job; trivy fails the job on CRITICAL |
| SLOs and alerts | Grafana → Alerting → rules: the burn-rate and breaker alerts with their runbook links to this README |

### Chaos profile

```sh
make chaos-up             # api now talks to the vendor stub, with 1s cache TTLs so stale shows quickly
make chaos FAULT=503      # or 429, slow, timeout
make heal
```

The stub serves the same two endpoints from fixtures captured from the real API and takes faults through a control endpoint; it is also the fixture of the client's integration tests. `make up` puts the api back on the real vendor.

## Database

The audit rows go to the `vendor_calls` table in the `postgres` container, which is not published to the host. Query it through compose from the repo root:

```sh
# latest rows written by the API (one per vendor call)
docker compose exec postgres psql -U artstudio -c \
  'select ts, operation, cache_status, upstream_status, attempts, latency_ms, breaker_state from vendor_calls order by ts desc limit 10'

# counts per operation and cache result
docker compose exec postgres psql -U artstudio -c \
  'select operation, cache_status, count(*) from vendor_calls group by 1, 2 order by 1, 2'

# what the vendor actually returned for the latest miss (stored as jsonb)
docker compose exec postgres psql -U artstudio -c \
  "select ts, response_bytes, left(response::text, 200) from vendor_calls where cache_status = 'miss' order by ts desc limit 1"
```

Pool and audit-queue metrics are on the metrics endpoint as `artstudio_db_pool_*` and `artstudio_audit_*`.

## Observability

Generate some traffic:

```sh
for i in $(seq 30); do curl -s -o /dev/null --cacert "$CA" "$API/art/cat?page=$((i % 5 + 1))"; done
```

Open https://grafana.127.0.0.1.nip.io:3000 and log in as `admin` with `GRAFANA_ADMIN_PASSWORD`.

- **Dashboards → artstudio**: service, vendor, cache, SLO and audit rows.
- **Traces**: Explore → Tempo → Search, service name `artstudio`. A trace shows the request, the cache lookup, each vendor attempt (attempt number and status) and the audit write.
- **Logs**: Explore → Loki → `{service_name="artstudio"}`. Each line carries `request_id`, `trace_id` and `span_id` as structured metadata (logs travel over OTLP, no docker socket). To follow one request: `{service_name="artstudio"} | request_id="<id>"` with the id from the response's `X-Request-ID`.
- **From logs to traces and back**: in a log line, the `trace_id` field is a link that opens the trace in Tempo; in a trace, "Logs for this span" opens Loki filtered by that `trace_id`.

## Stop

```sh
docker compose -f compose.yaml -f compose.obs.yaml down       # add -v to delete the data volumes
```

## Tests

```sh
make test               # unit and contract tests, no containers
make test-acceptance    # end-to-end checks against the running stack (12 checks; chaos and stop below)
make test-acceptance-stop                 # graceful stop: SIGTERM → draining → audit flushed → exit 0
make chaos-up && make test-acceptance-chaos   # breaker opens, stale served; needs a fresh chaos-up each run
make obs-validate       # static checks of the obs stack: promtool rules, alloy validate, dashboard, datasources, compose
GRAFANA_ADMIN_PASSWORD=... make test-obs   # live round trip: X-Request-ID → Loki line → trace in Tempo
make ci                 # what CI runs: vet, lint, contract lint, tests, govulncheck, gitleaks, hadolint, image build
```

## Troubleshooting

- **macOS curl says `bad decrypt` or the handshake fails:** the image ran emulated because your shell sets `DOCKER_DEFAULT_PLATFORM=linux/amd64`; keep the `DOCKER_DEFAULT_PLATFORM=` prefix on the compose line.
- **`*.127.0.0.1.nip.io` doesn't resolve:** use the `localhost` URLs; the certs cover them.
- **api unhealthy after `cp .env.example .env` on a machine that ran it before:** the `artstudio_pgdata` volume keeps the old `POSTGRES_PASSWORD`; `docker compose down -v` (deletes the audit rows) or reuse the old password.
- **`/version` says `dev`:** the image was built by `docker compose` without build args; `make up` stamps version, commit and build date.
- **Searches return 503:** the vendor is down or the breaker is open and that query isn't cached; `/ready` shows the state.
- **`docker compose` complains about `GRAFANA_ADMIN_PASSWORD`:** set it in `.env`; the obs overlay refuses an empty admin password.

## Architecture

```
client ──TLS1.3 :8443──▶ api ─┬─ lookup ─ cache (LRU, stale) ─▶ artic client ─https─▶ api.artic.edu (or stub in chaos)
                              │      │        retries · breaker · limiter · bulkhead
                              │      └─ audit queue (1000) ─batch COPY─▶ postgres   (network backend, not published)
reviewer ─TLS :9090 /metrics ─┤
                              └─ OTLP https ─▶ alloy ─▶ mimir / tempo / loki ─▶ grafana ◀─TLS :3000── browser
alloy ─scrape https (CA)─▶ api:9090                     (network obs, not published)
```

## Decisions

| Decision | Why | At scale |
|---|---|---|
| Internal links plaintext on unpublished networks (D-001) | every edge a person touches is TLS 1.3 | TLS with the same CA on every hop |
| Audit Close flushes, ClosePool releases (D-002) | final flush and readiness need the pool | same |
| Audit rows sanitised before COPY (D-003) | one bad row would drop the whole batch | same; per-row fallback |
| Breaker samples per call, local refusals don't count (D-004) | retries absorb transient errors | shared breaker state per vendor |
| Shared vendor call on a detached context (D-005) | one disconnect failed every waiter | same |
| Stale window 60 min, every vendor call audited (D-006) | old artwork records are still right | Redis behind the same Cache interface |
| Contract first, generated router, sanitised 400s (D-007) | one artefact drives docs and validation | same |
| Middleware order, Retry-After defaults (D-008) | reject early, always tell clients when to retry | limiter at the edge proxy |
| Telemetry over OTLP/https, off when unset (D-009) | logs reach Loki without a docker socket | collector per node |
| Chaos via `make chaos-up`, stub on the internal network (D-010) | a profile cannot change the api's env | fault injection in staging |
| CI: three light jobs + image/policy after them (D-011) | wall clock = slowest job, under 2 min warm | SHA-pinned actions, required checks |
| Alloy → Mimir/Tempo/Loki linked by trace id (D-012) | one round trip from `X-Request-ID` | HA backends, object storage |
| Client rate limit 1 req/s burst 5 per process | the vendor allows 60/min anonymous | with N replicas: a shared limiter (Redis) or 1/N each |

Full text: `docs/decisions/`, index in `DECISIONS.md`.

## SLOs and runbooks

| SLO | Target |
|---|---|
| Availability | 99.9% non-5xx over 30 days (stale counts as success) |
| Latency, cache hits | p95 < 20 ms |
| Latency, cache misses | p95 < 400 ms |

### Runbook: fast burn
The 30-day error budget burns 14.4× too fast (1 h and 5 min windows): page.
1. `/ready` → vendor `state`, `last_error`, `breaker`: is the vendor down or the breaker open?
2. `artstudio_upstream_requests_total` by status: vendor 5xx/429 vs our own 503 refusals (limiter, bulkhead)?
3. `artstudio_http_shed_total` and `artstudio_http_in_flight`: are we shedding because of load?

### Runbook: slow burn
The budget burns 6× too fast over 6 h: ticket, not page.
1. Dashboard SLO row: which route and status class carry the 5xx?
2. `artstudio_cache_requests_total{result="stale"}` rising: the vendor is degrading and stale is hiding it.
3. `docker compose logs api | grep '"status":5'` for the request ids, then their traces.

### Runbook: breaker open
1. `/ready` shows `last_error` and when the breaker opened; check https://api.artic.edu/api/v1/artworks?limit=1 from the host.
2. `artstudio_upstream_breaker_transitions_total`: flapping (open/half-open loops) or one long outage?
3. Cache hits and stale responses keep serving; if misses must recover sooner, nothing to do but wait 30 s half-open trials.

### Runbook: audit dropped
1. `artstudio_audit_queue_depth` at 1000 and `artstudio_audit_dropped_total` rising: the writer can't keep up.
2. `artstudio_db_pool_acquired_conns` vs `max_conns` and `artstudio_audit_write_duration_seconds`: is postgres slow?
3. `docker compose logs api | grep audit`: failed batches log their SQLSTATE.

### Runbook: vendor probe down
1. `/ready` vendor block: `last_check`, `last_error`, `latency_ms`.
2. Is it us (limiter rejections, breaker open) or them (timeouts, 5xx)? `artstudio_upstream_limiter_rejected_total`.
3. Stale serving covers known queries for 60 min; new queries answer 503 with Retry-After.

## Audit log

`vendor_calls` (append-only): `ts, request_id, trace_id, operation, cache_key, cache_status, upstream_status, attempts, latency_ms, breaker_state, error, response jsonb, response_bytes`.

Why it exists: an audit of exactly what the vendor returned; a queryable history per request when metrics only aggregate; and the write path is itself a degradation pattern. Rows go through a bounded async queue (1000) flushed in batches with `COPY`; when the queue is full the row is dropped and counted (`artstudio_audit_dropped_total`), never blocking a request. The migration is embedded and idempotent.

## Time spent

Box: 90 minutes, 16:36 → 18:06 UTC on 2026-10-14. Phase 1 (MVP) closed at 17:37 (+61: CI green on its first run, README gate from a clean clone); phase 2 merged at 17:51 (+75). Per-gate times in `docs/time-log.md`; nothing after the box.

## Next steps

See `BACKLOG.md` (B-001..B-042, each with its reason). The main ones:
- **TLS on internal links** (B-031): postgres `sslmode=verify-full` and the obs backends behind the same CA.
- **N replicas**: shared rate limiter and Redis behind the `Cache` interface; migration under an advisory lock (B-004).
- **Cache-hit latency** (B-041): the live run measured p95 ≈ 72 ms against the 20 ms SLO; profile before tuning.
- **Alerting**: latency SLO alerts (B-025), Alertmanager (B-030), Tempo metrics generator (B-029).
- **Supply chain**: SHA-pinned actions (B-001), stub out of the production image (B-037).

## How it was built

All prompts and answers: `docs/transcript.md`, `docs/agents/logs/`, `reports/`. Method: `docs/RULES.md`, `docs/SPEC-TEMPLATE.md`, `docs/TEAM-PROTOCOL.md`.
