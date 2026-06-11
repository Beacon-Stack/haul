-- +goose Up
-- Per-category upload_limit/download_limit were a CRUD round-trip with no
-- engine reader and no UI control — the same phantom surface as the
-- per-torrent limits dropped in 00002. The category save_path stays and
-- is now actually applied by Session.Add.
ALTER TABLE categories DROP COLUMN upload_limit;
ALTER TABLE categories DROP COLUMN download_limit;

-- +goose Down
ALTER TABLE categories ADD COLUMN upload_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE categories ADD COLUMN download_limit INTEGER NOT NULL DEFAULT 0;
