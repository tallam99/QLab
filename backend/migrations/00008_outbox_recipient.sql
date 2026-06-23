-- Add the notification recipient to the outbox. The actor who triggered the row
-- is already captured by created_by (the audit column); recipient_user_id is who
-- the notification is *for* (the slot's owner for a re-commit, the occupant for a
-- poke), so the Phase 11 delivery worker can route without parsing the payload.
-- Nullable and not a foreign key, consistent with the audit-actor columns: it
-- records a principal and must survive that user's deletion.

-- +goose Up

ALTER TABLE outbox ADD COLUMN recipient_user_id uuid;

-- +goose Down

ALTER TABLE outbox DROP COLUMN recipient_user_id;
