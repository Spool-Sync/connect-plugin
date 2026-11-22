-- The module's own state, now that SpoolSync is the only inventory. A time is
-- a timestamp with its zone.

-- What a person put on a printer tool that reads no tag: a material, and a
-- spool of it when one was picked. A tool that reads a tag has no row here:
-- its tag names the spool. A tool may carry a material with no spool.
CREATE TABLE assignments (
    printer_id      TEXT NOT NULL,
    tool            TEXT NOT NULL,
    filament_type_id TEXT NULL,
    spool_id        TEXT NULL,
    assigned_at TIMESTAMPTZ NOT NULL,
    assigned_by TEXT NOT NULL,
    PRIMARY KEY (printer_id, tool)
);

CREATE INDEX assignments_spool ON assignments (spool_id);

-- The materials each person starred, and the list filters each kept. Both
-- belong to the person, not to the inventory.
CREATE TABLE favourites (
    person           TEXT NOT NULL,
    filament_type_id TEXT NOT NULL,
    PRIMARY KEY (person, filament_type_id)
);

CREATE TABLE list_filters (
    person  TEXT NOT NULL PRIMARY KEY,
    filters TEXT NOT NULL
);

-- The last reading of a tool's odometer, so the filament fed between two
-- readings is counted once, also across a restart. odometer_id changes at each
-- reset of the counter; weight_g is NULL for a printer that reports no weight.
CREATE TABLE odometers (
    printer_id  TEXT NOT NULL,
    tool        TEXT NOT NULL,
    odometer_id BIGINT NULL,
    e_mm        DOUBLE PRECISION NOT NULL,
    weight_g    DOUBLE PRECISION NULL,
    updated_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (printer_id, tool)
);

-- The grams counted off a spool that SpoolSync does not hold yet: a whole gram
-- is written, and the fraction waits for the next print.
CREATE TABLE pending (
    spool_id TEXT NOT NULL PRIMARY KEY,
    grams    DOUBLE PRECISION NOT NULL DEFAULT 0
);
