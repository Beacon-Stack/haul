// Package activity persists bus lifecycle events into the
// torrent_events table so the Activity page can render a full
// history (across restarts) instead of just the in-memory live
// stream.
//
// What gets persisted: every event with an InfoHash, except the
// high-frequency telemetry types (speed_update, health_update). The
// noise types are deliberately filtered here, not at the bus, since
// the live websocket stream still needs them for the dashboard
// gauges.
//
// Failures are logged and swallowed — losing one row of activity
// history is not worth crashing a download for.
package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/beacon-stack/haul/internal/events"
)

// noisyTypes are the high-frequency events we never persist. Keep
// this list tight: when in doubt, persist; the table can absorb a
// lot of rows but a missing event leaves a gap a user might notice.
var noisyTypes = map[events.Type]struct{}{
	events.TypeSpeedUpdate:  {},
	events.TypeHealthUpdate: {},
}

// execer is the narrow surface Persister actually uses on *sql.DB.
// Defined as an interface so the unit test can supply a fake without
// standing up a real Postgres.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Persister is a bus subscriber that writes lifecycle events to the
// torrent_events table.
type Persister struct {
	db     execer
	logger *slog.Logger
}

// NewPersister builds a Persister. Pass the result of its HandleEvent
// method to bus.Subscribe.
func NewPersister(db *sql.DB, logger *slog.Logger) *Persister {
	// nil DB is an explicit valid case: the persister becomes a no-op.
	// Cast to the interface only when non-nil so the nil-DB branch in
	// HandleEvent still triggers (a typed nil interface is not == nil).
	if db == nil {
		return &Persister{db: nil, logger: logger}
	}
	return &Persister{db: db, logger: logger}
}

// HandleEvent is the bus subscriber callback. Drops noisy types and
// events without an InfoHash (those are not torrent-scoped — e.g.
// session-wide health pings).
func (p *Persister) HandleEvent(ctx context.Context, e events.Event) {
	if p.db == nil {
		return
	}
	if _, skip := noisyTypes[e.Type]; skip {
		return
	}
	if e.InfoHash == "" {
		return
	}

	// Marshal the data map into JSONB. An empty map is fine — the
	// column defaults to '{}' anyway.
	payload := []byte("{}")
	if len(e.Data) > 0 {
		b, err := json.Marshal(e.Data)
		if err != nil {
			p.logger.Warn("activity persist: marshal payload failed",
				"error", err, "type", e.Type, "hash", e.InfoHash)
			return
		}
		payload = b
	}

	_, err := p.db.ExecContext(ctx, `
		INSERT INTO torrent_events (info_hash, event_type, occurred_at, payload)
		VALUES ($1, $2, $3, $4)`,
		e.InfoHash, string(e.Type), e.Timestamp, payload,
	)
	if err != nil {
		p.logger.Warn("activity persist: insert failed",
			"error", err, "type", e.Type, "hash", e.InfoHash)
	}
}
