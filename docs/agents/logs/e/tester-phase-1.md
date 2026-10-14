# Team E tester, phase 1

## 2026-10-14T16:58:49Z prompt received (verbatim)

You are the TESTER of Team E (observability stack), phase 1, per docs/TEAM-PROTOCOL.md. Worktree: /Users/dct/code/EllyaOrg/projects/dalimaray/artstudio-wt/e (branch team-e/phase-1). Work only there. Your spec: .../scratchpad/briefs/team-e.md (frozen names: services, networks, certs, metric names, Loki label, Tempo service name). Budget: ~10 minutes.

Rules: Never amend, reset, rebase or stash; commit forward only; never push; never touch main or another worktree. Do not write the stack (compose.obs.yaml, deploy/obs/** configs) — the CODER does; do not edit Makefile, compose.yaml, Dockerfile, go.mod. Image versions in SPEC §4 are verified; do not check docs. Do not read docs/assignment.md or the full SPEC.md.
Write exactly the tests SPEC §6 lists for Team E: "obs static" and "obs live".
- obs static: a script deploy/obs/validate.sh (bash, set -euo pipefail; the Makefile's `obs-validate` target will call it at wiring) that runs, all via docker images from §4 (no local installs): promtool check rules on deploy/obs/rules/*.yml (promtool from the prom/prometheus image acceptable — say which tag and that it is test-only; or mimirtool), alloy validate (grafana/alloy:v1.20.1, alloy fmt/validate) on deploy/obs/alloy/config.alloy, jq empty (or python3 -m json.tool) on deploy/obs/grafana/dashboards/*.json plus checks that the dashboard has rows titled service, vendor, cache, SLO, audit and that every artstudio_* metric name used exists in the frozen list, that the Loki datasource has a derived field on trace_id linking to the Tempo datasource uid, Tempo datasource has tracesToLogsV2 to Loki and tracesToMetrics, alert rules carry runbook_url pointing to README anchors, and `docker compose -f compose.yaml -f compose.obs.yaml config -q`; plus a grep check that no service publishes anything other than 127.0.0.1:3000 in compose.obs.yaml and no insecure_skip_verify appears under deploy/obs. Prefix docker calls with DOCKER_DEFAULT_PLATFORM=.
- obs live: test/obs/obs_test.go with build tag `obs` (Go, testify): request to https://localhost:8443/art/monet with the CA deploy/certs/ca.pem, take X-Request-ID, query Loki (through Grafana's datasource proxy at https://localhost:3000, admin + GRAFANA_ADMIN_PASSWORD env) for the access log line with that request_id, extract trace_id, assert the trace exists in Tempo via the same proxy, and that the Loki datasource's derived field targets Tempo. Retries with deadline 60s for ingestion.
Both must fail now for a readable reason. Commit as `test(e-1): …` with trailer `Co-Authored-By: Claude Sonnet <noreply@anthropic.com>`.
Run `bash deploy/obs/validate.sh` and `go test -tags obs ./test/obs/...` and paste the red output. End with a TEST BRIEF. Logging: docs/agents/logs/e/tester-phase-1.md, timestamps from date -u; commit it.
(Abbreviated copy of the lead's message; the lead's message is authoritative.)

## 2026-10-14T16:55:35Z read brief, compose.yaml, Makefile (obs targets are not_yet stubs), go.mod. Worktree has no deploy/, test/obs, certs (git-ignored).

## 2026-10-14T16:58:49Z wrote deploy/obs/validate.sh and test/obs/obs_test.go
Checks: go vet -tags obs OK, bash -n OK. First validate run hit bash 3.2 'unbound variable' on empty arrays under set -u; fixed with ${arr[@]+...}. Python check block fixture-tested in scratchpad (valid data passes; unknown metric and non-grafana published port both caught). Fixed a bug found there (networks list handling).
promtool: prom/prometheus:latest (override PROMTOOL_IMAGE); yq: mikefarah/yq:latest (override YQ_IMAGE), both test-only, used to read YAML as JSON. Decision: latest per user's global rule; pin by env if desired.

## 2026-10-14T16:58:49Z red run: bash deploy/obs/validate.sh (exit 1)
```

== 1. promtool check rules (prom/prometheus:latest, test-only)
FAIL: no rule files match deploy/obs/rules/*.yml

== 2. alloy validate (grafana/alloy:v1.20.1)
FAIL: missing deploy/obs/alloy/config.alloy

== 3. dashboards parse (python3 -m json.tool)
FAIL: no dashboards match deploy/obs/grafana/dashboards/*.json

== 4. convert YAML (rules, datasources, compose.obs.yaml) to JSON via mikefarah/yq:latest
FAIL: no datasource files match deploy/obs/grafana/provisioning/datasources/*.y*ml
FAIL: missing compose.obs.yaml

== 5. dashboard rows, metric names, datasources, alert runbooks
FAIL: no alert rules found in deploy/obs/rules/*.yml
FAIL: no artstudio_* metric is used by any dashboard or rule
FAIL: datasource uid 'loki' missing
FAIL: datasource uid 'tempo' missing
FAIL: datasource uid 'mimir' missing
FAIL: loki datasource: need a derivedFields entry on trace_id with datasourceUid: tempo
FAIL: tempo datasource: jsonData.tracesToLogsV2.datasourceUid must be loki
FAIL: tempo datasource: jsonData.tracesToMetrics.datasourceUid must be mimir

== 6. no insecure TLS under deploy/obs
ok:   no insecure_skip_verify

== 7. docker compose config
open /Users/dct/code/EllyaOrg/projects/dalimaray/artstudio-wt/e/compose.obs.yaml: no such file or directory
FAIL: docker compose -f compose.yaml -f compose.obs.yaml config -q

obs-validate: 7 check(s) FAILED
```

## 2026-10-14T16:58:49Z red run: GRAFANA_ADMIN_PASSWORD=x go test -tags obs ./test/obs/... (60s, exit 1)
```
TestRequestIDToLokiToTempo: Get "https://localhost:8443/art/monet": x509 certificate signed by unknown authority ... api not reachable at https://localhost:8443/art/monet: is the stack up?
   (cause here: worktree certs differ from the CA of the api container already running from main; a fresh `make certs` was generated then deleted again)
TestLokiDerivedFieldTargetsTempo: grafana not reachable at https://localhost:3000: dial tcp [::1]:3000: connect: connection refused ... grafana datasource loki: not satisfied within 1m0s
```
Without certs the test fails earlier with: CA missing: run `make certs`.

## 2026-10-14T17:01:23Z TEST BRIEF

TEST BRIEF (Team E, phase 1)

Commands: `bash deploy/obs/validate.sh` (Makefile obs-validate must call exactly this; needs docker, python3); `GRAFANA_ADMIN_PASSWORD=... go test -tags obs ./test/obs/...` (Makefile test-obs; stack up, `make certs` done, password from .env).

obs static (validate.sh; runs all checks, reports every FAIL, exit 1 if any):
1. promtool check rules on deploy/obs/rules/*.yml (prom/prometheus:latest, test-only; env PROMTOOL_IMAGE).
2. alloy validate (falls back to alloy fmt) of deploy/obs/alloy/config.alloy, image grafana/alloy:v1.20.1.
3. deploy/obs/grafana/dashboards/*.json parse (python3 -m json.tool).
4. Dashboard has row panels whose titles contain (case-insensitive) service, vendor, cache, slo, audit (nested rows ok).
5. Every artstudio_[a-zA-Z0-9_]+ token in dashboard targets/variables and rule exprs is in the frozen list (histograms accept _bucket/_sum/_count) or is a `record:` name defined in the rules. Recording rules named with colons (artstudio:slo:...) are never flagged.
6. Datasources (yaml parsed via mikefarah/yq, test-only): uid loki type loki with jsonData.derivedFields[] containing matcherRegex with `trace_id` and datasourceUid: tempo; uid tempo type tempo with jsonData.tracesToLogsV2.datasourceUid: loki and jsonData.tracesToMetrics.datasourceUid: mimir; uid mimir type prometheus.
7. Every `alert:` rule has annotations.runbook_url ending in README.md#<lowercase-anchor> (at least one alert required).
8. compose.obs.yaml: services alloy, mimir, tempo, loki, grafana each on network obs (list or map form); only grafana may publish ports, exactly the string "127.0.0.1:3000:3000".
9. grep -rIEi 'insecure_?skip_?verify|tls_?skip_?verify' under deploy/obs finds nothing (Grafana datasources must NOT use tlsSkipVerify; use the CA).
10. docker compose -f compose.yaml -f compose.obs.yaml config -q (script copies .env.example to .env temporarily if .env is absent, and defaults POSTGRES_PASSWORD, GRAFANA_ADMIN_PASSWORD to a placeholder).

obs live (test/obs/obs_test.go, tag obs, 60s retry):
- TestRequestIDToLokiToTempo: GET https://localhost:8443/art/monet with deploy/certs/ca.pem -> 200 + X-Request-ID; via https://localhost:3000 (basic auth admin / GRAFANA_ADMIN_PASSWORD) GET /api/datasources/proxy/uid/loki/loki/api/v1/query_range with {service_name="artstudio"} filtered by `| request_id="<id>"` (structured metadata) or `|= "<id>"` (JSON line); needs a line with msg=="request" (or body "request"), request_id and trace_id (from stream labels/metadata or JSON body); trace_id is 32 hex; then GET /api/datasources/proxy/uid/tempo/api/traces/<trace_id> returns 200 and contains "artstudio".
- TestLokiDerivedFieldTargetsTempo: GET /api/datasources/uid/loki has jsonData.derivedFields with datasourceUid "tempo" and matcherRegex containing trace_id.

CODER must produce:
- compose.obs.yaml (services alloy, mimir, tempo, loki, grafana; network obs; only grafana publishes "127.0.0.1:3000:3000"; Grafana TLS with deploy/certs/grafana{,-key}.pem; admin user "admin", password from GRAFANA_ADMIN_PASSWORD).
- deploy/obs/alloy/config.alloy; deploy/obs/rules/*.yml (recording + alert rules, runbook_url .../README.md#anchor); deploy/obs/grafana/dashboards/*.json; deploy/obs/grafana/provisioning/datasources/*.yml or *.yaml (top-level `datasources:` list, uids exactly loki, tempo, mimir); a dashboards provider yaml under deploy/obs/grafana/provisioning/dashboards/.
- Loki must index service_name="artstudio" and expose request_id/trace_id as structured metadata (OTLP) or in a JSON line; Grafana's proxy must reach Loki at /loki/api/v1/query_range and Tempo at /api/traces/<id>.
- Makefile wiring (team C): obs-validate -> `bash deploy/obs/validate.sh`; test-obs -> `go test -tags obs -count=1 ./test/obs/...` with GRAFANA_ADMIN_PASSWORD exported.

## 2026-10-14T17:01:48Z pinned test-only images in validate.sh (lead request): prom/prometheus:v3.15.0, mikefarah/yq:4.54.1 (no :latest); env overrides kept.
