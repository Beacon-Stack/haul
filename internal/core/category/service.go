package category

import (
	"database/sql"
	"fmt"
)

// Category represents a torrent category with an optional default
// save path, applied by Session.Add when the caller doesn't override.
type Category struct {
	Name     string `json:"name"`
	SavePath string `json:"save_path,omitempty" required:"false"`
}

// Service manages torrent categories.
type Service struct {
	db *sql.DB
}

// NewService creates a new category service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// List returns all categories.
func (s *Service) List() ([]Category, error) {
	rows, err := s.db.Query(`SELECT name, save_path FROM categories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing categories: %w", err)
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Name, &c.SavePath); err != nil {
			return nil, fmt.Errorf("scanning category: %w", err)
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// Get returns a single category by name.
func (s *Service) Get(name string) (*Category, error) {
	var c Category
	err := s.db.QueryRow(`SELECT name, save_path FROM categories WHERE name = ?`, name).
		Scan(&c.Name, &c.SavePath)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("category not found: %s", name)
	}
	if err != nil {
		return nil, fmt.Errorf("getting category: %w", err)
	}
	return &c, nil
}

// Create creates a new category.
func (s *Service) Create(c Category) error {
	_, err := s.db.Exec(`INSERT INTO categories (name, save_path) VALUES (?, ?)`,
		c.Name, c.SavePath)
	if err != nil {
		return fmt.Errorf("creating category: %w", err)
	}
	return nil
}

// Update updates an existing category.
func (s *Service) Update(name string, c Category) error {
	res, err := s.db.Exec(`UPDATE categories SET save_path = ? WHERE name = ?`,
		c.SavePath, name)
	if err != nil {
		return fmt.Errorf("updating category: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("category not found: %s", name)
	}
	return nil
}

// Delete deletes a category. Torrents in this category are not removed.
func (s *Service) Delete(name string) error {
	res, err := s.db.Exec(`DELETE FROM categories WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("deleting category: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("category not found: %s", name)
	}
	return nil
}
