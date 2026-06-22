-- Tenancy and identity: users, labs, and the labs_users join that ties a user to
-- a lab with a role. Every tenant-scoped row elsewhere carries labs_id.
--
-- The audit columns created_by / updated_by (on every table) are plain uuid, NOT
-- foreign keys: they hold the authenticated principal that wrote the row — usually
-- a users_id, but also system/automation actors that aren't users, and they must
-- outlive a user's deletion. They're application-set (NULL for system/unattributed
-- or seed data). See migrations/README → Conventions.

-- +goose Up

CREATE TABLE users (
    users_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Firebase's stable external identity. NULL until the invited user first logs
    -- in (invite-only provisioning, Phase 8); UNIQUE so one Firebase identity maps
    -- to at most one user. Postgres allows multiple NULLs under UNIQUE.
    firebase_uid text UNIQUE,
    -- Canonical lowercase email. The lowercase CHECK makes "same email, different
    -- case" impossible so the UNIQUE actually means one human = one row.
    email      text NOT NULL UNIQUE
               CHECK (email = lower(email) AND position('@' IN email) > 1),
    -- Name parts are filled from the identity provider on first login; they may be
    -- empty between invite and first login, hence NOT NULL DEFAULT '' rather than
    -- required. dob is optional PII (never supplied by Google sign-in).
    first_name text NOT NULL DEFAULT '',
    last_name  text NOT NULL DEFAULT '',
    dob        date,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid,
    updated_by uuid
);

CREATE TABLE labs (
    labs_id    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL CHECK (length(trim(name)) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid,
    updated_by uuid
);

CREATE TABLE labs_users (
    labs_id    uuid NOT NULL REFERENCES labs(labs_id) ON DELETE CASCADE,
    users_id   uuid NOT NULL REFERENCES users(users_id) ON DELETE CASCADE,
    role       lab_role NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid,
    updated_by uuid,
    -- One membership row per (lab, user); the leading labs_id also serves
    -- lab-scoped membership listing.
    PRIMARY KEY (labs_id, users_id)
);

-- Reverse lookup: "which labs is this user in" (e.g. resolving a login to its
-- memberships). The PK already covers the (labs_id, …) direction.
CREATE INDEX labs_users_user ON labs_users (users_id);

-- +goose Down

DROP TABLE IF EXISTS labs_users;
DROP TABLE IF EXISTS labs;
DROP TABLE IF EXISTS users;
