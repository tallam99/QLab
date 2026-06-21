-- Transactional outbox for notifications. Designed now (PLAN Phase 5 / the
-- notifications convention) so it isn't bolted on later; the delivery worker,
-- channels, retry/backoff, and dead-lettering land in Phase 11. A notification is
-- written here in the SAME transaction as the event that triggers it, then
-- delivered asynchronously, so a failed send never fails the API request.

-- +goose Up

CREATE TABLE outbox (
    id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lab_id uuid NOT NULL REFERENCES labs(id) ON DELETE CASCADE,

    -- Idempotency: retries reuse the same dedup_key so a message is never
    -- double-sent. UNIQUE makes a duplicate enqueue a no-op at the DB level.
    dedup_key text NOT NULL UNIQUE,

    -- The triggering event (e.g. slot re-committed, resource freed, no-show) and
    -- its JSON-encoded protobuf payload. event_type stays text rather than an enum:
    -- the set of notification events grows in Phase 11 and the payload is already
    -- an open envelope, so a closed enum here would buy little.
    event_type text  NOT NULL CHECK (length(trim(event_type)) > 0),
    payload    jsonb NOT NULL,

    status   outbox_status NOT NULL DEFAULT 'PENDING',
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    -- Earliest the worker may (re)attempt delivery — drives exponential backoff.
    available_at timestamptz NOT NULL DEFAULT now(),

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- The worker's drain query: pending messages whose backoff has elapsed, oldest
-- first. Partial on PENDING so SENT/DEAD rows don't bloat the index.
CREATE INDEX outbox_pending_due
    ON outbox (available_at)
    WHERE status = 'PENDING';

CREATE INDEX outbox_lab ON outbox (lab_id);

-- +goose Down

DROP TABLE IF EXISTS outbox;
