#!/usr/bin/env bash
# Static validation of the observability stack (`make obs-validate`).
# Every tool runs from a docker image; nothing is installed locally.
# promtool and yq are TEST-ONLY tools: they are never part of the shipped stack.
set -euo pipefail

cd "$(dirname "$0")/../.."

# Test-only tool images, pinned (verified 2026-10-14). Override via env.
PROMTOOL_IMAGE="${PROMTOOL_IMAGE:-prom/prometheus:v3.15.0}"
YQ_IMAGE="${YQ_IMAGE:-mikefarah/yq:4.54.1}"
ALLOY_IMAGE="${ALLOY_IMAGE:-grafana/alloy:v1.20.1}"

OBS_DIR=deploy/obs
RULES_GLOB="$OBS_DIR/rules/*.yml"
ALLOY_CONF="$OBS_DIR/alloy/config.alloy"
DASH_GLOB="$OBS_DIR/grafana/dashboards/*.json"
DS_GLOB="$OBS_DIR/grafana/provisioning/datasources/*.y*ml"
COMPOSE_OBS=compose.obs.yaml

export DOCKER_DEFAULT_PLATFORM=
WORK="$(mktemp -d)"
CREATED_ENV=0
cleanup() {
  rm -rf "$WORK"
  if [ "$CREATED_ENV" = 1 ]; then rm -f .env; fi
}
trap cleanup EXIT

FAILS=0
fail() { echo "FAIL: $*" >&2; FAILS=$((FAILS + 1)); }
ok() { echo "ok:   $*"; }
step() { echo; echo "== $*"; }

shopt -s nullglob
rules=($RULES_GLOB)
dashes=($DASH_GLOB)
dss=($DS_GLOB)

# yq_json FILE -> JSON on stdout (docker, no local yq)
yq_json() {
  docker run --rm -v "$PWD:/w:ro" -w /w "$YQ_IMAGE" -o=json '.' "$1"
}

