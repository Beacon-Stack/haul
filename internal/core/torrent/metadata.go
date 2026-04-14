package torrent

import (
	"encoding/json"
	"fmt"
)

// RequesterMetadata holds structured context from the service that requested the download.
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
}

// SetMetadata attaches structured requester metadata to a torrent.
func (s *Session) SetMetadata(hash string, meta RequesterMetadata) error {
	s.mu.RLock()
	_, ok := s.torrents[hash]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("torrent not found: %s", hash)
	}

	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	_, err = s.db.Exec(`UPDATE torrents SET metadata = $1 WHERE info_hash = $2`, string(data), hash)
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
