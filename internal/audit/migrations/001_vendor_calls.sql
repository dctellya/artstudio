-- Append-only audit of every vendor call: the code only ever inserts.
create table if not exists vendor_calls (
    id             bigserial primary key,
    ts             timestamptz not null,
    request_id     text,
    trace_id       text,
    operation      text,
    cache_key      text,
    cache_status   text,
    upstream_status int,
    attempts       int,
    latency_ms     int,
    breaker_state  text,
    error          text,
    response       jsonb,
    response_bytes int
);
create index if not exists vendor_calls_ts_idx on vendor_calls (ts);
create index if not exists vendor_calls_request_id_idx on vendor_calls (request_id);
