-- Equipment as a POOL of interchangeable resources (ALGORITHM §1.4, §10): a pool
-- groups resources of one kind; the engine fans the queue out across them and
-- picks the specific resource near clock-in.
--
-- A lab may have MANY pools of the same kind (e.g. one vent-hood pool for PhDs and
-- another for undergrads) — nothing here restricts that. The extra UNIQUE
-- constraints are only FOREIGN KEY targets: they let downstream tables enforce
-- cross-row domain rules in the DB (a resource's lab and kind must match its pool;
-- a slot's resource must be in the slot's pool; see 00004_slots.sql). This is the
-- "belt and suspenders" — the service enforces the same rules, the DB makes the
-- wrong combination unrepresentable.

-- +goose Up

CREATE TABLE resource_pools (
    resource_pools_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    labs_id    uuid NOT NULL REFERENCES labs(labs_id) ON DELETE CASCADE,
    kind       resource_kind NOT NULL,
    name       text NOT NULL CHECK (length(trim(name)) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid,
    updated_by uuid,
    -- FK target: forces a child row's labs_id to equal this pool's labs_id.
    UNIQUE (resource_pools_id, labs_id),
    -- FK target: forces a child row's kind to equal this pool's kind. (resource_pools_id
    -- is already unique, so this does NOT limit pools per kind — it only pairs the
    -- id with the kind for the resources.kind FK below.)
    UNIQUE (resource_pools_id, kind)
);

CREATE INDEX resource_pools_lab ON resource_pools (labs_id);

CREATE TABLE resources (
    resources_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_pools_id uuid NOT NULL,
    labs_id           uuid NOT NULL,
    kind              resource_kind NOT NULL,
    name              text NOT NULL CHECK (length(trim(name)) > 0),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    created_by        uuid,
    updated_by        uuid,

    -- The resource lives in a pool that belongs to the SAME lab: the composite FK
    -- can only resolve when resources.labs_id equals the pool's labs_id.
    FOREIGN KEY (resource_pools_id, labs_id)
        REFERENCES resource_pools (resource_pools_id, labs_id) ON DELETE CASCADE,
    -- The resource's kind matches its pool's kind (a vent-hood pool holds only
    -- vent hoods): the composite FK can only resolve when the kinds agree.
    FOREIGN KEY (resource_pools_id, kind)
        REFERENCES resource_pools (resource_pools_id, kind),

    -- FK targets for slots (00004): a slot's assigned resource must be in the
    -- slot's pool, and a slot's resource must share the slot's lab.
    UNIQUE (resources_id, resource_pools_id),
    UNIQUE (resources_id, labs_id)
);

CREATE INDEX resources_pool ON resources (resource_pools_id);

-- +goose Down

DROP TABLE IF EXISTS resources;
DROP TABLE IF EXISTS resource_pools;
