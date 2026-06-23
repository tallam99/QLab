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
