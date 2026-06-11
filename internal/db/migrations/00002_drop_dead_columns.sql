-- +goose Up
-- Drop torrents columns that backed features removed in the 2026-06
-- dead-code sweep. None of them ever had a reader:
--
--   deadline        — deadline-based priority scheduler (SetDeadline /
--                     GetDeadline / EffectivePriority) that was never
--                     wired into the engine
--   sequential      — "sequential download" toggle whose engine half was
--                     a disguised no-op (SetDisplayName), persisted as a
--                     hardcoded FALSE
--   download_limit  — per-torrent speed limits written by an endpoint
--   upload_limit      no client ever called; the engine reads only the
--                     global limiters (categories keep their own limits)
ALTER TABLE torrents DROP COLUMN deadline;
ALTER TABLE torrents DROP COLUMN sequential;
ALTER TABLE torrents DROP COLUMN download_limit;
ALTER TABLE torrents DROP COLUMN upload_limit;

-- +goose Down
ALTER TABLE torrents ADD COLUMN deadline TEXT;
ALTER TABLE torrents ADD COLUMN sequential BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE torrents ADD COLUMN download_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN upload_limit INTEGER NOT NULL DEFAULT 0;
