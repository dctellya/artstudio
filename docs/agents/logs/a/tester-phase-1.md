# A tester, phase 1

## 2026-10-14T16:55:35Z prompt received (verbatim, abbreviated only by the spec path)

You are the TESTER of Team A (vendor client + cache + lookup), phase 1, per docs/TEAM-PROTOCOL.md. Worktree: /Users/dct/code/EllyaOrg/projects/dalimaray/artstudio-wt/a (branch team-a/phase-1). Work only there. Your spec: .../scratchpad/briefs/team-a.md, plus internal/ports/*.go and internal/config/config.go in your worktree. Budget: ~10 minutes.

Rules: Never amend, reset, rebase or stash; commit forward only; never push; never touch main or another worktree. No production code, build logic or configs beyond test dependencies; testify already in go.mod. Write exactly the tests SPEC §6 lists for Team A: rows "artic unit", "cache unit", "lookup unit", "artic integration". Integration test under build tag integration using github.com/dctellya/artstudio/internal/artic/stubserver (Team D; will not compile until D merges). Signatures to use exactly: artic.New(cfg, reg, opts ...artic.Option) (*artic.Client, error); artic.NewProber(v, cfg, reg) *artic.Prober with Run(ctx), CheckOnce(ctx), ports.Checker "vendor"; cache.New(cfg, reg, opts ...cache.Option) *cache.LRU, cache.WithClock, SearchKey, ArtworkKey, NormalizeQuery; lookup.New(v, c, a, cfg, reg) *lookup.Service. httptest servers, small durations, suite under 20s, fresh registry per test. Commit as test(a-1): ... with trailer. Run go test -race ./internal/artic/... ./internal/cache/... ./internal/lookup/... and paste red output. End with TEST BRIEF. Log to docs/agents/logs/a/tester-phase-1.md with date -u timestamps.

## 2026-10-14T16:55:35Z action: read brief, ports, config, TEAM-PROTOCOL; worktree clean on team-a/phase-1; no internal/artic, cache, lookup yet.

## 2026-10-14T16:57:11Z action: wrote internal/artic/helpers_test.go (config + httptest env + metric helper).

## 2026-10-14T16:58:50Z action: wrote internal/artic/client_test.go, prober_test.go, integration_test.go (tag integration), internal/cache/cache_test.go, internal/lookup/lookup_test.go; gofmt clean.

## 2026-10-14T16:59:12Z action: go test -race ./internal/artic/... ./internal/cache/... ./internal/lookup/...
Result: red, build failed in all three packages ("no non-test Go files" because packages do not exist yet). Type-checked with a scratch overlay of empty package files: only "undefined: artic.New/Client/Option/WithRand/NewProber, cache.New/LRU/WithClock/SearchKey/ArtworkKey/NormalizeQuery, lookup.New/Service" errors. go vet -tags integration: stubserver package missing (expected until Team D merges).

TEST BRIEF: see the final message of the tester (also appended below).

## 2026-10-14T16:59:43Z final output
### TEST BRIEF (Team A, phase 1)

Commands: `go test -race ./internal/artic/... ./internal/cache/... ./internal/lookup/...`; integration: `go test -race -tags integration ./internal/artic/...` (needs Team D stubserver).

**artic unit** (internal/artic/client_test.go, prober_test.go, helpers_test.go; httptest vendor, attempt/budget in ms)
- TestClientImplementsVendor: *Client satisfies ports.Vendor.
- TestAttemptTimeout: hanging vendor, attempt 50ms, 0 retries -> error (not ErrBudgetExceeded), <500ms, Attempts=1.
- TestBudgetExceeded: attempt 100ms, budget 250ms, 5 retries, hanging vendor -> ErrBudgetExceeded in <700ms.
- TestRetryPolicy: 429/502/503/504 -> 3 hits (1+2 retries); 500, 403, undecodable 200 body -> 1 hit; dropped connection -> 3 hits; final errors: 429 -> ErrUpstreamRateLimited, other statuses/undecodable -> ErrBadUpstream; call.Attempts matches.
- TestRetrySucceedsAfterTransientFailure: 503 then 200 -> ok, Attempts=2, upstream_retries_total{operation=artwork}=1.
- TestBackoffFullJitterCapped: rand=0 -> total <60ms; rand=0.999 with base 40ms/max 60ms -> 90..180ms (sleep = rand()*min(max, base*2^n)); exactly one rand() draw per retry.
- TestRetryAfterHonouredWhenItFitsBudget: 429 Retry-After:1 then 200 -> waits >=900ms, Attempts=2.
- TestRetryAfterExceedingBudgetStopsAndIsReported: Retry-After:120, budget 300ms -> returns <250ms with 1 hit, ErrUpstreamRateLimited, errors.As ports.RetryAfterError gives 120s.
- TestRedirectNotFollowed: 302 -> ErrBadUpstream, target never hit, Attempts=1, call.Status=302.
- TestBreakerOpensAfterConsecutiveFailures: 5 x 503 (0 retries, 1 call = 1 breaker sample); state "closed" before, "open" after; 6th call ErrBreakerOpen with no vendor hit, call.Breaker="open"; breaker_transitions_total{from=closed,to=open}=1; breaker_state gauge=2.
- TestBreakerOpensOnFailureRatio: consecutive limit 1000, ratio 0.5, min 20: 8 ok then failures; still closed after 19 requests, open after the 20th (12/20), then ErrBreakerOpen.
- TestBreakerHalfOpenAdmitsThreeTrials: open timeout 100ms, half-open max 3: 3 concurrent trials admitted, 4th -> ErrBreakerOpen, after 3 successes state "closed".
- TestLimiterRejectsWhenWaitExceedsMax: rate 5/s burst 1 maxwait 100ms: 2nd call ErrLimiterRejected in <150ms, no vendor hit, upstream_limiter_rejected_total=1.
- TestLimiterWaitsWithinMax: rate 20/s burst 1 maxwait 500ms: 2nd call succeeds after >=30ms.
- TestBulkheadRejects11thConcurrentCall: bulkhead 10; 10 blocked calls, upstream_bulkhead_in_use=10, 11th -> ErrBulkheadFull, upstream_bulkhead_rejected_total=1, never hits vendor, gauge back to 0 after release.
- TestNotFound: 404 -> ErrNotFound, call.Status=404, Operation="artwork", never retried, 6 x 404 leave breaker "closed".
- TestContactHeaderSent: header `AIC-User-Agent: artstudio (ops@example.com)`.
- TestSearchRequestAndImageURL: GET /api/v1/artworks/search with q, limit, page query params; decodes pagination (total, limit, current_page->Page), items; Call.Operation="search", Status 200, Body non-empty; ImageURL = `<config.iiif_url>/<image_id>/full/843,/0/default.jpg`; Artist from `artist_title`, Date from `date_display`, ImageID from `image_id`.
- TestArtworkDecoded: GET /api/v1/artworks/{id}, `{"data":{...},"config":{"iiif_url":...}}` decoded.
- TestProberUpdatesState: *Prober is ports.Checker, Name "vendor"; Detail state "up"/"down", breaker (from Vendor.BreakerState()), latency_ms, last_check, last_error (string containing the probe error); OK always true; upstream_probe_up 1/0.
- TestProberRunLoopsUntilCancelled: Run probes every ProbeInterval (>=3 probes in 2s) and returns on ctx cancel.

**cache unit** (internal/cache/cache_test.go; fake clock via WithClock; CacheMaxEntries 3 for eviction)
- TestImplementsPorts: *LRU satisfies ports.Cache.
- TestLRUBoundAndEviction: Get touches recency; 4th Set evicts least recently used; Len()==3; cache_evictions_total=1, cache_entries=3.
- TestTTLHitThenStale: Get returns (v, hit) before TTL, (v, stale) after TTL with the value still returned.
- TestStaleGoneAfterWindow: stale at TTL+59s, (nil, miss) at 71s after Set, Len()==0 (stale window 60s).
- TestNegativeEntryExpiresAfterNegativeTTL: SetNegative -> (nil, negative) until CacheTTLNegative, then miss (never stale).
- TestSingleflightCollapsesConcurrentCalls: 20 concurrent Do on one key -> fn called once, all get the value, 19 report shared=true, cache_singleflight_dedupes_total=19.
- TestSingleflightPropagatesError: fn error returned; a later Do runs a new fn, shared=false.
- TestKeyNormalisation: NormalizeQuery("  Monet   WATER\tLilies \n")="monet water lilies"; SearchKey(Q " Monet ", 10, 2)="search:v1:monet:10:2"; equivalent queries share a key, different pages do not; ArtworkKey(27992)="artwork:v1:27992".

**lookup unit** (internal/lookup/lookup_test.go; fake Vendor, fake Auditor, real cache.LRU with fake clock; TTL search 10s, artwork 20s, stale 60s, negative 5s)
- TestServiceImplementsLookup.
- TestSearchMissThenHit: miss then hit; Meta.RequestID from ctx, Attempts and UpstreamLatency copied from Call; stored under cache.SearchKey; hit makes no vendor call.
- TestArtworkMissThenHit: artwork TTL (20s) used, hit at +15s; stored under cache.ArtworkKey.
- TestExpiredEntryWithHealthyVendorIsRefreshed: expired + vendor ok -> miss with fresh data.
- TestStaleServedOnVendorError: expired + vendor error -> nil error, Meta.Cache "stale", Attempts from the failed call; failed vendor call audited (2 records, Error non-empty, UpstreamStatus 503).
- TestStaleServedOnBreakerOpen: expired + ErrBreakerOpen -> stale.
- TestErrorWhenNothingStale: cold cache or beyond stale window: errors.Is(err, vendor error) for ErrBreakerOpen, ErrBadUpstream, ErrBudgetExceeded (wrapping allowed).
- TestNegativeCaching: vendor ErrNotFound -> ErrNotFound; 2nd call ErrNotFound with Meta.Cache "negative", no vendor call, no audit record; after 6s vendor asked again.
- TestOneAuditRecordPerVendorCallNoneOnHit: miss -> exactly one VendorCall {Operation, RequestID, CacheKey=cache.SearchKey/ArtworkKey, CacheStatus=miss, UpstreamStatus, Attempts, LatencyMS=30, Breaker, Error="", TS set, Response=Call.Body}; hit adds none.

**artic integration** (internal/artic/integration_test.go, tag `integration`)
- TestVendorFaultRetriesBreakerOpenThenStale: stubserver.New() via httptest; client + cache + lookup wired; search miss then hit; `POST /__ctl?fault=503` -> vendor.Search gives ErrBadUpstream with 3 attempts/status 503; repeated failures open the breaker within 5 more calls; next call ErrBreakerOpen; after cache TTL expiry lookup.Search serves stale (equal to first result); `fault=clear` -> breaker recovers (open timeout 200ms) and state "closed". Needs stub: search for any query returns >=1 item.

### Required production API
- package artic: `type Client`, `type Option func(*...)`, `func New(cfg config.Config, reg prometheus.Registerer, opts ...Option) (*Client, error)`, `func WithRand(func() float64) Option` (optional `WithHTTPClient(*http.Client)` unused by tests); Client methods Search, Artwork, Probe, BreakerState() returning "closed" | "half-open" | "open"; `type Prober`, `func NewProber(v ports.Vendor, cfg config.Config, reg prometheus.Registerer) *Prober`, methods `Run(ctx)`, `CheckOnce(ctx)`, `Name() string` ("vendor"), `Check(ctx) ports.CheckResult`.
- Vendor URLs: `<UpstreamBaseURL>/artworks/search?q&limit&page[&fields]`, `<base>/artworks/<id>[?fields]`, probe `<base>/artworks?limit=1&fields=id`. Header `AIC-User-Agent: artstudio (<ContactEmail>)`. Call.Operation values "search", "artwork", "probe". One breaker sample per Client call (not per attempt); 404 is not a failure; limiter/bulkhead rejections not breaker failures.
- Config fields used: UpstreamAttemptTimeout, UpstreamBudget, UpstreamMaxRetries, UpstreamBackoffBase/Max, UpstreamRate/Burst, UpstreamLimiterMaxWait, UpstreamBulkhead, BreakerConsecutive/Ratio/MinRequests/OpenTimeout/HalfOpenMax, ProbeInterval, CacheMaxEntries, CacheTTLSearch/Artwork/Negative/Stale.
- Metrics (namespace artstudio): upstream_retries_total{operation,reason}, upstream_breaker_transitions_total{from,to}, upstream_breaker_state, upstream_limiter_rejected_total, upstream_bulkhead_in_use, upstream_bulkhead_rejected_total, upstream_probe_up, cache_evictions_total, cache_entries, cache_singleflight_dedupes_total.
- package cache: `type LRU`, `type Option`, `New(cfg, reg, opts ...Option) *LRU`, `WithClock(func() time.Time) Option`, `SearchKey(ports.SearchQuery) string`, `ArtworkKey(int) string`, `NormalizeQuery(string) string`. Get: hit (fresh), stale (expired within TTL+CacheTTLStale of Set, value returned), miss (nil), negative (nil, for CacheTTLNegative then miss). Do returns (value, shared, err); shared=false for leader.
- package lookup: `type Service`, `New(v ports.Vendor, c ports.Cache, a ports.Auditor, cfg config.Config, reg prometheus.Registerer) *Service`. Stores JSON-ish bytes under cache.SearchKey/ArtworkKey with CacheTTLSearch/CacheTTLArtwork; one Auditor.Record per vendor call (including failed ones), none for hit/negative/stale-without-call; vendor error with stale entry -> stale + nil error, else vendor error unchanged (errors.Is).
