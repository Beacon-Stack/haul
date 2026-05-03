-- +goose Up

-- cleanup_history captures rows removed via the admin diagnostics tab so
-- the operator can review what was deleted and restore it inside the
-- retention window. Rows here are JSONB snapshots of the original row
-- — restore reconstructs the row in its original table.
--
-- See plans/db-inspector.md for the design rationale (chosen over
-- per-table deleted_at columns to avoid schema drift across every
-- existing query).

CREATE TABLE cleanup_history (
    id                BIGSERIAL PRIMARY KEY,
    diagnostic        TEXT NOT NULL,           -- e.g. "orphan_torrents"
    source_table      TEXT NOT NULL,           -- e.g. "torrents"
    source_pk         TEXT NOT NULL,           -- stringified primary key (info_hash, id, …)
    row_data          JSONB NOT NULL,          -- the full original row at time of deletion
    deleted_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    request_id        TEXT NOT NULL DEFAULT '',
    actor_key_prefix  TEXT NOT NULL DEFAULT ''
);

-- The retention sweep runs `WHERE deleted_at < now() - interval`.
CREATE INDEX cleanup_history_deleted_at_idx ON cleanup_history (deleted_at);

-- Restore + dedupe lookups go by (table, pk).
CREATE INDEX cleanup_history_table_pk_idx ON cleanup_history (source_table, source_pk);

-- +goose Down
DROP TABLE IF EXISTS cleanup_history;
