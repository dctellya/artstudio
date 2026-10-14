# D-005 The shared vendor call runs on a detached context

- **What:** on a miss the singleflight leader calls the vendor with `context.WithoutCancel(ctx)`; each caller waits with `DoChan` and its own `ctx.Done()`.
- **Why:** with the leader's context, one client disconnecting failed every follower with a 500 (review finding, SLO impact).
- **Alternatives:** per-caller vendor calls (loses dedupe); refcounted cancellation.
- **Consequences:** a vendor call may finish after every caller left; it is bounded by the client's budget and its result still fills the cache.
