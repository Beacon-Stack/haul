-- +goose Up

-- torrent_events is the persistent audit trail behind the Activity
-- page. Every lifecycle event the bus publishes (added / completed /
-- failed / removed / stalled / state_changed) lands here so the UI
-- can render a per-torrent timeline. High-frequency noise (speed +
-- health updates) is filtered out at the subscriber and never
-- written.
--
-- payload is the bus event's Data map verbatim — keeps the schema
-- stable while letting individual event types carry their own
-- shape (peer counts on stall events, exit code on failures, etc).
--
-- The hash column is intentionally not a foreign key: a torrent can
-- be hard-deleted from the torrents table (admin diagnostics) and
-- we still want its event trail visible.

CREATE TABLE torrent_events (
    id          BIGSERIAL PRIMARY KEY,
    info_hash   TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload     JSONB NOT NULL DEFAULT '{}'
);

-- Per-torrent timeline lookup: "show me everything that happened to
-- this hash, newest first."
CREATE INDEX torrent_events_hash_time_idx ON torrent_events (info_hash, occurred_at DESC);

-- Global activity feed lookup: "show me the latest 50 events across
-- all torrents."
CREATE INDEX torrent_events_time_idx ON torrent_events (occurred_at DESC);

-- Resolution is denormalised into a column so the UI can sort by it
-- without parsing the name on every row. Source of truth is the
-- torrent name; the column is populated at Add time and re-derived
-- by this migration for existing rows.
ALTER TABLE torrents ADD COLUMN resolution TEXT NOT NULL DEFAULT '';

-- Backfill from existing names. Order matters — match the highest
-- resolution token first so "1080p.2160p.fake" doesn't get tagged
-- as 1080p. Case-insensitive on the trailing 'p'.
UPDATE torrents SET resolution = '2160p' WHERE name ~* '(^|[^0-9])(2160p|4k|uhd)([^0-9p]|$)';
UPDATE torrents SET resolution = '1080p' WHERE resolution = '' AND name ~* '(^|[^0-9])1080p([^0-9p]|$)';
UPDATE torrents SET resolution = '720p'  WHERE resolution = '' AND name ~* '(^|[^0-9])720p([^0-9p]|$)';
UPDATE torrents SET resolution = '480p'  WHERE resolution = '' AND name ~* '(^|[^0-9])(480p|sd)([^0-9p]|$)';

-- +goose Down

ALTER TABLE torrents DROP COLUMN resolution;
DROP INDEX IF EXISTS torrent_events_time_idx;
DROP INDEX IF EXISTS torrent_events_hash_time_idx;
DROP TABLE IF EXISTS torrent_events;