step "1. promtool check rules ($PROMTOOL_IMAGE, test-only)"
if [ ${#rules[@]} -eq 0 ]; then
  fail "no rule files match $RULES_GLOB"
elif docker run --rm --entrypoint promtool -v "$PWD:/w:ro" -w /w "$PROMTOOL_IMAGE" check rules "${rules[@]}"; then
  ok "promtool check rules"
else
  fail "promtool check rules"
fi

step "2. alloy validate ($ALLOY_IMAGE)"
if [ ! -f "$ALLOY_CONF" ]; then
  fail "missing $ALLOY_CONF"
else
  # `alloy validate` where the binary has it, else `alloy fmt` (syntax check) as fallback.
  if docker run --rm -v "$PWD/$OBS_DIR/alloy:/cfg:ro" "$ALLOY_IMAGE" validate --help >/dev/null 2>&1; then
    alloy_cmd=(validate /cfg/config.alloy)
  else
    alloy_cmd=(fmt /cfg/config.alloy)
  fi
  if docker run --rm -v "$PWD/$OBS_DIR/alloy:/cfg:ro" "$ALLOY_IMAGE" "${alloy_cmd[@]}" >/dev/null; then
    ok "alloy ${alloy_cmd[0]}"
  else
    fail "alloy ${alloy_cmd[0]} $ALLOY_CONF"
  fi
fi

step "3. dashboards parse (python3 -m json.tool)"
if [ ${#dashes[@]} -eq 0 ]; then
  fail "no dashboards match $DASH_GLOB"
else
  for f in ${dashes[@]+"${dashes[@]}"}; do
    if python3 -m json.tool "$f" >/dev/null; then ok "$f parses"; else fail "$f is not valid JSON"; fi
  done
fi

step "4. convert YAML (rules, datasources, compose.obs.yaml) to JSON via $YQ_IMAGE"
mkdir -p "$WORK/rules" "$WORK/ds"
for f in ${rules[@]+"${rules[@]}"}; do yq_json "$f" >"$WORK/rules/$(basename "$f").json" || fail "yq $f"; done
for f in ${dss[@]+"${dss[@]}"}; do yq_json "$f" >"$WORK/ds/$(basename "$f").json" || fail "yq $f"; done
[ ${#dss[@]} -gt 0 ] || fail "no datasource files match $DS_GLOB"
if [ -f "$COMPOSE_OBS" ]; then
  yq_json "$COMPOSE_OBS" >"$WORK/compose.obs.json" || fail "yq $COMPOSE_OBS"
else
  fail "missing $COMPOSE_OBS"
fi

step "5. dashboard rows, metric names, datasources, alert runbooks"
if ! python3 - "$WORK" ${dashes[@]+"${dashes[@]}"} <<'PY'
import glob, json, re, sys, os

work, dashes = sys.argv[1], sys.argv[2:]
errs = []
def err(m): errs.append(m)

# ---- frozen metric names (SPEC section 5), prefixed artstudio_
frozen = """
http_requests_total http_request_duration_seconds http_in_flight http_shed_total http_rate_limited_total
upstream_requests_total upstream_request_duration_seconds upstream_retries_total upstream_breaker_state
upstream_breaker_transitions_total upstream_limiter_wait_seconds upstream_limiter_rejected_total
upstream_bulkhead_in_use upstream_bulkhead_rejected_total upstream_probe_up upstream_probe_latency_seconds
cache_requests_total cache_entries cache_evictions_total cache_singleflight_dedupes_total
audit_queue_depth audit_writes_total audit_dropped_total audit_write_duration_seconds
db_pool_total_conns db_pool_idle_conns db_pool_acquired_conns db_pool_max_conns
db_pool_acquire_total db_pool_acquire_duration_seconds_total build_info
""".split()
frozen = {"artstudio_" + n for n in frozen}
hist = {"artstudio_http_request_duration_seconds", "artstudio_upstream_request_duration_seconds",
        "artstudio_upstream_limiter_wait_seconds", "artstudio_upstream_probe_latency_seconds",
        "artstudio_audit_write_duration_seconds"}
allowed = set(frozen)
for h in hist:
    allowed |= {h + "_bucket", h + "_sum", h + "_count"}

# ---- rules
records, alert_count = set(), 0
exprs = []
for rf in sorted(glob.glob(os.path.join(work, "rules", "*.json"))):
    doc = json.load(open(rf))
    for g in doc.get("groups") or []:
        for r in g.get("rules") or []:
            if "record" in r:
                records.add(r["record"])
            if "expr" in r:
                exprs.append((os.path.basename(rf) + ":" + (r.get("alert") or r.get("record", "?")), str(r["expr"])))
            if "alert" in r:
                alert_count += 1
                url = (r.get("annotations") or {}).get("runbook_url", "")
                if not re.search(r"README\.md#[a-z0-9][a-z0-9-]*$", url):
                    err("alert %s: annotations.runbook_url must end with README.md#<anchor>, got %r" % (r["alert"], url))
if alert_count == 0:
    err("no alert rules found in deploy/obs/rules/*.yml")

# ---- dashboards
required_rows = ["service", "vendor", "cache", "slo", "audit"]
def walk(panels):
    for p in panels or []:
        yield p
        yield from walk(p.get("panels"))
for df in dashes:
    d = json.load(open(df))
    panels = list(walk(d.get("panels")))
    rows = [p.get("title", "").lower() for p in panels if p.get("type") == "row"]
    for want in required_rows:
        if not any(want in t for t in rows):
            err("%s: no row panel titled %r (rows: %s)" % (df, want, rows))
    for p in panels:
        for t in p.get("targets") or []:
            if "expr" in t:
                exprs.append((df + ":" + str(p.get("title")), t["expr"]))
    for v in (d.get("templating") or {}).get("list") or []:
        q = v.get("query")
        if isinstance(q, str):
            exprs.append((df + ":var " + v.get("name", "?"), q))
        elif isinstance(q, dict) and "query" in q:
            exprs.append((df + ":var " + v.get("name", "?"), str(q["query"])))

used = set()
for where, e in exprs:
    for m in re.findall(r"artstudio_[a-zA-Z0-9_]+", e):
        used.add(m)
        if m not in allowed and m not in records:
            err("%s uses unknown metric %s (not in the frozen list)" % (where, m))
if not used:
    err("no artstudio_* metric is used by any dashboard or rule")

# ---- datasources
dss = {}
for f in sorted(glob.glob(os.path.join(work, "ds", "*.json"))):
    for ds in (json.load(open(f)) or {}).get("datasources") or []:
        dss[ds.get("uid")] = ds
for uid, typ in (("loki", "loki"), ("tempo", "tempo"), ("mimir", "prometheus")):
    if uid not in dss:
        err("datasource uid %r missing" % uid)
    elif dss[uid].get("type") != typ:
        err("datasource %r must be type %r" % (uid, typ))
loki, tempo = dss.get("loki", {}), dss.get("tempo", {})
dfs = (loki.get("jsonData") or {}).get("derivedFields") or []
# otelslog over OTLP puts trace_id in structured metadata, not in the line body.
if not any(d.get("datasourceUid") == "tempo" and d.get("matcherType") == "label" and d.get("matcherRegex") == "trace_id" for d in dfs):
    err("loki datasource: need a derivedFields entry with matcherType: label, matcherRegex: trace_id, datasourceUid: tempo")
tj = tempo.get("jsonData") or {}
tl = tj.get("tracesToLogsV2") or {}
if tl.get("datasourceUid") != "loki":
    err("tempo datasource: jsonData.tracesToLogsV2.datasourceUid must be loki")
tq = str(tl.get("query", "")).replace("$${", "${")
if tl.get("customQuery") is not True:
    err("tempo datasource: tracesToLogsV2.customQuery must be true")
if 'service_name="artstudio"' not in tq or 'trace_id="${__span.traceId}"' not in tq:
    err("tempo datasource: tracesToLogsV2.query must contain service_name=\"artstudio\" and trace_id=\"${__span.traceId}\" (YAML: $${__span.traceId}), got %r" % tl.get("query"))
if (tj.get("tracesToMetrics") or {}).get("datasourceUid") != "mimir":
    err("tempo datasource: jsonData.tracesToMetrics.datasourceUid must be mimir")

# ---- compose.obs.yaml
cf = os.path.join(work, "compose.obs.json")
if os.path.exists(cf):
    svcs = (json.load(open(cf)) or {}).get("services") or {}
    for s in ("alloy", "mimir", "tempo", "loki", "grafana"):
        if s not in svcs:
            err("compose.obs.yaml: service %r missing" % s)
        elif "obs" not in (svcs[s].get("networks") or []):
            err("compose.obs.yaml: service %r must be on network obs" % s)
    for name, s in svcs.items():
        for p in s.get("ports") or []:
            if not (name == "grafana" and str(p) in ("127.0.0.1:3000:3000", "127.0.0.1:3000:3000/tcp")):
                err("compose.obs.yaml: service %s publishes %r; only grafana 127.0.0.1:3000:3000 is allowed" % (name, p))

# grafana admin password must be a required variable
if os.path.exists(cf):
    env = (svcs.get("grafana") or {}).get("environment") or {}
    if isinstance(env, list):
        env = dict(x.split("=", 1) for x in env if "=" in x)
    pw = str(env.get("GF_SECURITY_ADMIN_PASSWORD", ""))
    if not pw.startswith("${GRAFANA_ADMIN_PASSWORD:?"):
        err("compose.obs.yaml: GF_SECURITY_ADMIN_PASSWORD must use the required form ${GRAFANA_ADMIN_PASSWORD:?...}, got %r" % pw)

# alloy otlp receiver TLS 1.3
ac = "deploy/obs/alloy/config.alloy"
if os.path.exists(ac):
    m = re.search(r'^otelcol\.receiver\.otlp\b.*?^}', open(ac).read(), re.S | re.M)
    if not m or not re.search(r'tls\s*\{[^}]*min_version\s*=\s*"1\.3"', m.group(0)):
        err('%s: otelcol.receiver.otlp tls block must set min_version = "1.3" (Alloy syntax)' % ac)

for e in errs:
    print("FAIL: " + e, file=sys.stderr)
sys.exit(1 if errs else 0)
PY
then FAILS=$((FAILS + 1)); else ok "dashboard rows, metrics, datasources, runbooks, published ports"; fi

step "6. no insecure TLS under $OBS_DIR"
if [ -d "$OBS_DIR" ] && grep -rInEi 'insecure_?skip_?verify|tls_?skip_?verify' "$OBS_DIR" --exclude=validate.sh; then
  fail "TLS verification disabled somewhere under $OBS_DIR"
else
  ok "no insecure_skip_verify"
fi

step "7. docker compose config"
if [ ! -f .env ]; then cp .env.example .env && CREATED_ENV=1; fi
export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-placeholder-not-a-secret}"
export GRAFANA_ADMIN_PASSWORD="${GRAFANA_ADMIN_PASSWORD:-placeholder-not-a-secret}"
if docker compose -f compose.yaml -f "$COMPOSE_OBS" config -q; then
  ok "compose config"
else
  fail "docker compose -f compose.yaml -f $COMPOSE_OBS config -q"
fi

echo
if [ "$FAILS" -gt 0 ]; then
  echo "obs-validate: $FAILS check(s) FAILED" >&2
  exit 1
fi
echo "obs-validate: all checks passed"
