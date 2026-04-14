package torrent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/events"
)

// WebhookDispatcher sends events to configured webhook URLs.
type WebhookDispatcher struct {
	webhooks []config.WebhookConfig
	http     *http.Client
	logger   *slog.Logger
}

// NewWebhookDispatcher creates a dispatcher for outbound webhooks.
func NewWebhookDispatcher(webhooks []config.WebhookConfig, logger *slog.Logger) *WebhookDispatcher {
	return &WebhookDispatcher{
		webhooks: webhooks,
		http:     &http.Client{Timeout: 10 * time.Second},
		logger:   logger,
	}
}

// HandleEvent implements events.Handler — sends matching events to webhooks.
func (d *WebhookDispatcher) HandleEvent(_ context.Context, e events.Event) {
	if len(d.webhooks) == 0 {
		return
	}

	payload, err := json.Marshal(e)
	if err != nil {
		return
	}

	for _, wh := range d.webhooks {
		if !d.shouldSend(wh, e.Type) {
			continue
		}
		go d.send(wh.URL, payload)
	}
}

func (d *WebhookDispatcher) shouldSend(wh config.WebhookConfig, eventType events.Type) bool {
	if len(wh.Events) == 0 {
		return true // no filter = send all
	}
	for _, e := range wh.Events {
		if e == string(eventType) {
			return true
		}
	}
	return false
}

func (d *WebhookDispatcher) send(url string, payload []byte) {
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := d.http.Post(url, "application/json", bytes.NewReader(payload))
		if err != nil {
			d.logger.Debug("webhook delivery failed", "url", url, "attempt", attempt+1, "error", err)
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
		d.logger.Debug("webhook non-2xx response", "url", url, "status", resp.StatusCode)
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	d.logger.Warn("webhook delivery exhausted retries", "url", url)
}
