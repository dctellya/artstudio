# Team A — phase 1 report

Scope: internal/artic (client, prober), internal/cache, internal/lookup. Time: 16:55 → 17:10 UTC (15 min).

## Tests written (red)
a06de3d (+9561d47 artist_display per spec): artic unit (timeouts, budget, retries, jitter, Retry-After, no redirects, breaker, limiter, bulkhead, typed errors, header, image URL, prober), cache unit, lookup unit, artic integration vs stubserver. Red: build failed (packages missing). 1a0383f: cancelled leader test (red: context canceled). d186839: ctl helper accepts 2xx; half-open needs 3 trials.

## Code added
8869a26: client with retries/backoff/breaker/limiter/bulkhead, typed errors, metrics, vendor.attempt spans, prober; LRU cache with TTL/stale/negative/singleflight; lookup with stale fallback and audit. 05ca1e9: detached singleflight call.

## Review
a83d5e8 (opus): 1 BLOCKING (leader cancellation fails followers) — fixed once. 9 BACKLOG → B-017..B-024.

## Gate (lead, 2026-10-14T17:10:15Z)
```
ok  internal/artic             3.608s
ok  internal/artic/stubserver  5.140s
ok  internal/cache             1.622s
ok  internal/lookup            1.388s
ok  internal/artic (integration) 3.342s
go vet clean; golangci-lint 0 issues
```

## Deviations
WithHTTPClient option; (*LRU).Checker(); lookup registers no metrics; prober probes immediately (D-004..D-006).

## Left red
None.
