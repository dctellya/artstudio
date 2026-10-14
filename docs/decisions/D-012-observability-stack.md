# D-012 Observability stack: Alloy → Mimir/Tempo/Loki, linked by trace id

- **What:** the api sends traces and logs over OTLP/https to Alloy (TLS 1.3, `min_version = "1.3"`, the only syntax Alloy v1.20.1 accepts); Alloy scrapes the api's `:9090` over https with the CA and forwards to Mimir (remote write), Tempo and Loki (OTLP) on the unpublished `obs` network; Grafana (HTTPS, required admin password) is provisioned from files: datasources `mimir`, `loki`, `tempo`, one dashboard, rules in Mimir's ruler. Loki keeps `trace_id`/`span_id`/`request_id` as structured metadata, so the derived field uses the label matcher and Tempo's "logs for this span" is a custom query on `trace_id`.
- **Why:** no docker socket anywhere; the logs↔traces round trip must work from a response's `X-Request-ID`.
- **Alternatives:** Promtail/Alloy tailing container logs; regex derived fields on JSON bodies (do not match OTLP logs).
- **Consequences:** Alloy keeps its WAL on a tmpfs (`--storage.path=/tmp/alloy-data`) because `cap_drop: ALL` removes root's access to `/var/lib/alloy`; data in flight is lost on restart. No Alertmanager: alerts evaluate, are not delivered (B-030).
