# README skeleton

The README of the repository follows this structure, in this order, with every placeholder filled from the build. It starts with the walking skeleton and each merge completes its section. Nothing goes before "What it is". Keep the commands literal: a reader copies them.

---

# <name>

<name> is a gateway in front of a third-party API (the Art Institute of Chicago), built to show what belongs around any vendor call in production:

- **Resilience**: per-attempt timeouts and a total budget, retries with jitter honouring Retry-After, circuit breaker, client-side rate limit, bulkhead; typed errors mapped to honest status codes.
- **Cache** with stale serving: when the vendor fails or the breaker is open, a stale answer beats a 502.
- **Audit log** in Postgres: one row per vendor call with what the vendor returned, written off the request path through a bounded queue.
- **TLS everywhere on the edges**, a local CA you generate, everything published on loopback only.
- **Operability**: `/health`, `/ready` (reports the vendor and the breaker, never gates on them), `/metrics`, `/version`, and the API described in `/docs` as an OpenAPI contract that also validates requests.
- **Observability** on the Grafana stack: metrics, traces and logs, linked by trace id, one dashboard with the SLOs and the error budget, alert rules with runbooks.
- **Hardening**: per-client rate limit, load shedding, request ids, panic recovery, graceful shutdown, config from the environment, no secrets in logs; a distroless, non-root, multi-arch image.
- **CI** as a gate: lint, unit and contract tests, vulnerability scan, secrets scan, image scan, SBOM, keyless signature, policy checks; acceptance tests end to end against the running stack.

Images are never fetched: the service builds the IIIF URL and the client hotlinks it.

## Requirements

Docker with Compose v2, Go <version>, make, curl.

## Start

```sh
git clone <repo url> && cd <name>
cp .env.example .env    # set CONTACT_EMAIL and GRAFANA_ADMIN_PASSWORD
make certs              # local CA + TLS certs in deploy/certs
DOCKER_DEFAULT_PLATFORM= docker compose -f compose.yaml -f compose.obs.yaml up -d --build --wait
```

`compose.obs.yaml` is an overlay on `compose.yaml`, so both files are needed, in that order. `DOCKER_DEFAULT_PLATFORM=` forces a native build; harmless if the variable is not set.

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
curl -s  --cacert "$CA" "$API/artworks/<id>"                           # one artwork with its image URL
curl -s  --cacert "$CA" "$API/artworks/999999999"                      # 404 problem+json with request id
curl -s  --cacert "$CA" "$API/art/monet?limit=51"                      # 400 from the OpenAPI contract
curl -s  --cacert "$CA" "$API/ready"                                   # vendor probe and breaker state
curl -s  --cacert "$CA" https://api.127.0.0.1.nip.io:9090/metrics | grep ^<namespace>_
```

API docs (Swagger UI): https://api.127.0.0.1.nip.io:8443/docs. The browser warns because the CA is local; trust `deploy/certs/ca.pem` or click through.

## Verify each pattern

One command per claim. `CA` and `API` as above.

| Claim | How to see it |
|---|---|
| Request ids | `curl -si --cacert "$CA" "$API/art/monet" \| grep -i x-request-id`; send your own with `-H 'X-Request-ID: test-1'` and it comes back |
| Contract validation | `curl -s --cacert "$CA" "$API/art/monet?limit=51"` → 400 problem+json naming the field |
| Cache miss, hit, negative | the two `x-cache` lines above; `curl -si --cacert "$CA" "$API/artworks/999999999" \| grep -i x-cache` twice → `negative` on the second |
| Per-client rate limit | `seq 40 \| xargs -P 40 -I{} curl -s -o /dev/null -w '%{http_code}\n' --cacert "$CA" "$API/art/cat"` → some 429 with `Retry-After` |
| Client limiter and bulkhead | after the burst: `curl -s --cacert "$CA" https://api.127.0.0.1.nip.io:9090/metrics \| grep -E '<namespace>_upstream_(limiter|bulkhead)'` |
| Retries and attempts | a trace in Tempo: one span per vendor attempt with its status |
| Breaker, stale, timeouts, Retry-After | with the chaos profile (below): `make chaos FAULT=503` then two requests → `/ready` shows the breaker open, a known query answers with `x-cache: stale`; `make heal` closes it |
| Audit log off the request path | the Database section; `<namespace>_audit_queue_depth` and `<namespace>_audit_dropped_total` on the metrics endpoint |
| Graceful shutdown | `docker compose stop api` then `docker compose logs api \| tail` → drain and flush lines, exit 0 |
| Load shedding | `<namespace>_http_shed_total` in metrics (needs a load tool to trigger; the unit test covers it) |
| TLS only | `curl -s http://localhost:8443/health` fails; `curl --cacert "$CA" https://localhost:8443/health` works; `openssl s_client -connect localhost:8443 -tls1_2` is refused |
| Signed image, SBOM | `cosign verify ghcr.io/<owner>/<name>:<tag> --certificate-identity-regexp 'github.com/<owner>/<name>' --certificate-oidc-issuer https://token.actions.githubusercontent.com`; the SBOM is an artifact of the image job |
| SLOs and alerts | Grafana → Alerting → rules: the burn-rate and breaker alerts with their runbook links to this README |

