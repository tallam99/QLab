-- Static queries for the QLab store, compiled to type-safe Go by sqlc (see
-- sqlc.yaml; regenerate with `mage genSqlc`). Transaction control — the per-event
-- SET LOCAL app.current_lab_id and the pool advisory lock — stays hand-written in
-- pool.go, since it is plumbing rather than data access.

-- name: CountLabs :one
SELECT count(*) FROM labs;

-- name: IsMember :one
SELECT EXISTS (
    SELECT 1 FROM labs_users WHERE labs_id = $1 AND users_id = $2
);

-- name: UserByFirebaseUID :one
SELECT users_id, firebase_uid, email, first_name, last_name
FROM users WHERE firebase_uid = @firebase_uid::text;

-- name: UserByEmail :one
SELECT users_id, firebase_uid, email, first_name, last_name
FROM users WHERE email = $1;

-- name: LinkFirebaseUID :one
-- First-login provisioning: bind a Firebase uid to an existing user row and fill
-- any name parts the invite left blank (keep an existing name if the provider sent
-- none). The user is recorded as their own updater.
UPDATE users SET
    firebase_uid = @firebase_uid::text,
    first_name   = COALESCE(NULLIF(@first_name::text, ''), first_name),
    last_name    = COALESCE(NULLIF(@last_name::text, ''), last_name),
    updated_by   = @actor,
    updated_at   = now()
WHERE users_id = @users_id
RETURNING users_id, firebase_uid, email, first_name, last_name;

-- name: ResourcePoolByID :one
SELECT resource_pools_id, labs_id, kind, name
FROM resource_pools
WHERE labs_id = $1 AND resource_pools_id = $2;

-- name: ListResources :many
SELECT resources_id, resource_pools_id, labs_id, kind, name
FROM resources
WHERE labs_id = $1 AND resource_pools_id = $2
ORDER BY resources_id;

-- name: SlotByID :one
SELECT slots_id, labs_id, users_id, resource_pools_id, resources_id,
       slot_priority, status, desired_start, lookahead, duration,
       committed_start, actual_start, note
FROM slots
WHERE labs_id = $1 AND slots_id = $2;

-- name: ListSlots :many
SELECT slots_id, labs_id, users_id, resource_pools_id, resources_id,
       slot_priority, status, desired_start, lookahead, duration,
       committed_start, actual_start, note
FROM slots
WHERE labs_id = $1 AND resource_pools_id = $2
ORDER BY desired_start, slots_id;

-- name: ListLiveSlotsForUpdate :many
SELECT slots_id, labs_id, users_id, resource_pools_id, resources_id,
       slot_priority, status, desired_start, lookahead, duration,
       committed_start, actual_start, note
FROM slots
WHERE labs_id = $1 AND resource_pools_id = $2 AND status IN ('SCHEDULED', 'ACTIVE')
ORDER BY slot_priority
FOR UPDATE;

-- name: UpsertSlot :exec
INSERT INTO slots (
    slots_id, labs_id, users_id, resource_pools_id, resources_id,
    slot_priority, desired_start, lookahead, duration,
    committed_start, actual_start, status, note, created_by, updated_by
) VALUES (
    @slots_id, @labs_id, @users_id, @resource_pools_id, @resources_id,
    @slot_priority, @desired_start, @lookahead, @duration,
    @committed_start, @actual_start, @status::slot_status, @note, @actor, @actor
)
ON CONFLICT (slots_id) DO UPDATE SET
    resources_id      = EXCLUDED.resources_id,
    resource_pools_id = EXCLUDED.resource_pools_id,
    slot_priority     = EXCLUDED.slot_priority,
    desired_start     = EXCLUDED.desired_start,
    lookahead         = EXCLUDED.lookahead,
    duration          = EXCLUDED.duration,
    committed_start   = EXCLUDED.committed_start,
    actual_start      = EXCLUDED.actual_start,
    status            = EXCLUDED.status,
    note              = EXCLUDED.note,
    updated_by        = EXCLUDED.updated_by;

-- name: InsertOutbox :exec
INSERT INTO outbox (labs_id, dedup_key, event_type, payload, recipient_user_id, created_by, updated_by)
VALUES (@labs_id, @dedup_key, @event_type, @payload::jsonb, @recipient_user_id, @actor, @actor)
ON CONFLICT (dedup_key) DO NOTHING;

-- Operator tooling (staging/local only; runs on an elevated, RLS-bypassing
-- connection). These are cross-tenant admin queries — see store.OperatorStore.

-- name: CreateLab :one
INSERT INTO labs (labs_id, name, created_by, updated_by)
VALUES (@labs_id, @name, @actor, @actor)
RETURNING labs_id, name;

-- name: CreateUserWithEmail :one
INSERT INTO users (users_id, email, first_name, last_name, created_by, updated_by)
VALUES (@users_id, @email, @first_name, @last_name, @actor, @actor)
RETURNING users_id, firebase_uid, email, first_name, last_name;

-- name: CreateMembership :exec
INSERT INTO labs_users (labs_id, users_id, role, created_by, updated_by)
VALUES (@labs_id, @users_id, @role::lab_role, @actor, @actor);

-- name: CreateResourcePool :one
INSERT INTO resource_pools (resource_pools_id, labs_id, kind, name, created_by, updated_by)
VALUES (@resource_pools_id, @labs_id, @kind::resource_kind, @name, @actor, @actor)
RETURNING resource_pools_id, labs_id, kind, name;

-- name: CreateResource :one
INSERT INTO resources (resources_id, resource_pools_id, labs_id, kind, name, created_by, updated_by)
VALUES (@resources_id, @resource_pools_id, @labs_id, @kind::resource_kind, @name, @actor, @actor)
RETURNING resources_id, resource_pools_id, labs_id, kind, name;

-- name: ListLabsWithCounts :many
SELECT l.labs_id, l.name,
       (SELECT count(*) FROM labs_users lu WHERE lu.labs_id = l.labs_id) AS user_count,
       (SELECT count(*) FROM resources r WHERE r.labs_id = l.labs_id) AS resource_count
FROM labs l
WHERE @feature::text = '' OR l.name ILIKE '%' || @feature || '%'
ORDER BY l.name, l.labs_id;

-- name: LabByID :one
SELECT labs_id, name FROM labs WHERE labs_id = $1;

-- name: ListLabMembers :many
SELECT u.users_id, u.firebase_uid, u.email, u.first_name, u.last_name, lu.role
FROM labs_users lu JOIN users u ON u.users_id = lu.users_id
WHERE lu.labs_id = $1
ORDER BY lu.role, u.email;

-- name: ListLabResourcePools :many
SELECT resource_pools_id, labs_id, kind, name FROM resource_pools
WHERE labs_id = $1 ORDER BY name, resource_pools_id;

-- name: ListLabResources :many
SELECT resources_id, resource_pools_id, labs_id, kind, name FROM resources
WHERE labs_id = $1 ORDER BY name, resources_id;

-- name: ListLabSlots :many
SELECT slots_id, labs_id, users_id, resource_pools_id, resources_id,
       slot_priority, status, desired_start, lookahead, duration,
       committed_start, actual_start, note
FROM slots WHERE labs_id = $1
ORDER BY resource_pools_id, slot_priority, slots_id;

-- name: UserByID :one
SELECT users_id, firebase_uid, email, first_name, last_name FROM users WHERE users_id = $1;

-- name: DeleteLab :execrows
DELETE FROM labs WHERE labs_id = $1;
