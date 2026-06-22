-- Local demo seed (applied by `mage seed`, LOCAL ONLY — see the magefile).
--
-- Builds one lab with a head + four members (five people), a vent-hood pool of two
-- resources, and a small queue mirroring the ALGORITHM test matrix: one ACTIVE slot
-- pinned to a resource, plus SCHEDULED slots with varying lookahead left unplaced
-- for the engine to position (Phase 7). IDs are fixed so the schema tests can
-- assert exact values. created_by/updated_by are NULL (system-seeded, no actor).
-- Re-runnable: it truncates the demo tables first.
--
-- This is demo data, NOT reference data. Anything that must exist in staging/prod
-- belongs in a migration, never here.

BEGIN;

TRUNCATE labs, users, labs_users, resource_pools, resources, slots, outbox
    RESTART IDENTITY CASCADE;

INSERT INTO users (users_id, firebase_uid, email, first_name, last_name, dob) VALUES
    ('20000000-0000-0000-0000-000000000001', 'demo-head',    'demo-head@example.com',    'Dana', 'Head',   '1980-04-12'),
    ('20000000-0000-0000-0000-000000000002', 'demo-member1', 'demo-member1@example.com', 'Mara', 'Member', '1995-09-30'),
    ('20000000-0000-0000-0000-000000000003', 'demo-member2', 'demo-member2@example.com', 'Milo', 'Member', '1998-01-05'),
    ('20000000-0000-0000-0000-000000000004', 'demo-member3', 'demo-member3@example.com', 'Nora', 'Nguyen', '1992-07-18'),
    ('20000000-0000-0000-0000-000000000005', 'demo-member4', 'demo-member4@example.com', 'Omar', 'Okafor', '2000-11-22');

INSERT INTO labs (labs_id, name) VALUES
    ('10000000-0000-0000-0000-000000000001', 'Demo Lab');

INSERT INTO labs_users (labs_id, users_id, role) VALUES
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'HEAD'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'MEMBER'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', 'MEMBER'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000004', 'MEMBER'),
    ('10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000005', 'MEMBER');

INSERT INTO resource_pools (resource_pools_id, labs_id, kind, name) VALUES
    ('30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hoods');

INSERT INTO resources (resources_id, resource_pools_id, labs_id, kind, name) VALUES
    ('40000000-0000-0000-0000-000000000001',
     '30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hood A'),
    ('40000000-0000-0000-0000-000000000002',
     '30000000-0000-0000-0000-000000000001',
     '10000000-0000-0000-0000-000000000001', 'VENT_HOOD', 'Vent Hood B');

-- Queue. Priority 1 is ACTIVE (clocked in, pinned to Vent Hood A). The rest are
-- SCHEDULED and unplaced (resources_id / actual_start NULL) — the engine places
-- them on the next reschedule.
INSERT INTO slots (
    slots_id, labs_id, users_id, resource_pools_id, resources_id,
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
     '20000000-0000-0000-0000-000000000004',
     '30000000-0000-0000-0000-000000000001',
     NULL,
     4, '2026-06-21 12:00:00+00', 15, 45,
     NULL, NULL, 'SCHEDULED', 'short slot, may gap-fill'),

    ('50000000-0000-0000-0000-000000000005',
     '10000000-0000-0000-0000-000000000001',
     '20000000-0000-0000-0000-000000000005',
     '30000000-0000-0000-0000-000000000001',
     NULL,
     5, '2026-06-21 13:00:00+00', 60, 30,
     NULL, NULL, 'SCHEDULED', 'wide earliness band');

COMMIT;
