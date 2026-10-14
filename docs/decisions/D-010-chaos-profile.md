# D-010 Chaos profile: `make chaos-up`, stub on the internal network

- **What:** `make chaos-up` restarts the stack with `UPSTREAM_BASE_URL=http://stub:8080/api/v1` and 1 s cache TTLs (stale window unchanged), profile `chaos`; `make chaos FAULT=429|503|slow|timeout` and `make heal` drive the stub through `docker compose exec stub /stubserver ctl`. The stub has no healthcheck (the image's would probe the api binary) and plain http on `backend` only. `make up` returns to the real vendor.
- **Why:** a compose profile cannot change another service's environment (F-004); 1 s TTLs make stale visible within seconds.
- **Alternatives:** a second api service in the profile; a compose override file.
- **Consequences:** each chaos acceptance run needs a fresh `make chaos-up` (the breaker stays open for 30 s after a run).
