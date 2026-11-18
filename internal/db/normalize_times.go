package db

import (
	"database/sql"
	"fmt"

	"github.com/beacon-stack/haul/internal/dbtime"
)

// torrentTimeColumns are the TEXT timestamp columns on torrents that older
// Haul versions wrote by binding a time.Time directly, which stores
// time.Time.String() output instead of RFC3339.
var torrentTimeColumns = []string{"added_at", "completed_at", "stalled_at", "removed_at"}

// NormalizeTorrentTimes rewrites every torrents timestamp that isn't in
// dbtime's format. It runs on every startup and only touches rows that
// need it, so after the first run it's a cheap scan. Values it can't
// parse are left as they are.
//
// Done in Go rather than as a SQL migration because the old values carry
// a zone and offset that SQLite's date functions can't parse.
func NormalizeTorrentTimes(sqlDB *sql.DB) (int, error) {
	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	changed := 0
	for _, col := range torrentTimeColumns {
		// col comes from the fixed list above, never from input.
		rows, err := tx.Query(fmt.Sprintf(`SELECT info_hash, %s FROM torrents WHERE %s IS NOT NULL`, col, col))
		if err != nil {
			return 0, fmt.Errorf("reading torrents.%s: %w", col, err)
		}
		type fix struct{ hash, value string }
		var fixes []fix
		for rows.Next() {
			var hash, v string
			if err := rows.Scan(&hash, &v); err != nil {
				_ = rows.Close()
				return 0, fmt.Errorf("scanning torrents.%s: %w", col, err)
			}
			if dbtime.IsCanonical(v) {
				continue
			}
			if nv, ok := dbtime.Normalize(v); ok {
				fixes = append(fixes, fix{hash, nv})
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, err
		}
		_ = rows.Close()

		for _, f := range fixes {
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE torrents SET %s = ? WHERE info_hash = ?`, col), f.value, f.hash); err != nil {
				return 0, fmt.Errorf("updating torrents.%s: %w", col, err)
			}
		}
		changed += len(fixes)
	}
	return changed, tx.Commit()
}
