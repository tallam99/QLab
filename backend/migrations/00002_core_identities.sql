-- Tenancy and identity: labs, users, and the membership join that ties a user to
-- a lab with a role. Every tenant-scoped row elsewhere carries lab_id and scopes
-- to one of these labs.

-- +goose Up

CREATE TABLE labs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL CHECK (length(trim(name)) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Firebase's stable external identity. NULL until the invited user first logs
    -- in (invite-only provisioning, Phase 8); UNIQUE so one Firebase identity maps
    -- to at most one user. Postgres allows multiple NULLs under UNIQUE.
    firebase_uid text UNIQUE,
    -- Canonical lowercase email. The lowercase CHECK makes "same email, different
    -- case" impossible so the UNIQUE actually means one human = one row.
    email        text NOT NULL UNIQUE
                 CHECK (email = lower(email) AND position('@' IN email) > 1),
    display_name text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE lab_memberships (
    lab_id     uuid NOT NULL REFERENCES labs(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       lab_role NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- One membership row per (lab, user); the leading lab_id also serves
    -- lab-scoped membership listing.
    PRIMARY KEY (lab_id, user_id)
);

-- Reverse lookup: "which labs is this user in" (e.g. resolving a login to its
-- memberships). The PK already covers the (lab_id, …) direction.
CREATE INDEX lab_memberships_user ON lab_memberships (user_id);

-- +goose Down

DROP TABLE IF EXISTS lab_memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS labs;
