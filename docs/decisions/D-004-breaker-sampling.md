# D-004 Breaker samples per client call; local rejections are not failures

- **What:** gobreaker counts one sample per `Client` call (after its retries), not per attempt; 404, caller cancellation, limiter/bulkhead rejections do not count as failures. States are reported as `closed`, `half-open`, `open`.
- **Why:** retries already absorb transient errors inside one call; counting attempts would open the breaker on a single bad request; local refusals say nothing about the vendor's health.
- **Alternatives:** per-attempt sampling (faster to open, noisier).
- **Consequences:** 5 consecutive failed calls (up to 15 attempts) open it; documented in README.
