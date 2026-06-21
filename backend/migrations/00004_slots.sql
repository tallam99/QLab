-- Slots: the priority queue. One row per booking. Mirrors dynamicqueue.Slot
-- (ALGORITHM §1.1); the engine's pure domain type is the shape this is mapped to
-- and from at the edges (Phase 7). Times are timestamptz; lookahead/duration are
-- integer minutes.

-- +goose Up

CREATE TABLE slots (
    slots_id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    labs_id           uuid NOT NULL,
    -- The booker.
    users_id          uuid NOT NULL,
    resource_pools_id uuid NOT NULL,
    -- The specific resource the engine assigned. NULL until clock-in — provisional
    -- placement, fixed once ACTIVE (§1.1, §1.4).
    resources_id      uuid,

    -- Position in line: lower runs ahead. A unique total order across the pool's
    -- live slots (enforced by the partial unique index below) and the sole
    -- processing/tie-break key (§1.3, §4). bigint leaves room to insert between;
    -- the range itself is effectively inexhaustible.
    slot_priority bigint NOT NULL,

    -- Booked start and earliness flexibility. Earliest allowed start is
    -- desired_start − lookahead (§2). Only lookahead >= 0 is constrained: durations
    -- are inviolable and unrelated to lookahead, so NO lookahead <= duration check
    -- (ALGORITHM §2, PLAN Phase 5 note).
    desired_start timestamptz NOT NULL,
    lookahead     integer NOT NULL CHECK (lookahead >= 0),
    duration      integer NOT NULL CHECK (duration > 0),

    -- The start the user was last notified of (re-commit / no-show reference, §2.2,
    -- §2.3). NULL until first committed.
    committed_start timestamptz,
    -- Where the current schedule places this slot (engine output). NULL until
    -- placed; always set for an ACTIVE slot (see the active-pinned CHECK).
    actual_start    timestamptz,

    status slot_status NOT NULL DEFAULT 'SCHEDULED',
    note   text NOT NULL DEFAULT '',

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES users(users_id) ON DELETE SET NULL,
    updated_by uuid REFERENCES users(users_id) ON DELETE SET NULL,

    -- The slot's pool belongs to the slot's lab (cross-lab booking impossible).
    FOREIGN KEY (resource_pools_id, labs_id)
        REFERENCES resource_pools (resource_pools_id, labs_id) ON DELETE CASCADE,
    -- The booker is a member of the slot's lab (no booking by a non-member).
    FOREIGN KEY (labs_id, users_id)
        REFERENCES labs_users (labs_id, users_id) ON DELETE CASCADE,
    -- When assigned, the resource is one of THIS slot's pool (not another pool's).
    -- Composite-FK MATCH SIMPLE: with resources_id NULL the rule is inactive, so
    -- unassigned slots are unconstrained.
    FOREIGN KEY (resources_id, resource_pools_id)
        REFERENCES resources (resources_id, resource_pools_id),

    -- An ACTIVE slot is pinned: it must have a concrete resource and start time.
    CONSTRAINT slots_active_pinned CHECK (
        status <> 'ACTIVE'
        OR (resources_id IS NOT NULL AND actual_start IS NOT NULL)
    )
);

-- slot_priority is a UNIQUE total order — but only across LIVE slots; settled
-- history (COMPLETE/CANCELLED/NO_SHOW) may reuse values. Partial unique index
-- enforces that and doubles as the queue-order lookup (resource_pools_id, priority).
CREATE UNIQUE INDEX slots_live_priority_uniq
    ON slots (resource_pools_id, slot_priority)
    WHERE status IN ('SCHEDULED', 'ACTIVE');

-- Per-resource timeline lookups (PLAN Phase 5).
CREATE INDEX slots_resource_timeline
    ON slots (resources_id, actual_start)
    WHERE resources_id IS NOT NULL;

-- Tenant scoping.
CREATE INDEX slots_lab ON slots (labs_id);

-- Occupancy as a half-open range [start, start + duration). Wrapped in an
-- IMMUTABLE function because the index/exclusion below needs immutability, yet the
-- `timestamptz + interval` operator is only marked STABLE (intervals may carry
-- month/day components whose meaning depends on the timezone). Our intervals are
-- minutes only — pure UTC addition, genuinely deterministic — so asserting
-- IMMUTABLE here is correct.
-- +goose StatementBegin
CREATE FUNCTION slot_occupancy(start_at timestamptz, duration_mins integer)
    RETURNS tstzrange
    LANGUAGE sql IMMUTABLE
AS $$
    SELECT tstzrange(start_at, start_at + make_interval(mins => duration_mins));
$$;
-- +goose StatementEnd

-- Per-resource no-overlap (invariant #1): on one resource, two live slots'
-- half-open [actual_start, actual_start + duration) intervals may not overlap.
-- Different resources may overlap freely (fan-out, §1.4), hence the equality on
-- resources_id. DEFERRABLE INITIALLY DEFERRED so Phase 7's reschedule can rewrite
-- many rows in one transaction through transient overlaps and be checked only at
-- COMMIT. Settled/unassigned rows are excluded via the WHERE predicate. At QLab's
-- scale (a handful of resources, a short per-pool queue) the GiST check is
-- negligible; revisit only if a single pool's queue ever grows large.
ALTER TABLE slots ADD CONSTRAINT slots_no_resource_overlap
    EXCLUDE USING gist (
        resources_id WITH =,
        slot_occupancy(actual_start, duration) WITH &&
    )
    WHERE (
        status IN ('SCHEDULED', 'ACTIVE')
        AND resources_id IS NOT NULL
        AND actual_start IS NOT NULL
    )
    DEFERRABLE INITIALLY DEFERRED;

-- +goose Down

DROP TABLE IF EXISTS slots;
DROP FUNCTION IF EXISTS slot_occupancy(timestamptz, integer);
