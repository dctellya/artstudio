# D-006 Stale window and audit scope

- **What:** an entry is fresh for its TTL and stale for a further `CACHE_TTL_STALE`; stale is served only when the vendor fails or the breaker is open, with `X-Cache: stale`. Negative entries never go stale. Every vendor call is audited, failures and local rejections included; cache hits are not.
- **Why:** an artwork record an hour old is still correct; the audit answers "what did the vendor do", including when we refused to ask it.
- **Alternatives:** stale-while-revalidate on every expiry; audit only successful calls.
- **Consequences:** audit rows include breaker-open refusals (B-022 may split them out).
