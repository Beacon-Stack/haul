-- +goose Up

CREATE TABLE torrents (
    info_hash          TEXT PRIMARY KEY,
    name               TEXT NOT NULL,
    save_path          TEXT NOT NULL,
    category           TEXT NOT NULL DEFAULT '',
    added_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at       TIMESTAMPTZ,
    size_bytes         INTEGER NOT NULL DEFAULT 0,
    sequential         BOOLEAN NOT NULL DEFAULT FALSE,
    seed_ratio_limit   REAL,
    seed_time_limit    INTEGER,
    priority           INTEGER NOT NULL DEFAULT 0,
    upload_limit       INTEGER NOT NULL DEFAULT 0,
    download_limit     INTEGER NOT NULL DEFAULT 0,
    metadata           TEXT NOT NULL DEFAULT '{}',
    last_activity_at   TIMESTAMPTZ,
    deadline           TIMESTAMPTZ,
    seed_limit_action  TEXT NOT NULL DEFAULT '',
    torrent_data       BYTEA
);

CREATE TABLE torrent_tags (
    info_hash TEXT NOT NULL REFERENCES torrents(info_hash) ON DELETE CASCADE,
    tag       TEXT NOT NULL,
    PRIMARY KEY (info_hash, tag)
);

CREATE TABLE categories (
    name           TEXT PRIMARY KEY,
    save_path      TEXT NOT NULL DEFAULT '',
    upload_limit   INTEGER NOT NULL DEFAULT 0,
    download_limit INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE webhooks (
    id      TEXT PRIMARY KEY,
    url     TEXT NOT NULL,
    events  TEXT NOT NULL DEFAULT '[]',
    enabled BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS torrent_tags;
DROP TABLE IF EXISTS torrents;
DROP TABLE IF EXISTS categories;
