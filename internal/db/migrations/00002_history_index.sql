-- +goose Up

-- Promote requester metadata into indexed columns so Pilot/Prism can
-- ask "have you ever downloaded this?" without scanning the full
-- torrents table or parsing JSON in WHERE clauses.
--
-- Source of truth stays the `metadata` JSON column (so callers that
-- write extra fields not modeled here aren't lost). The indexed
-- columns are denormalized at write time by Session.SetMetadata.
--
-- Existing rows are back-filled by parsing the metadata JSON below.

ALTER TABLE torrents
  ADD COLUMN removed_at           TIMESTAMPTZ,            -- nullable; record kept after remove
  ADD COLUMN requester_service    TEXT NOT NULL DEFAULT '', -- "pilot" | "prism" | "manual" | ""
  ADD COLUMN requester_movie_id   TEXT NOT NULL DEFAULT '', -- arr's UUID
  ADD COLUMN requester_series_id  TEXT NOT NULL DEFAULT '',
  ADD COLUMN requester_episode_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN requester_tmdb_id    INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN requester_season     INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN requester_episode    INTEGER NOT NULL DEFAULT 0;

-- Back-fill from the existing JSON metadata. We look up the same
-- fields the live SetMetadata write path emits going forward.
-- jsonb_path_query_first wouldn't work because metadata is TEXT, not
-- JSONB; cast at extraction time. NULL-safe via COALESCE.
UPDATE torrents
SET
  requester_service = COALESCE(metadata::jsonb ->> 'requester', ''),
  requester_tmdb_id = COALESCE((metadata::jsonb ->> 'tmdb_id')::INTEGER, 0),
  requester_season  = COALESCE((metadata::jsonb ->> 'season_number')::INTEGER, 0),
  requester_episode = COALESCE((metadata::jsonb ->> 'episode_number')::INTEGER, 0)
WHERE metadata IS NOT NULL AND metadata <> '' AND metadata <> '{}';

-- Composite index for the common Pilot/Prism query: "did service X
-- ever download tmdb=Y season=S episode=E?". The partial WHERE
-- excludes the (default 0/empty) zero rows so the index stays small.
CREATE INDEX idx_torrents_requester_tmdb ON torrents (
  requester_service, requester_tmdb_id, requester_season, requester_episode
) WHERE requester_tmdb_id > 0;

-- Lookups by arr-side ID (movie/series/episode UUID). Used by the
-- per-row library badge — "do you have anything for episode_id=X?".
CREATE INDEX idx_torrents_requester_movie ON torrents (requester_service, requester_movie_id)
  WHERE requester_movie_id <> '';
CREATE INDEX idx_torrents_requester_series ON torrents (requester_service, requester_series_id)
  WHERE requester_series_id <> '';
CREATE INDEX idx_torrents_requester_episode_id ON torrents (requester_service, requester_episode_id)
  WHERE requester_episode_id <> '';

-- +goose Down

DROP INDEX IF EXISTS idx_torrents_requester_episode_id;
DROP INDEX IF EXISTS idx_torrents_requester_series;
DROP INDEX IF EXISTS idx_torrents_requester_movie;
DROP INDEX IF EXISTS idx_torrents_requester_tmdb;

ALTER TABLE torrents
  DROP COLUMN requester_episode,
  DROP COLUMN requester_season,
  DROP COLUMN requester_tmdb_id,
  DROP COLUMN requester_episode_id,
  DROP COLUMN requester_series_id,
  DROP COLUMN requester_movie_id,
  DROP COLUMN requester_service,
  DROP COLUMN removed_at;
