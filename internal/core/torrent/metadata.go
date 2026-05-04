package torrent

import (
	"encoding/json"
	"fmt"
)

// RequesterMetadata holds structured context from the service that requested the download.
//
// The MovieID/SeriesID/EpisodeID fields carry the arr's own UUIDs so
// Pilot/Prism can later look up "have I downloaded anything for
// episode_id=X?" via Haul's history endpoints. They're opaque strings
// to Haul — Haul never resolves them, just stores and returns them.
type RequesterMetadata struct {
	Requester     string `json:"requester,omitempty"`      // "prism", "pilot", "manual"
	MediaType     string `json:"media_type,omitempty"`     // "movie", "tv", "unknown"
	Title         string `json:"title,omitempty"`          // "Breaking Bad" or "Fight Club"
	Year          int    `json:"year,omitempty"`           // 2008
	TMDBID        int    `json:"tmdb_id,omitempty"`        // 550
	SeasonNumber  int    `json:"season_number,omitempty"`  // 1
	EpisodeNumber int    `json:"episode_number,omitempty"` // 4
	EpisodeTitle  string `json:"episode_title,omitempty"`  // "Cancer Man"
	Quality       string `json:"quality,omitempty"`        // "Bluray-1080p"
	QualityCodec  string `json:"quality_codec,omitempty"`  // "x265"
	RequestedBy   string `json:"requested_by,omitempty"`   // user who requested
	RequestedAt   string `json:"requested_at,omitempty"`   // ISO8601
	// Arr-side identifiers — UUID-shaped strings the requester uses to
	// reference its own DB rows. Empty when the caller didn't supply them.
	MovieID   string `json:"movie_id,omitempty"`   // Prism movie UUID
	SeriesID  string `json:"series_id,omitempty"`  // Pilot series UUID
	EpisodeID string `json:"episode_id,omitempty"` // Pilot episode UUID
}

// SetMetadata attaches structured requester metadata to a torrent.
//
// Persists the full struct to the `metadata` JSON column AND
// denormalizes the indexed fields (requester_*, requester_tmdb_id,
// season, episode, movie_id, series_id, episode_id) into their own
// columns so Pilot/Prism's history-lookup queries can hit indexes
// instead of scanning JSON.
func (s *Session) SetMetadata(hash string, meta RequesterMetadata) error {
	s.mu.Lock()
	mt, ok := s.torrents[hash]
	if ok {
		// Mirror the requester string into in-memory state so the
		// /api/v1/torrents endpoint can surface it without a DB hit.
		mt.requester = meta.Requester
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	_, err = s.db.Exec(`UPDATE torrents SET
		metadata             = $1,
		requester_service    = $2,
		requester_movie_id   = $3,
		requester_series_id  = $4,
		requester_episode_id = $5,
		requester_tmdb_id    = $6,
		requester_season     = $7,
		requester_episode    = $8
	WHERE info_hash = $9`,
		string(data),
		meta.Requester,
		meta.MovieID,
		meta.SeriesID,
		meta.EpisodeID,
		meta.TMDBID,
		meta.SeasonNumber,
		meta.EpisodeNumber,
		hash,
	)
	return err
}

// GetMetadata retrieves the requester metadata for a torrent.
func (s *Session) GetMetadata(hash string) (*RequesterMetadata, error) {
	var raw string
	err := s.db.QueryRow(`SELECT metadata FROM torrents WHERE info_hash = $1`, hash).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}

	var meta RequesterMetadata
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &meta); err != nil {
			return nil, fmt.Errorf("parsing metadata: %w", err)
		}
	}
	return &meta, nil
}
