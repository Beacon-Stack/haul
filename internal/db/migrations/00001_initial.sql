-- +goose Up

-- ── Torrents ────────────────────────────────────────────────────────────────
-- The full torrents table assembled from 00001 plus the columns layered on by
-- 00002 (requester denormalisation), 00004 (resolution), and 00005 (stalled_at).
CREATE TABLE torrents (
    info_hash            TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    save_path            TEXT NOT NULL,
    category             TEXT NOT NULL DEFAULT '',
    added_at             TEXT NOT NULL,
    completed_at         TEXT,
    size_bytes           INTEGER NOT NULL DEFAULT 0,
    sequential           BOOLEAN NOT NULL DEFAULT FALSE,
    seed_ratio_limit     REAL,
    seed_time_limit      INTEGER,
    priority             INTEGER NOT NULL DEFAULT 0,
    upload_limit         INTEGER NOT NULL DEFAULT 0,
    download_limit       INTEGER NOT NULL DEFAULT 0,
    metadata             TEXT NOT NULL DEFAULT '{}',
    last_activity_at     TEXT,
    deadline             TEXT,
    seed_limit_action    TEXT NOT NULL DEFAULT '',
    torrent_data         BLOB,
    removed_at           TEXT,
    requester_service    TEXT NOT NULL DEFAULT '',
    requester_movie_id   TEXT NOT NULL DEFAULT '',
    requester_series_id  TEXT NOT NULL DEFAULT '',
    requester_episode_id TEXT NOT NULL DEFAULT '',
    requester_tmdb_id    INTEGER NOT NULL DEFAULT 0,
    requester_season     INTEGER NOT NULL DEFAULT 0,
    requester_episode    INTEGER NOT NULL DEFAULT 0,
    resolution           TEXT NOT NULL DEFAULT '',
    stalled_at           TEXT
);

-- Requester partial indexes from 00002. Original WHERE-clause forms preserved
-- so the indexes stay small (only rows with a requester identity are covered).
CREATE INDEX idx_torrents_requester_tmdb ON torrents (
  requester_service, requester_tmdb_id, requester_season, requester_episode
) WHERE requester_tmdb_id > 0;

CREATE INDEX idx_torrents_requester_movie ON torrents (requester_service, requester_movie_id)
  WHERE requester_movie_id <> '';
CREATE INDEX idx_torrents_requester_series ON torrents (requester_service, requester_series_id)
  WHERE requester_series_id <> '';
CREATE INDEX idx_torrents_requester_episode_id ON torrents (requester_service, requester_episode_id)
  WHERE requester_episode_id <> '';

-- Stalled-row dashboard index from 00005.
CREATE INDEX torrents_stalled_at_idx ON torrents (stalled_at) WHERE stalled_at IS NOT NULL;

-- ── Torrent tags ─────────────────────────────────────────────────────────────
CREATE TABLE torrent_tags (
    info_hash TEXT NOT NULL REFERENCES torrents(info_hash) ON DELETE CASCADE,
    tag       TEXT NOT NULL,
    PRIMARY KEY (info_hash, tag)
);

-- ── Categories ──────────────────────────────────────────────────────────────
CREATE TABLE categories (
    name           TEXT PRIMARY KEY,
    save_path      TEXT NOT NULL DEFAULT '',
    upload_limit   INTEGER NOT NULL DEFAULT 0,
    download_limit INTEGER NOT NULL DEFAULT 0
);

-- ── Webhooks ────────────────────────────────────────────────────────────────
CREATE TABLE webhooks (
    id      TEXT PRIMARY KEY,
    url     TEXT NOT NULL,
    events  TEXT NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

-- ── Settings ────────────────────────────────────────────────────────────────
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- ── Cleanup history (was 00003) ─────────────────────────────────────────────
-- Snapshots of rows removed via the admin diagnostics tab. row_data is a JSON
-- snapshot of the original row at deletion time; restore reconstructs from it.
-- BIGSERIAL becomes INTEGER PRIMARY KEY AUTOINCREMENT (never-reuse semantics
-- matching Postgres sequences). JSONB collapses to TEXT.
CREATE TABLE cleanup_history (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    diagnostic        TEXT NOT NULL,
    source_table      TEXT NOT NULL,
    source_pk         TEXT NOT NULL,
    row_data          TEXT NOT NULL,
    deleted_at        TEXT NOT NULL,
    request_id        TEXT NOT NULL DEFAULT '',
    actor_key_prefix  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX cleanup_history_deleted_at_idx ON cleanup_history (deleted_at);
CREATE INDEX cleanup_history_table_pk_idx ON cleanup_history (source_table, source_pk);

-- ── Torrent events (was 00004) ──────────────────────────────────────────────
-- Persistent audit trail behind the Activity page. payload is the bus event's
-- Data map verbatim (TEXT-encoded JSON). info_hash is intentionally not a
-- foreign key so events survive admin hard-deletes of their torrents.
CREATE TABLE torrent_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    info_hash   TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX torrent_events_hash_time_idx ON torrent_events (info_hash, occurred_at DESC);
CREATE INDEX torrent_events_time_idx ON torrent_events (occurred_at DESC);

-- +goose Down

DROP INDEX IF EXISTS torrent_events_time_idx;
DROP INDEX IF EXISTS torrent_events_hash_time_idx;
DROP TABLE IF EXISTS torrent_events;
DROP INDEX IF EXISTS cleanup_history_table_pk_idx;
DROP INDEX IF EXISTS cleanup_history_deleted_at_idx;
DROP TABLE IF EXISTS cleanup_history;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS torrent_tags;
DROP INDEX IF EXISTS torrents_stalled_at_idx;
DROP INDEX IF EXISTS idx_torrents_requester_episode_id;
DROP INDEX IF EXISTS idx_torrents_requester_series;
DROP INDEX IF EXISTS idx_torrents_requester_movie;
DROP INDEX IF EXISTS idx_torrents_requester_tmdb;
DROP TABLE IF EXISTS torrents;
