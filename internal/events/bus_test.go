package events

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestBusPublishNoSubscribers(t *testing.T) {
	bus := New(testLogger())
	// Should not panic with zero subscribers.
	bus.Publish(context.Background(), Event{
		Type: TypeTorrentAdded,
		Data: map[string]any{"name": "test"},
	})
}

func TestBusSubscribeAndReceive(t *testing.T) {
	bus := New(testLogger())

	var received atomic.Value
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(func(_ context.Context, e Event) {
		received.Store(e)
		wg.Done()
	})

	bus.Publish(context.Background(), Event{
		Type:     TypeTorrentCompleted,
		InfoHash: "abc123",
		Data:     map[string]any{"name": "test.mkv"},
	})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	e := received.Load().(Event)
	if e.Type != TypeTorrentCompleted {
		t.Errorf("expected type %s, got %s", TypeTorrentCompleted, e.Type)
	}
	if e.InfoHash != "abc123" {
		t.Errorf("expected info_hash abc123, got %s", e.InfoHash)
	}
}

func TestBusTimestampAutoFill(t *testing.T) {
	bus := New(testLogger())

	var received atomic.Value
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(func(_ context.Context, e Event) {
		received.Store(e)
		wg.Done()
	})

	before := time.Now().UTC()
	bus.Publish(context.Background(), Event{Type: TypeSpeedUpdate})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	e := received.Load().(Event)
	if e.Timestamp.Before(before) {
		t.Error("auto-filled timestamp should be >= before")
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := New(testLogger())

	var count atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		bus.Subscribe(func(_ context.Context, e Event) {
			count.Add(1)
			wg.Done()
		})
	}

	bus.Publish(context.Background(), Event{Type: TypeTorrentAdded})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if count.Load() != 5 {
		t.Errorf("expected 5 handlers called, got %d", count.Load())
	}
}

func TestBusHandlerPanicRecovery(t *testing.T) {
	bus := New(testLogger())

	var wg sync.WaitGroup
	wg.Add(2)

	// First handler panics.
	bus.Subscribe(func(_ context.Context, e Event) {
		defer wg.Done()
		panic("intentional test panic")
	})

	// Second handler should still run.
	var ran atomic.Bool
	bus.Subscribe(func(_ context.Context, e Event) {
		ran.Store(true)
		wg.Done()
	})

	bus.Publish(context.Background(), Event{Type: TypeTorrentFailed})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if !ran.Load() {
		t.Error("second handler should have run despite first panic")
	}
}

func TestEventTypeConstants(t *testing.T) {
	types := []Type{
		TypeTorrentAdded,
		TypeTorrentRemoved,
		TypeTorrentCompleted,
		TypeTorrentFailed,
		TypeTorrentStateChanged,
		TypeTorrentStalled,
		TypeSpeedUpdate,
		TypeHealthUpdate,
	}

	seen := make(map[Type]bool)
	for _, tt := range types {
		if tt == "" {
			t.Error("event type should not be empty")
		}
		if seen[tt] {
			t.Errorf("duplicate event type: %s", tt)
		}
		seen[tt] = true
	}
}
