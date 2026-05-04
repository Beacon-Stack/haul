package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/version"
	"github.com/beacon-stack/pulse/pkg/sdk"
)

// Integration wraps the Pulse SDK client.
type Integration struct {
	Client *sdk.Client
	logger *slog.Logger

	// pulseURL and pulseAPIKey are captured at construction so we can
	// make HTTP calls to Pulse beyond what the pinned SDK version
	// exposes. Specifically: api_key lookup for siblings (added to
	// Pulse's discovery response in this branch but not yet to the
	// SDK's Service struct, so we parse the raw JSON here).
	pulseURL    string
	pulseAPIKey string
}

// PeerService is a sibling Beacon service registered with Pulse,
// including its API key. Only available via DiscoverWithKeys, not the
// SDK's DiscoverAll, since the SDK's Service struct on the pinned
// version doesn't include the key.
type PeerService struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	APIURL string `json:"api_url"`
	APIKey string `json:"api_key"`
}

// DiscoverWithKeys returns siblings registered with Pulse, including
// their API keys. Used by Haul's research-proxy endpoint to
// authenticate server-to-server calls into Pilot/Prism without
// requiring the user to wire per-service credentials.
func (i *Integration) DiscoverWithKeys(ctx context.Context) ([]PeerService, error) {
	if i == nil || i.pulseURL == "" {
		return nil, fmt.Errorf("pulse integration not configured")
	}
	dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, i.pulseURL+"/api/v1/services", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", i.pulseAPIKey)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("pulse %d: %s", resp.StatusCode, string(body))
	}
	var out []PeerService
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode pulse response: %w", err)
	}
	return out, nil
}

// New creates and registers with Pulse using retry/backoff. Returns nil
// (not an error) if Pulse is not configured (empty URL). Returns nil + error
// if Pulse is configured but unreachable after retries — the caller should
// continue in standalone mode.
//
// serviceAPIKey is this Haul instance's own API key. It's sent during
// registration so Pilot/Prism can discover and authenticate with Haul
// via Pulse's service registry.
func New(cfg config.PulseConfig, serverHost string, serverPort int, serviceAPIKey string, logger *slog.Logger) (*Integration, error) {
	if cfg.URL == "" {
		logger.Info("pulse integration disabled (no URL configured)")
		return nil, nil
	}

	apiKey := cfg.APIKey.Value()
	if apiKey == "" {
		discovered := discoverAPIKey(logger)
		if discovered == "" {
			logger.Info("pulse integration disabled — no API key configured and could not auto-discover")
			return nil, nil
		}
		apiKey = discovered
		logger.Info("pulse: auto-discovered API key from local config file")
	}

	apiURL := fmt.Sprintf("http://%s:%d", serverHost, serverPort)
	healthURL := apiURL + "/health"

	if serverHost == "0.0.0.0" || serverHost == "" {
		host := "localhost"
		if h := os.Getenv("ADVERTISE_HOST"); h != "" {
			host = h
		} else if h, err := os.Hostname(); err == nil && h != "" {
			host = h
		}
		apiURL = fmt.Sprintf("http://%s:%d", host, serverPort)
		healthURL = apiURL + "/health"
	}

	client, err := sdk.NewWithRetry(sdk.Config{
		PulseURL:      cfg.URL,
		APIKey:        apiKey,
		ServiceName:   version.AppName,
		ServiceType:   "download-client",
		APIURL:        apiURL,
		HealthURL:     healthURL,
		Version:       version.Version,
		ServiceAPIKey: serviceAPIKey,
		Capabilities: []string{
			"supports_torrent",
			"supports_categories",
			"supports_tags",
		},
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}

	return &Integration{Client: client, logger: logger, pulseURL: cfg.URL, pulseAPIKey: apiKey}, nil
}

// Close stops heartbeats.
func (i *Integration) Close() {
	if i != nil && i.Client != nil {
		i.Client.Close()
	}
}

func discoverAPIKey(logger *slog.Logger) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cfgPath := fmt.Sprintf("%s/.config/pulse/config.yaml", home)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}

	for _, line := range splitLines(string(data)) {
		trimmed := trimLeftSpace(line)
		if len(trimmed) > 8 && trimmed[:8] == "api_key:" {
			value := trimSpace(trimmed[8:])
			value = trimQuotes(value)
			if value != "" && value != "***" {
				return value
			}
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimLeftSpace(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return s[i:]
		}
	}
	return ""
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
