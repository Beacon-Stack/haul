package activity

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/beacon-stack/haul/internal/events"
)

// fakeExecer captures every ExecContext call so the test can assert
// the query and args. ExecContext is the only method Persister
// touches; nothing else needs to be implemented.
type fakeExecer struct {
	calls []execCall
	// err is returned from ExecContext when set. Used to verify the
	// persister logs and swallows the failure rather than panicking.
	err error
}

type execCall struct {
	query string
	args  []any
}

func (f *fakeExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.calls = append(f.calls, execCall{query: query, args: args})
	return nil, f.err
}

func newPersister(db execer) *Persister {
	return &Persister{db: db, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestPersister_WritesLifecycleEvent(t *testing.T) {
	fake := &fakeExecer{}
	p := newPersister(fake)

	ts := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	p.HandleEvent(context.Background(), events.Event{
		Type:      events.TypeTorrentAdded,
		Timestamp: ts,
		InfoHash:  "abc123",
		Data:      map[string]any{"name": "Ubuntu", "size": 1024},
	})

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly 1 INSERT, got %d", len(fake.calls))
	}
	c := fake.calls[0]
	if !strings.Contains(c.query, "INSERT INTO torrent_events") {
		t.Fatalf("expected INSERT INTO torrent_events, got: %s", c.query)
	}
	if len(c.args) != 4 {
		t.Fatalf("expected 4 args (hash, type, ts, payload), got %d: %v", len(c.args), c.args)
	}
	if c.args[0] != "abc123" {
		t.Errorf("hash arg: got %v, want abc123", c.args[0])
	}
	if c.args[1] != "torrent_added" {
		t.Errorf("type arg: got %v, want torrent_added", c.args[1])
	}
	if c.args[2] != ts {
		t.Errorf("ts arg: got %v, want %v", c.args[2], ts)
	}
	// payload is JSON bytes — must contain both fields, in either order.
	payload, ok := c.args[3].([]byte)
	if !ok {
		t.Fatalf("payload arg: expected []byte, got %T", c.args[3])
	}
	if !strings.Contains(string(payload), `"name":"Ubuntu"`) || !strings.Contains(string(payload), `"size":1024`) {
		t.Errorf("payload missing fields: %s", payload)
	}
}

func TestPersister_SkipsNoisyTypes(t *testing.T) {
	fake := &fakeExecer{}
	p := newPersister(fake)

	for _, ty := range []events.Type{events.TypeSpeedUpdate, events.TypeHealthUpdate} {
		p.HandleEvent(context.Background(), events.Event{
			Type:      ty,
			InfoHash:  "abc123",
			Timestamp: time.Now(),
		})
	}

	if len(fake.calls) != 0 {
		t.Fatalf("noisy types must NOT be persisted, got %d INSERT calls", len(fake.calls))
	}
}

func TestPersister_SkipsEventsWithoutInfoHash(t *testing.T) {
	// Session-wide events (e.g. health pings) come through with an
	// empty InfoHash. They have no torrent to attach to, so they
	// belong on the live websocket only — not in the per-torrent
	// timeline.
	fake := &fakeExecer{}
	p := newPersister(fake)

	p.HandleEvent(context.Background(), events.Event{
		Type:      events.TypeTorrentStateChanged,
		InfoHash:  "",
		Timestamp: time.Now(),
	})

	if len(fake.calls) != 0 {
		t.Fatalf("events without info_hash must be skipped, got %d INSERT calls", len(fake.calls))
	}
}

func TestPersister_NilDBIsNoOp(t *testing.T) {
	// The persister is constructed before the bus loop runs; if the
	// DB happens to be nil (test build, in-memory mode, etc) we
	// must not crash on the first event.
	p := NewPersister(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil-DB persister panicked: %v", r)
		}
	}()
	p.HandleEvent(context.Background(), events.Event{
		Type:      events.TypeTorrentAdded,
		InfoHash:  "abc123",
		Timestamp: time.Now(),
	})
}

func TestPersister_EmptyDataMapStillInserts(t *testing.T) {
	// torrent_completed and torrent_removed often fire with an empty
	// Data map — only the hash matters. The row should still land,
	// with payload defaulting to "{}".
	fake := &fakeExecer{}
	p := newPersister(fake)

	p.HandleEvent(context.Background(), events.Event{
		Type:      events.TypeTorrentCompleted,
		InfoHash:  "abc123",
		Timestamp: time.Now(),
	})

	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 INSERT, got %d", len(fake.calls))
	}
	payload := fake.calls[0].args[3].([]byte)
	if string(payload) != "{}" {
		t.Errorf("expected empty-map payload \"{}\", got %s", payload)
	}
}

func TestPersister_DBErrorSwallowed(t *testing.T) {
	// Postgres being down must not crash the bus subscriber loop.
	// The error is logged (verified by inspection — the logger is
	// the slog.Discard handler in this test) and HandleEvent
	// returns normally.
	fake := &fakeExecer{err: sql.ErrConnDone}
	p := newPersister(fake)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DB error must not panic: %v", r)
		}
	}()
	p.HandleEvent(context.Background(), events.Event{
		Type:      events.TypeTorrentAdded,
		InfoHash:  "abc123",
		Timestamp: time.Now(),
	})
	if len(fake.calls) != 1 {
		t.Fatalf("expected the INSERT to have been attempted, got %d calls", len(fake.calls))
	}
}
