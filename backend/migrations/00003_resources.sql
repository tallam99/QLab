-- Equipment as a POOL of interchangeable resources (ALGORITHM §1.4, §10): a pool
-- groups resources of one kind; the engine fans the queue out across them and
-- picks the specific resource near clock-in. MVP ships one pool of vent hoods.
--
-- The extra UNIQUE constraints here exist to be FOREIGN KEY targets: they let
-- downstream tables enforce cross-row domain rules in the DB itself (a resource's
-- lab and kind must match its pool; a slot's resource must be in the slot's pool;
-- see 00004_slots.sql). This is the "belt and suspenders" — the service enforces
-- the same rules, the DB makes the wrong combination unrepresentable.

-- +goose Up

CREATE TABLE resource_pools (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    lab_id     uuid NOT NULL REFERENCES labs(id) ON DELETE CASCADE,
    kind       resource_kind NOT NULL,
    name       text NOT NULL CHECK (length(trim(name)) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    -- FK target: forces a child row's lab_id to equal this pool's lab_id.
    UNIQUE (id, lab_id),
    -- FK target: forces a child row's kind to equal this pool's kind.
    UNIQUE (id, kind)
);

CREATE INDEX resource_pools_lab ON resource_pools (lab_id);

CREATE TABLE resources (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_pool_id uuid NOT NULL,
    lab_id           uuid NOT NULL,
    kind             resource_kind NOT NULL,
    name             text NOT NULL CHECK (length(trim(name)) > 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    -- The resource lives in a pool that belongs to the SAME lab: the composite FK
    -- can only resolve when resources.lab_id equals the pool's lab_id.
    FOREIGN KEY (resource_pool_id, lab_id)
        REFERENCES resource_pools (id, lab_id) ON DELETE CASCADE,
    -- The resource's kind matches its pool's kind (a vent-hood pool holds only
    -- vent hoods): the composite FK can only resolve when the kinds agree.
    FOREIGN KEY (resource_pool_id, kind)
        REFERENCES resource_pools (id, kind),

    -- FK targets for slots (00004): a slot's assigned resource must be in the
    -- slot's pool, and a slot's resource must share the slot's lab.
    UNIQUE (id, resource_pool_id),
    UNIQUE (id, lab_id)
);

CREATE INDEX resources_pool ON resources (resource_pool_id);

-- +goose Down

DROP TABLE IF EXISTS resources;
DROP TABLE IF EXISTS resource_pools;
