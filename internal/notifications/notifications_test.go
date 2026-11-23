package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/beacon-stack/pulse/pkg/notify"
	_ "modernc.org/sqlite"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/events"
)

type fakeTorrents map[string]*torrent.Info

func (f fakeTorrents) Get(hash string) (*torrent.Info, error) {
	if i, ok := f[hash]; ok {
		return i, nil
	}
	return nil, errors.New("not found")
}

type recorder struct{ got []notify.Event }

func (r *recorder) Publish(e notify.Event) { r.got = append(r.got, e) }

func TestTranslate_OnlyYourOwnDownloads(t *testing.T) {
	rec := &recorder{}
	tr := &translator{out: rec, torrents: fakeTorrents{
		"mine":  {Name: "Ubuntu 26.04", Size: 6 << 30, Category: "linux", Requester: "manual"},
		"pilot": {Name: "Andor.S02E03", Requester: "pilot"},
		"prism": {Name: "Dune.2024", Requester: "Prism"},
	}}
	ctx := context.Background()
	for _, h := range []string{"mine", "pilot", "prism"} {
		tr.handle(ctx, events.Event{Type: events.TypeTorrentCompleted, InfoHash: h, Data: map[string]any{"name": h}})
	}
	if len(rec.got) != 1 {
		t.Fatalf("Pilot's and Prism's downloads are theirs to announce: %+v", rec.got)
	}
	e := rec.got[0]
	if e.Type != notify.TypeDownloaded || e.Title != "Downloaded: mine" || e.Fields["Size"] != "6.0 GB" || e.Fields["Category"] != "linux" {
		t.Errorf("downloaded %+v", e)
	}

	// A stall is announced once, however many times Haul escalates it.
	rec.got = nil
	for i := 0; i < 3; i++ {
		tr.handle(ctx, events.Event{Type: events.TypeTorrentStalled, InfoHash: "mine", Data: map[string]any{"name": "Ubuntu 26.04", "inactive_secs": int64(1800), "reason": "no peers"}})
	}
	tr.handle(ctx, events.Event{Type: events.TypeTorrentStalled, InfoHash: "pilot", Data: map[string]any{"name": "Andor.S02E03"}})
	if len(rec.got) != 1 || rec.got[0].Title != "Download stalled: Ubuntu 26.04" || rec.got[0].Message != "No progress for 30 minutes. no peers" || rec.got[0].Level != notify.LevelWarning {
		t.Errorf("stalled %+v", rec.got)
	}

	rec.got = nil
	tr.handle(ctx, events.Event{Type: events.TypeTorrentAdded, InfoHash: "mine"})
	tr.handle(ctx, events.Event{Type: events.TypeHealthUpdate})
	if len(rec.got) != 0 {
		t.Errorf("other events produced %+v", rec.got)
	}
}

func TestHealthCheck(t *testing.T) {
	got := HealthCheck(func() *torrent.HealthReport {
		return &torrent.HealthReport{EngineStatus: "healthy", VPNActive: false, DiskTotalBytes: 2000 << 30, DiskFreeBytes: 8 << 30}
	})(context.Background())
	keys := map[string]bool{}
	for _, p := range got {
		keys[p.Key] = true
	}
	if len(got) != 2 || !keys["vpn"] || !keys["disk"] || keys["engine"] {
		t.Errorf("problems %+v", got)
	}
	if got := HealthCheck(func() *torrent.HealthReport { return nil })(context.Background()); got != nil {
		t.Errorf("no report should mean no problems: %+v", got)
	}
}

func TestSetup_BringsOverConfigWebhooks(t *testing.T) {
	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "haul.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, out, err := Setup(ctx, Deps{DB: sqlDB, Torrents: fakeTorrents{}, Bus: events.New(log), Log: log, Webhooks: []config.WebhookConfig{
		{URL: "http://ha.local/api/webhook/haul", Events: []string{"torrent_completed"}},
		{URL: "http://n8n.local/hook"},
		{URL: "http://old.local/x", Events: []string{"torrent_added"}},
	}})
	if err != nil || out == nil {
		t.Fatal(err)
	}
	list, _ := store.List(ctx)
	if len(list) != 3 {
		t.Fatalf("channels %+v", list)
	}
	byURL := map[string]notify.Channel{}
	for _, c := range list {
		var s struct{ URL string }
		_ = jsonUnmarshal(c.Settings, &s)
		byURL[s.URL] = c
	}
	if e := byURL["http://ha.local/api/webhook/haul"].Events; len(e) != 1 || e[0] != notify.TypeDownloaded {
		t.Errorf("completed-only hook: %v", e)
	}
	if e := byURL["http://n8n.local/hook"].Events; len(e) != 0 {
		t.Errorf("unfiltered hook should get everything: %v", e)
	}
	if e := byURL["http://old.local/x"].Events; len(e) == 0 {
		t.Errorf("a hook filtered to events that no longer exist should get the defaults, not everything: %v", e)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