### Chaos profile

```sh
docker compose -f compose.yaml -f compose.obs.yaml --profile chaos up -d --wait   # api now talks to the vendor stub
make chaos FAULT=503      # or 429, slow, timeout
make heal
```

The stub serves the same two endpoints from fixtures captured from the real API and takes faults through a control endpoint; it is also the fixture of the client's integration tests.

## Database

The audit rows go to the `vendor_calls` table in the `postgres` container, which is not published to the host. Query it through compose from the repo root:

```sh
# latest rows written by the API (one per vendor call)
docker compose exec postgres psql -U <user> -c \
  'select ts, operation, cache_status, upstream_status, attempts, latency_ms, breaker_state from vendor_calls order by ts desc limit 10'

# counts per operation and cache result
docker compose exec postgres psql -U <user> -c \
  'select operation, cache_status, count(*) from vendor_calls group by 1, 2 order by 1, 2'

# what the vendor actually returned for the latest miss (stored as jsonb)
docker compose exec postgres psql -U <user> -c \
  "select ts, response_bytes, left(response::text, 200) from vendor_calls where cache_status = 'miss' order by ts desc limit 1"
```

Pool and audit-queue metrics are on the metrics endpoint as `<namespace>_db_pool_*` and `<namespace>_audit_*`.

## Observability

Generate some traffic:

```sh
for i in $(seq 30); do curl -s -o /dev/null --cacert "$CA" "$API/art/cat?page=$((i % 5 + 1))"; done
```

Open https://grafana.127.0.0.1.nip.io:3000 and log in as `admin` with `GRAFANA_ADMIN_PASSWORD`.

- **Dashboards → <name>**: service, vendor, cache, SLO and audit rows.
- **Traces**: Explore → Tempo → Search, service name `<name>`. A trace shows the request, the cache lookup, each vendor attempt (attempt number and status) and the audit write.
- **Logs**: Explore → Loki → `{service_name="<name>"}`. Each line is JSON with `request_id`, `trace_id` and `span_id`. To follow one request: `{service_name="<name>"} | request_id="<id>"` with the id from the response's `X-Request-ID`.
- **From logs to traces and back**: in a log line, the `trace_id` field is a link that opens the trace in Tempo; in a trace, "Logs for this span" opens Loki filtered by that `trace_id`.

## Stop

```sh
docker compose -f compose.yaml -f compose.obs.yaml down       # add -v to delete the data volumes
```

## Tests

```sh
make test               # unit and contract tests, no containers
make test-acceptance    # end-to-end checks against the running stack (<n> checks)
make ci                 # what CI runs: vet, lint, tests, govulncheck, gitleaks, hadolint, image build
```

## Troubleshooting

- **macOS curl says `bad decrypt`:** the image ran emulated because your shell sets `DOCKER_DEFAULT_PLATFORM=linux/amd64`; keep the `DOCKER_DEFAULT_PLATFORM=` prefix on the compose line.
- **`*.127.0.0.1.nip.io` doesn't resolve:** use the `localhost` URLs; the certs cover them.
- **Searches return 503:** the vendor is down or the breaker is open and that query isn't cached; `/ready` shows the state.

## Architecture

<ASCII diagram: client → api (TLS) → vendor; cache; audit queue → postgres; metrics/traces/logs → Alloy → Mimir/Tempo/Loki → Grafana.>

## Decisions

<One row per decision: what, why, what changes at scale. Full text in docs/decisions/.>

## SLOs and runbooks

<The SLOs with their targets; one short runbook section per alert, three checks each; the alerts link here.>

## Audit log

<The table and the three reasons it exists; how it is written (async, bounded, dropped counted).>

## Time spent

<The real figure, in two lines: the box, and anything after it.>

## Next steps

<What was left out and the one-line design for each.>

## How it was built

All prompts and answers: `docs/transcript.md`, `docs/agents/logs/`, `reports/`. Method: `docs/RULES.md`, `docs/SPEC-TEMPLATE.md`, `docs/TEAM-PROTOCOL.md`.
