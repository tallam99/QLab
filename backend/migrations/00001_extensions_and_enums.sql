-- Extensions and enum types underpinning the schema.
--
-- Native PG enums (not text+CHECK) give the strongest, self-documenting
-- enforcement; their labels match the Go `enumer` string form (snake-upper), so
-- the domain layer (Phase 7) maps straight across. btree_gist lets the slots
-- no-overlap exclusion constraint combine equality (assigned_resource_id) with a
-- range overlap test in one GiST index (see 00004_slots.sql).

-- +goose Up

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- A lab member's role. HEAD performs admin actions (inviting members); MEMBER is
-- a regular user. Maps to the auth role enum that lands in Phase 8.
CREATE TYPE lab_role AS ENUM ('HEAD', 'MEMBER');

-- What a resource is. The engine is kind-agnostic, but a pool groups
-- interchangeable resources of ONE kind. MVP ships exactly one (VENT_HOOD);
-- matches dynamicqueue.ResourceKind (§1.4).
CREATE TYPE resource_kind AS ENUM ('VENT_HOOD');

-- A slot's lifecycle. Broader than the engine's input statuses on purpose: the
-- engine sees only SCHEDULED + ACTIVE (history filtered out, NO_SHOW is an
-- output), while the persisted row tracks the full lifecycle (ALGORITHM §1.2).
CREATE TYPE slot_status AS ENUM (
    'SCHEDULED',  -- waiting; the engine places it
    'ACTIVE',     -- clocked in, running now; pinned to a resource
    'COMPLETE',   -- finished (settled history)
    'CANCELLED',  -- cancelled (settled history)
    'NO_SHOW'     -- clock-in grace lapsed; freed (engine output, §2.3)
);

-- Transactional-outbox delivery state (notifications; designed now, drained in
-- Phase 11). PENDING awaits delivery; SENT succeeded; DEAD exhausted retries and
-- is dead-lettered for an admin alert.
CREATE TYPE outbox_status AS ENUM ('PENDING', 'SENT', 'DEAD');

-- +goose Down

DROP TYPE IF EXISTS outbox_status;
DROP TYPE IF EXISTS slot_status;
DROP TYPE IF EXISTS resource_kind;
DROP TYPE IF EXISTS lab_role;
DROP EXTENSION IF EXISTS btree_gist;
