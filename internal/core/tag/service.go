package tag

import (
	"database/sql"
	"fmt"
)

// Service manages torrent tags.
type Service struct {
	db *sql.DB
}

// NewService creates a new tag service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// List returns all unique tags.
func (s *Service) List() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT tag FROM torrent_tags ORDER BY tag`)
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scanning tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// AddToTorrent adds a tag to a torrent.
func (s *Service) AddToTorrent(infoHash, tag string) error {
	_, err := s.db.Exec(`INSERT INTO torrent_tags (info_hash, tag) VALUES ($1, $2) ON CONFLICT DO NOTHING`, infoHash, tag)
	if err != nil {
		return fmt.Errorf("adding tag: %w", err)
	}
	return nil
}

// RemoveFromTorrent removes a tag from a torrent.
func (s *Service) RemoveFromTorrent(infoHash, tag string) error {
	_, err := s.db.Exec(`DELETE FROM torrent_tags WHERE info_hash = $1 AND tag = $2`, infoHash, tag)
	if err != nil {
		return fmt.Errorf("removing tag: %w", err)
	}
	return nil
}

// GetForTorrent returns all tags for a torrent.
func (s *Service) GetForTorrent(infoHash string) ([]string, error) {
	rows, err := s.db.Query(`SELECT tag FROM torrent_tags WHERE info_hash = $1 ORDER BY tag`, infoHash)
	if err != nil {
		return nil, fmt.Errorf("getting tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scanning tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// DeleteTag removes a tag from all torrents.
func (s *Service) DeleteTag(tag string) error {
	_, err := s.db.Exec(`DELETE FROM torrent_tags WHERE tag = $1`, tag)
	if err != nil {
		return fmt.Errorf("deleting tag: %w", err)
	}
	return nil
}
