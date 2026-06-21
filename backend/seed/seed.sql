-- Local demo seed (applied by `mage seed`, LOCAL ONLY — see the magefile).
--
-- Builds one lab with a head + two members, a vent-hood pool of two resources, and
-- a small queue mirroring the ALGORITHM test matrix: one ACTIVE slot pinned to a
-- resource, plus SCHEDULED slots with varying lookahead left unplaced for the
-- engine to position (Phase 7). IDs are fixed so the schema tests can assert exact
-- values. Re-runnable: it truncates the demo tables first.
--
-- This is demo data, NOT reference data. Anything that must exist in staging/prod
-- belongs in a migration, never here.

BEGIN;

TRUNCATE labs, users, lab_memberships, resource_pools, resources, slots, outbox
    RESTART IDENTITY CASCADE;

INSERT INTO labs (id, name) VALUES
    ('10000000-0000-0000-0000-000000000001', 'Demo Lab');

INSERT INTO users (id, firebase_uid, email, display_name) VALUES
    ('20000000-0000-0000-0000-000000000001', 'demo-head',    'demo-head@example.com',    'Dana Head'),
    ('20000000-0000-0000-0000-000000000002', 'demo-member1', 'demo-member1@example.com', 'Mara Member'),
    ('20000000-0000-0000-0000-000000000003', 'demo-member2', 'demo-member2@example.com', 'Milo Member');

INSERT INTO lab_memberships (lab_id, user_id, role) VALUES
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'HEAD'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'MEMBER'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', 'MEMBER');

INSERT INTO resource_pools (id, lab_id, kind, name) VALUES
    ('30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hoods');

INSERT INTO resources (id, resource_pool_id, lab_id, kind, name) VALUES
    ('40000000-0000-0000-0000-000000000001',
     '30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hood A'),
    ('40000000-0000-0000-0000-000000000002',
     '30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hood B');

-- Queue. Priority 1 is ACTIVE (clocked in, pinned to Vent Hood A). The rest are
-- SCHEDULED and unplaced (assigned_resource_id / actual_start NULL) — the engine
-- places them on the next reschedule.
INSERT INTO slots (
    id, lab_id, user_id, resource_pool_id, assigned_resource_id,
    slot_priority, desired_start, lookahead, duration,
    committed_start, actual_start, status, note
) VALUES
    ('50000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000002',
     '30000000-0000-0000-0000-000000000001',
     '40000000-0000-0000-0000-000000000001',
     1, '2026-06-21 09:00:00+00', 0, 60,
     '2026-06-21 09:00:00+00', '2026-06-21 09:00:00+00', 'ACTIVE', 'running now'),

    ('50000000-0000-0000-0000-000000000002',
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000003',
     '30000000-0000-0000-0000-000000000001',
     NULL,
     2, '2026-06-21 10:30:00+00', 30, 60,
     NULL, NULL, 'SCHEDULED', 'flexible: may pull 30m earlier'),

    ('50000000-0000-0000-0000-000000000003',
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000001',
     '30000000-0000-0000-0000-000000000001',
     NULL,
     3, '2026-06-21 11:30:00+00', 0, 30,
     NULL, NULL, 'SCHEDULED', 'no earliness'),

    ('50000000-0000-0000-0000-000000000004',
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000002',
     '30000000-0000-0000-0000-000000000001',
     NULL,
     4, '2026-06-21 12:00:00+00', 15, 45,
     NULL, NULL, 'SCHEDULED', 'short slot, may gap-fill');

COMMIT;
