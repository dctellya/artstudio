# D-001 Internal links stay plaintext on unpublished networks

- **What:** api→postgres, api→stub (chaos profile), Alloy/Grafana→Mimir/Tempo/Loki use plaintext on the compose networks `backend` and `obs`, which publish nothing to the host.
- **Why:** the box is 90 minutes; every edge a reviewer touches (8443, 9090, 3000, OTLP to Alloy) is TLS 1.3 from the local CA.
- **Alternatives:** TLS on every hop with the same CA (sslmode=verify-full, per-backend certs).
- **Consequences:** a container on those networks could sniff traffic. TLS for internal links is in BACKLOG.
