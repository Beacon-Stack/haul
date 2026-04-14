package torrent

import (
	"net/url"
	"strings"
	"testing"
)

func TestDefaultPublicTrackersNotEmpty(t *testing.T) {
	if len(DefaultPublicTrackers) == 0 {
		t.Fatal("DefaultPublicTrackers should not be empty")
	}

	totalTrackers := 0
	for _, tier := range DefaultPublicTrackers {
		if len(tier) == 0 {
			t.Error("tracker tier should not be empty")
		}
		totalTrackers += len(tier)
	}

	if totalTrackers < 5 {
		t.Errorf("expected at least 5 trackers, got %d", totalTrackers)
	}
}

func TestDefaultPublicTrackersValidURLs(t *testing.T) {
	for tierIdx, tier := range DefaultPublicTrackers {
		for _, tracker := range tier {
			u, err := url.Parse(tracker)
			if err != nil {
				t.Errorf("tier %d: invalid URL %q: %v", tierIdx, tracker, err)
				continue
			}

			switch u.Scheme {
			case "http", "https", "udp":
				// valid schemes for trackers
			default:
				t.Errorf("tier %d: unexpected scheme %q in %q", tierIdx, u.Scheme, tracker)
			}

			if u.Host == "" {
				t.Errorf("tier %d: missing host in %q", tierIdx, tracker)
			}

			if !strings.HasSuffix(u.Path, "/announce") {
				t.Errorf("tier %d: tracker URL should end with /announce: %q", tierIdx, tracker)
			}
		}
	}
}

func TestDefaultPublicTrackersNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, tier := range DefaultPublicTrackers {
		for _, tracker := range tier {
			if seen[tracker] {
				t.Errorf("duplicate tracker: %s", tracker)
			}
			seen[tracker] = true
		}
	}
}

func TestDefaultPublicTrackersHasHTTPS(t *testing.T) {
	hasHTTPS := false
	for _, tier := range DefaultPublicTrackers {
		for _, tracker := range tier {
			if strings.HasPrefix(tracker, "https://") {
				hasHTTPS = true
				break
			}
		}
	}
	if !hasHTTPS {
		t.Error("tracker list should include at least one HTTPS tracker for VPN compatibility")
	}
}

func TestDefaultPublicTrackersTieredStructure(t *testing.T) {
	if len(DefaultPublicTrackers) < 2 {
		t.Error("expected at least 2 tracker tiers")
	}

	// First tier should prefer HTTPS for VPN reliability
	firstTier := DefaultPublicTrackers[0]
	for _, tracker := range firstTier {
		if !strings.HasPrefix(tracker, "https://") {
			t.Errorf("first tier should be HTTPS trackers, got: %s", tracker)
		}
	}
}
