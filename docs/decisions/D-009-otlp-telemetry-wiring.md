# D-009 Telemetry over OTLP/https, off when unset

- **What:** when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (compose obs overlay: `https://alloy:4318`) the api exports traces and logs (slog → otelslog bridge, fanned out with stdout JSON) to `<endpoint>/v1/traces` and `/v1/logs`, trusting only `OTEL_EXPORTER_OTLP_CERTIFICATE`; config rejects a non-https endpoint. Unset: no-op providers, stdout JSON only.
- **Why:** logs reach Loki with no docker socket mounted; TLS on every edge; the MVP runs without the obs stack.
- **Alternatives:** Alloy tailing container logs (needs the docker socket); OTLP gRPC.
- **Consequences:** `trace_id`/`span_id` are OTLP fields, so Loki keeps them as structured metadata (Grafana derived field uses the label matcher).
