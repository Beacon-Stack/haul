-- +goose Up

-- stalled_at marks the moment the stall watcher gave up on a torrent
-- and auto-paused it. Replaces the old "category = 'archived'" hack
-- which (a) stomped the requester's actual category and (b) made the
-- torrent invisible to /api/v1/torrents because the engine dropped
-- it from memory entirely.
--
-- Going forward: the watcher pauses the torrent and stamps
-- stalled_at; the torrent stays in s.torrents and the live list, just
-- in a paused state with a 'stalled' tag the UI uses to filter and
-- group. Resume clears stalled_at + the tag.
ALTER TABLE torrents ADD COLUMN stalled_at TIMESTAMPTZ;

-- Backfill: every torrent currently labelled 'archived' was put there
-- by the old auto-archive path. Restamp them with stalled_at = now()
-- so the UI lights them up as "needs attention" on first load post-
-- migration. Restore a sensible category from the requester (Pilot
-- always meant tv, Prism always meant movies; manual / empty stays
-- empty), then add the 'stalled' tag so the new tag-based filter
-- shows them.
UPDATE torrents
SET stalled_at = NOW(),
    category = CASE
        WHEN requester_service = 'pilot' THEN 'tv'
        WHEN requester_service = 'prism' THEN 'movies'
        ELSE ''
    END
WHERE category = 'archived';

INSERT INTO torrent_tags (info_hash, tag)
SELECT info_hash, 'stalled' FROM torrents WHERE stalled_at IS NOT NULL
ON CONFLICT DO NOTHING;

-- Index so the dashboard "Needs attention" rail can count active
-- stalled rows without scanning the whole table.
CREATE INDEX torrents_stalled_at_idx ON torrents (stalled_at) WHERE stalled_at IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS torrents_stalled_at_idx;
DELETE FROM torrent_tags WHERE tag = 'stalled';
ALTER TABLE torrents DROP COLUMN stalled_at;
