package torrent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

func testWebhookLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestWebhookDispatcherNoWebhooks(t *testing.T) {
	d := NewWebhookDispatcher(nil, testWebhookLogger())
	// Should not panic.
	d.HandleEvent(context.Background(), events.Event{Type: events.TypeTorrentAdded})
}

func TestWebhookDispatcherSendsEvent(t *testing.T) {
	var received atomic.Bool
	var body []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = b
		received.Store(true)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]config.WebhookConfig{
		{URL: srv.URL, Events: []string{"torrent_completed"}},
	}, testWebhookLogger())

	d.HandleEvent(context.Background(), events.Event{
		Type:     events.TypeTorrentCompleted,
		InfoHash: "abc123",
		Data:     map[string]any{"name": "test.mkv"},
	})

	// Wait for async send.
	deadline := time.After(3 * time.Second)
	for !received.Load() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for webhook delivery")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	var e events.Event
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("failed to parse webhook body: %v", err)
	}
	if e.Type != events.TypeTorrentCompleted {
		t.Errorf("expected type %s, got %s", events.TypeTorrentCompleted, e.Type)
	}
	if e.InfoHash != "abc123" {
		t.Errorf("expected info_hash abc123, got %s", e.InfoHash)
	}
}

func TestWebhookDispatcherFiltersEvents(t *testing.T) {
	var hitCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewWebhookDispatcher([]config.WebhookConfig{
		{URL: srv.URL, Events: []string{"torrent_completed"}},
	}, testWebhookLogger())

	// This event should NOT be sent (filtered out).
	d.HandleEvent(context.Background(), events.Event{Type: events.TypeTorrentAdded})

	// This event SHOULD be sent.
	d.HandleEvent(context.Background(), events.Event{Type: events.TypeTorrentCompleted})

	time.Sleep(500 * time.Millisecond)

	if hitCount.Load() != 1 {
		t.Errorf("expected 1 webhook delivery, got %d", hitCount.Load())
	}
}

func TestWebhookDispatcherNoFilterSendsAll(t *testing.T) {
	var hitCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// No Events filter = send all.
	d := NewWebhookDispatcher([]config.WebhookConfig{
		{URL: srv.URL},
	}, testWebhookLogger())

	d.HandleEvent(context.Background(), events.Event{Type: events.TypeTorrentAdded})
	d.HandleEvent(context.Background(), events.Event{Type: events.TypeTorrentCompleted})
	d.HandleEvent(context.Background(), events.Event{Type: events.TypeSpeedUpdate})

	time.Sleep(500 * time.Millisecond)

	if hitCount.Load() != 3 {
		t.Errorf("expected 3 webhook deliveries, got %d", hitCount.Load())
	}
}

func TestShouldSend(t *testing.T) {
	d := NewWebhookDispatcher(nil, testWebhookLogger())

	tests := []struct {
		name   string
		wh     config.WebhookConfig
		event  events.Type
		expect bool
	}{
		{
			name:   "no filter sends all",
			wh:     config.WebhookConfig{URL: "http://example.com"},
			event:  events.TypeTorrentAdded,
			expect: true,
		},
		{
			name:   "matching event",
			wh:     config.WebhookConfig{URL: "http://example.com", Events: []string{"torrent_added"}},
			event:  events.TypeTorrentAdded,
			expect: true,
		},
		{
			name:   "non-matching event",
			wh:     config.WebhookConfig{URL: "http://example.com", Events: []string{"torrent_completed"}},
			event:  events.TypeTorrentAdded,
			expect: false,
		},
		{
			name:   "multiple filters — match",
			wh:     config.WebhookConfig{URL: "http://example.com", Events: []string{"torrent_added", "torrent_completed"}},
			event:  events.TypeTorrentCompleted,
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.shouldSend(tt.wh, tt.event)
			if got != tt.expect {
				t.Errorf("shouldSend(%s, %s) = %v, want %v", tt.wh.Events, tt.event, got, tt.expect)
			}
		})
	}
}
