-- The filament each print took, for the Jobs listing's column and a print's
-- page. A job is the id Prusa Connect gives each print. A weight is grams of
-- filament, the spool not included.

-- The print running on each printer, from the job events, so a count made
-- after a restart mid-print still knows its job.
CREATE TABLE printer_jobs (
    printer_id TEXT NOT NULL PRIMARY KEY,
    job_id     TEXT NOT NULL
);

-- The grams one print took off one spool, on one tool.
CREATE TABLE job_usage (
    job_id     TEXT NOT NULL,
    printer_id TEXT NOT NULL,
    tool       TEXT NOT NULL,
    spool_id   TEXT NOT NULL,
    grams      REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (job_id, tool, spool_id)
);
