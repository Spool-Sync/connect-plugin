-- The inventory of a standalone module, one with no SpoolSync server: the
-- filament types (the catalogue's and the custom ones), the storage locations,
-- the spools and the weight readings of each. With a server set these stay
-- empty: SpoolSync keeps the inventory. A time is a timestamp with its zone. A weight is
-- gross, the spool included, as SpoolSync keeps it.

CREATE TABLE filament_types (
    id                TEXT NOT NULL PRIMARY KEY,
    -- The OpenPrintTag material uuid. A catalogue import updates the type
    -- that carries it, and makes a custom one that does a catalogue one.
    opt_material_uuid TEXT NULL UNIQUE,
    brand             TEXT NOT NULL,
    material          TEXT NOT NULL,
    name              TEXT NOT NULL,
    color_hex         TEXT NULL,
    diameter_mm       DOUBLE PRECISION NOT NULL DEFAULT 1.75,
    density_g_cm3     DOUBLE PRECISION NULL,
    spool_weight_g    DOUBLE PRECISION NOT NULL DEFAULT 200,
    net_weight_g      DOUBLE PRECISION NULL,
    -- 1 for a type a person made, 0 for one of the catalogue.
    custom            INTEGER NOT NULL DEFAULT 1,
    notes             TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);

CREATE INDEX filament_types_material ON filament_types (material);
CREATE INDEX filament_types_brand ON filament_types (brand);

CREATE TABLE locations (
    id   TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE spools (
    id                TEXT NOT NULL PRIMARY KEY,
    filament_type_id  TEXT NOT NULL REFERENCES filament_types (id),
    initial_weight_g  DOUBLE PRECISION NOT NULL,
    current_weight_g  DOUBLE PRECISION NOT NULL,
    -- ACTIVE or SPENT.
    status            TEXT NOT NULL DEFAULT 'ACTIVE',
    -- The OpenPrintTag instance uuid of the spool's tag.
    opt_instance_uuid TEXT NULL UNIQUE,
    location_id       TEXT NULL REFERENCES locations (id) ON DELETE SET NULL,
    notes             TEXT NULL,
    archived_at       TIMESTAMPTZ NULL,
    archive_reason    TEXT NULL,
    archive_note      TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL
);

CREATE INDEX spools_filament_type ON spools (filament_type_id);

-- Each weight a spool was given, for the usage chart.
CREATE TABLE weight_history (
    id          TEXT NOT NULL PRIMARY KEY,
    spool_id    TEXT NOT NULL REFERENCES spools (id) ON DELETE CASCADE,
    weight_g    DOUBLE PRECISION NOT NULL,
    source      TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX weight_history_recorded ON weight_history (recorded_at);
CREATE INDEX weight_history_spool ON weight_history (spool_id);
