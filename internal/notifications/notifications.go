// Package notifications connects Haul to the shared Beacon notifications
// (pulse/pkg/notify): finished and stalled downloads, and health.
package notifications

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/beacon-stack/pulse/pkg/notify"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/events"
)

// Torrents is what the translator needs from Haul's session.
type Torrents interface {
	Get(hash string) (*torrent.Info, error)
}

// Deps is what Setup needs from Haul.
type Deps struct {
	DB       *sql.DB
	Torrents Torrents
	Bus      *events.Bus
	// Hub returns Pulse when Haul is connected to it, nil otherwise.
	Hub func() notify.Hub
	Log *slog.Logger
	// Webhooks from config.yaml (the old way to get events out of Haul);
	// brought over once as "Other app (URL)" channels.
	Webhooks []config.WebhookConfig
}

// Setup opens the store, brings over config-file webhooks (once) and
// starts turning Haul's events into notifications. Run the returned
// outbox's Run in the background.
func Setup(ctx context.Context, d Deps) (*notify.Store, *notify.Outbox, error) {
	store, err := notify.NewStore(ctx, d.DB)
	if err != nil {
		return nil, nil, err
	}
	importWebhooks(ctx, store, d.Webhooks, d.Log)
	out := &notify.Outbox{App: "Haul", Store: store, Hub: d.Hub, Log: d.Log}
	t := &translator{torrents: d.Torrents, out: out}
	d.Bus.Subscribe(t.handle)
	return store, out, nil
}

// Old Haul webhook event names → shared types.
var webhookEvents = map[string]string{
	string(events.TypeTorrentCompleted): notify.TypeDownloaded,
	string(events.TypeTorrentStalled):   notify.TypeDownloadProblem,
}

func importWebhooks(ctx context.Context, store *notify.Store, hooks []config.WebhookConfig, log *slog.Logger) {
	if len(hooks) == 0 {
		return
	}
	old := make([]notify.LegacyChannel, 0, len(hooks))
	for i, h := range hooks {
		// LegacyChannel takes the old names and translates them; keep only
		// ones with a meaning now.
		var evts []string
		for _, e := range h.Events {
			if _, ok := webhookEvents[e]; ok {
				evts = append(evts, e)
			}
		}
		settings := fmt.Sprintf(`{"url":%q}`, h.URL)
		old = append(old, notify.LegacyChannel{Name: fmt.Sprintf("Webhook %d", i+1), Kind: "webhook", Enabled: true, Settings: []byte(settings), Events: translateHaul(evts, len(h.Events) > 0)})
	}
	n, _, err := store.ImportLegacy(ctx, old)
	if err != nil {
		log.Warn("notifications: bringing over config webhooks failed", "error", err)
		return
	}
	if n > 0 {
		log.Info("notifications: config-file webhooks are now notification channels; manage them in Settings → Notifications and remove them from config.yaml", "count", n)
	}
}

// translateHaul maps Haul names to names ImportLegacy understands.
func translateHaul(evts []string, filtered bool) []string {
	var out []string
	for _, e := range evts {
		switch webhookEvents[e] {
		case notify.TypeDownloaded:
			out = append(out, "download_done")
		case notify.TypeDownloadProblem:
			out = append(out, "grab_stalled")
		}
	}
	if filtered && len(out) == 0 {
		return []string{"none"} // filtered to things that no longer exist → defaults
	}
	return out
}

// HealthCheck turns Haul's health report into problems for
// notify.HealthWatcher.
func HealthCheck(report func() *torrent.HealthReport) func(context.Context) []notify.Problem {
	return func(context.Context) []notify.Problem {
		h := report()
		if h == nil {
			return nil
		}
		var out []notify.Problem
		if h.EngineStatus != "" && h.EngineStatus != "healthy" {
			out = append(out, notify.Problem{Key: "engine", Message: "The torrent engine is " + h.EngineStatus, Level: notify.LevelError})
		}
		if !h.VPNActive {
			// Only announced if it changes: someone without a VPN never
			// hears about it; someone whose VPN drops does.
			out = append(out, notify.Problem{Key: "vpn", Message: "The VPN isn't active; downloads may be using your own connection", Level: notify.LevelError})
		}
		if h.DiskTotalBytes > 0 {
			const gb = 1 << 30
			pct := float64(h.DiskFreeBytes) / float64(h.DiskTotalBytes)
			if pct < 0.05 || h.DiskFreeBytes < 10*gb {
				out = append(out, notify.Problem{Key: "disk", Message: fmt.Sprintf("The download disk is nearly full (%.1f GB free)", float64(h.DiskFreeBytes)/gb), Level: notify.LevelWarning})
			}
		}
		return out
	}
}

type publisher interface {
	Publish(notify.Event)
}

type translator struct {
	torrents Torrents
	out      publisher

	mu      sync.Mutex
	stalled map[string]time.Time // hashes already told about
}

// managed reports whether Pilot or Prism added the torrent: they announce
// their own downloads, with better titles, so Haul stays quiet about them.
func (t *translator) managed(hash string) (*torrent.Info, bool) {
	info, err := t.torrents.Get(hash)
	if err != nil || info == nil {
		return nil, false
	}
	r := strings.ToLower(info.Requester)
	return info, r == "pilot" || r == "prism"
}

func (t *translator) firstStall(hash string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stalled == nil {
		t.stalled = map[string]time.Time{}
	}
	now := time.Now()
	for h, at := range t.stalled {
		if now.Sub(at) > 24*time.Hour {
			delete(t.stalled, h)
		}
	}
	if _, seen := t.stalled[hash]; seen {
		return false
	}
	t.stalled[hash] = now
	return true
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

func size(b int64) string {
	const gb = 1 << 30
	if b >= gb {
		return fmt.Sprintf("%.1f GB", float64(b)/gb)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
}

func (t *translator) handle(_ context.Context, e events.Event) {
	if ev, ok := t.translate(e); ok {
		t.out.Publish(ev)
	}
}

func (t *translator) translate(e events.Event) (notify.Event, bool) {
	switch e.Type {
	case events.TypeTorrentCompleted:
		info, managed := t.managed(e.InfoHash)
		if managed {
			return notify.Event{}, false
		}
		name := str(e.Data, "name")
		ev := notify.Event{Type: notify.TypeDownloaded, Level: notify.LevelSuccess, Title: "Downloaded: " + name}
		if info != nil {
			ev.Fields = map[string]string{}
			if info.Size > 0 {
				ev.Fields["Size"] = size(info.Size)
			}
			if info.Category != "" {
				ev.Fields["Category"] = info.Category
			}
		}
		return ev, name != ""

	case events.TypeTorrentStalled:
		if _, managed := t.managed(e.InfoHash); managed || !t.firstStall(e.InfoHash) {
			return notify.Event{}, false
		}
		name := str(e.Data, "name")
		msg := str(e.Data, "reason")
		if secs, ok := e.Data["inactive_secs"].(int64); ok && secs > 0 {
			msg = strings.TrimSpace(fmt.Sprintf("No progress for %d minutes. %s", secs/60, msg))
		}
		return notify.Event{Type: notify.TypeDownloadProblem, Level: notify.LevelWarning, Title: "Download stalled: " + name, Message: msg}, name != ""
	}
	return notify.Event{}, false
}
