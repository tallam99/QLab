-- Triggers enforcing rules that CHECK/FK constraints can't express.
--
--   * set_updated_at        — keep updated_at honest on every row mutation.
--   * slots_enforce_active_pin — encode "ACTIVE is untouched" (invariant #5) at
--     the DB: once a slot is ACTIVE its resource and start are immutable and it may
--     only move on to COMPLETE/CANCELLED. The engine already guarantees this; the
--     trigger is the belt-and-suspenders so a stray UPDATE can't corrupt a running
--     slot.

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER labs_set_updated_at
    BEFORE UPDATE ON labs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER lab_memberships_set_updated_at
    BEFORE UPDATE ON lab_memberships
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER resource_pools_set_updated_at
    BEFORE UPDATE ON resource_pools
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER resources_set_updated_at
    BEFORE UPDATE ON resources
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER slots_set_updated_at
    BEFORE UPDATE ON slots
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER outbox_set_updated_at
    BEFORE UPDATE ON outbox
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
CREATE FUNCTION slots_enforce_active_pin() RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'ACTIVE' THEN
        IF NEW.assigned_resource_id IS DISTINCT FROM OLD.assigned_resource_id THEN
            RAISE EXCEPTION 'cannot reassign an ACTIVE slot (id=%)', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.actual_start IS DISTINCT FROM OLD.actual_start THEN
            RAISE EXCEPTION 'cannot move an ACTIVE slot''s start (id=%)', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        -- An ACTIVE slot settles to COMPLETE or CANCELLED only; it never returns to
        -- SCHEDULED and is never NO_SHOW (no-show applies to slots that never
        -- clocked in, §2.3).
        IF NEW.status NOT IN ('ACTIVE', 'COMPLETE', 'CANCELLED') THEN
            RAISE EXCEPTION 'invalid status transition ACTIVE -> % (slot id=%)',
                NEW.status, OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER slots_enforce_active_pin
    BEFORE UPDATE ON slots
    FOR EACH ROW EXECUTE FUNCTION slots_enforce_active_pin();

-- +goose Down

DROP TRIGGER IF EXISTS slots_enforce_active_pin ON slots;
DROP FUNCTION IF EXISTS slots_enforce_active_pin();
DROP TRIGGER IF EXISTS outbox_set_updated_at ON outbox;
DROP TRIGGER IF EXISTS slots_set_updated_at ON slots;
DROP TRIGGER IF EXISTS resources_set_updated_at ON resources;
DROP TRIGGER IF EXISTS resource_pools_set_updated_at ON resource_pools;
DROP TRIGGER IF EXISTS lab_memberships_set_updated_at ON lab_memberships;
DROP TRIGGER IF EXISTS users_set_updated_at ON users;
DROP TRIGGER IF EXISTS labs_set_updated_at ON labs;
DROP FUNCTION IF EXISTS set_updated_at();
