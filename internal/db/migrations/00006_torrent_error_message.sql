-- +goose Up

-- error_message captures the most recent restore failure for a torrent.
-- See TODO.md BUG-005: anacrolix-torrent chmods completed files to 0o444
-- on promotion, which then breaks every subsequent restart because the
-- library can't reopen its own read-only files for read+write. The
-- restore path now auto-heals 0o444 files and only persists an error
-- message when that fallback fails — typical operator-visible cases are
-- a missing mount point or corrupted .torrent data, both of which need
-- human intervention.
--
-- Cleared on successful AddTorrent (including the auto-heal retry path).
-- Surfaced on /api/v1/torrents as the new "error" status with the
-- message string for the UI to render.
ALTER TABLE torrents ADD COLUMN error_message TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE torrents DROP COLUMN error_message;
