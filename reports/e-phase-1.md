# Team E — phase 1 (built from t0) and phase 2 integration report

Scope: compose.obs.yaml, deploy/obs/** (Alloy, Mimir, Tempo, Loki, Grafana provisioning, rules, dashboard), test/obs. Time: 16:55 → 17:49 UTC.

## Tests written (red)
3a86c76/26b99db: deploy/obs/validate.sh (7 static checks) and test/obs (live round trip, tag obs). 92ab254: test-only images pinned (prom/prometheus:v3.15.0, mikefarah/yq:4.54.1). 40d1247: checks for label-matcher derived field, custom traces-to-logs query, required admin password, TLS min. f2910a3: Alloy TLS syntax "1.3".

## Code added
c4832a6 stack; e524723 review fixes; 4644477 alloy storage on tmpfs, TLS "1.3", probe latency panel.

## Review
773df55 (opus): 3 BLOCKING (derived field regex on body, traces-to-logs text search, empty admin password) — fixed once; 7 BACKLOG → B-025..B-030.

## Live check (phase 2, after the C wiring)
First run 17:39: FAIL (alloy crash-loop: storage path permission under cap_drop ALL; then "TLS13" unsupported). After 4644477: `go test -tags obs` ok 6.978s (coder), ok 6.192s (lead, 17:48). Loki lines carry request_id/trace_id/span_id as structured metadata; Tempo trace spans GET /art/{query}, HTTP GET, cache.get, vendor.attempt; Mimir: artstudio_http_requests_total 3 series, artstudio_upstream_requests_total 72; ruler groups artstudio-alerts and artstudio-slo-recording health ok; dashboard panels return data (miss p95 NaN when no misses in window).

## Gate (lead)
validate.sh exit 0, "obs-validate: all checks passed"; obs live test ok.

## Deviations
D-012. No healthchecks/read_only on obs services, no metrics_generator, no Alertmanager (B-030). Alloy as root with tmpfs WAL (B-042). Observed hit p95 ~72 ms vs 20 ms SLO (B-041).

## Left red
None.
