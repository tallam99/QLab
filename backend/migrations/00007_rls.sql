-- Row-level security: tenant isolation at the database, as defense-in-depth behind
-- the service's own lab_id scoping (decision 0005). Every lab-scoped table only
-- exposes rows for the lab in the session's `app.current_lab_id` setting, which the
-- service sets per request transaction (Phase 7). It is fail-closed: with the
-- setting unset, current_setting(..., true) is NULL, so `labs_id = NULL` matches no
-- rows and permits no writes.
--
-- This binds only non-owner, non-BYPASSRLS roles — i.e. the app role (qlab_app).
-- The owner (migrations) and superusers bypass it, so migrations and seeding are
-- unaffected. `users` is intentionally NOT covered: a user is not lab-scoped (one
-- person can belong to several labs); the service scopes user reads via membership.

-- +goose Up

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['labs', 'labs_users', 'resource_pools', 'resources', 'slots', 'outbox']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format($p$
            CREATE POLICY %2$I ON %1$I
                USING (labs_id = current_setting('app.current_lab_id', true)::uuid)
                WITH CHECK (labs_id = current_setting('app.current_lab_id', true)::uuid)
        $p$, t, t || '_tenant_isolation');
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['labs', 'labs_users', 'resource_pools', 'resources', 'slots', 'outbox']
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS %2$I ON %1$I', t, t || '_tenant_isolation');
        EXECUTE format('ALTER TABLE %I DISABLE ROW LEVEL SECURITY', t);
    END LOOP;
END;
$$;
-- +goose StatementEnd
