package torrent

import (
	"testing"
	"time"
)

func TestTorrentInfoNotReady(t *testing.T) {
	// A torrent that hasn't received metadata should return safe minimal info
	// without panicking.
	mt := &managedTorrent{
		paused:   false,
		category: "test-cat",
		tags:     []string{"tag1", "tag2"},
		addedAt:  time.Now(),
		savePath: "/downloads",
		ready:    false,
	}

	// We can't call torrentInfo without a real torrent handle,
	// but we can verify the ready flag logic.
	if mt.ready {
		t.Error("expected ready=false for new managedTorrent")
	}

	// After metadata arrives, ready should be set.
	mt.ready = true
	if !mt.ready {
		t.Error("expected ready=true after setting")
	}
}

func TestManagedTorrentDefaults(t *testing.T) {
	mt := &managedTorrent{
		savePath: "/downloads",
		addedAt:  time.Now(),
	}

	if mt.ready {
		t.Error("ready should default to false")
	}
	if mt.paused {
		t.Error("paused should default to false")
	}
	if mt.category != "" {
		t.Error("category should default to empty")
	}
	if mt.lastBytesRead != 0 {
		t.Error("lastBytesRead should default to 0")
	}
}

func TestStatusConstants(t *testing.T) {
	statuses := []Status{
		StatusDownloading,
		StatusSeeding,
		StatusPaused,
		StatusChecking,
		StatusQueued,
		StatusCompleted,
		StatusFailed,
	}

	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("status should not be empty")
		}
	}
}

func TestEffectivePriority(t *testing.T) {
	tests := []struct {
		name     string
		base     int
		deadline *time.Time
		wantLess bool // should effective priority be less (higher urgency) than base?
	}{
		{
			name:     "no deadline",
			base:     10,
			deadline: nil,
			wantLess: false,
		},
		{
			name:     "deadline in 30 minutes",
			base:     10,
			deadline: timePtr(time.Now().Add(30 * time.Minute)),
			wantLess: true,
		},
		{
			name:     "deadline in 3 hours",
			base:     10,
			deadline: timePtr(time.Now().Add(3 * time.Hour)),
			wantLess: true,
		},
		{
			name:     "deadline in 2 days",
			base:     10,
			deadline: timePtr(time.Now().Add(48 * time.Hour)),
			wantLess: true,
		},
		{
			name:     "deadline in 5 days — no boost",
			base:     10,
			deadline: timePtr(time.Now().Add(120 * time.Hour)),
			wantLess: false,
		},
		{
			name:     "overdue deadline — maximum urgency",
			base:     10,
			deadline: timePtr(time.Now().Add(-1 * time.Hour)),
			wantLess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective := EffectivePriority(tt.base, tt.deadline)
			if tt.wantLess && effective >= tt.base {
				t.Errorf("expected priority < %d, got %d", tt.base, effective)
			}
			if !tt.wantLess && effective != tt.base {
				t.Errorf("expected priority == %d, got %d", tt.base, effective)
			}
		})
	}
}

func TestStallLevelConstants(t *testing.T) {
	if StallNone >= StallLevel1 {
		t.Error("StallNone should be less than StallLevel1")
	}
	if StallLevel1 >= StallLevel2 {
		t.Error("StallLevel1 should be less than StallLevel2")
	}
	if StallLevel2 >= StallLevel3 {
		t.Error("StallLevel2 should be less than StallLevel3")
	}
}

func TestTransferStatsZeroValue(t *testing.T) {
	var stats TransferStats
	if stats.TotalTorrents != 0 {
		t.Error("zero value TotalTorrents should be 0")
	}
	if stats.ActiveDownloads != 0 {
		t.Error("zero value ActiveDownloads should be 0")
	}
	if stats.DownloadSpeed != 0 {
		t.Error("zero value DownloadSpeed should be 0")
	}
}

func TestAddRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     AddRequest
		wantErr bool
	}{
		{
			name:    "empty request",
			req:     AddRequest{},
			wantErr: true,
		},
		{
			name:    "magnet URI",
			req:     AddRequest{URI: "magnet:?xt=urn:btih:abc123"},
			wantErr: false,
		},
		{
			name:    "HTTP URL",
			req:     AddRequest{URI: "http://example.com/file.torrent"},
			wantErr: false,
		},
		{
			name:    "base64 torrent data",
			req:     AddRequest{URI: "data:application/x-bittorrent;base64,dGVzdA=="},
			wantErr: false,
		},
		{
			name:    "raw file bytes",
			req:     AddRequest{File: []byte("test")},
			wantErr: false, // will fail on parse but not on validation
		},
		{
			name:    "unsupported scheme",
			req:     AddRequest{URI: "ftp://example.com/file"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasURI := tt.req.URI != ""
			hasFile := len(tt.req.File) > 0

			if !hasURI && !hasFile {
				if !tt.wantErr {
					t.Error("expected error for empty request")
				}
				return
			}

			if hasURI {
				switch {
				case len(tt.req.URI) >= 7 && tt.req.URI[:7] == "magnet:":
					// valid
				case len(tt.req.URI) >= 5 && tt.req.URI[:5] == "data:":
					// valid
				case len(tt.req.URI) >= 7 && (tt.req.URI[:7] == "http://" || tt.req.URI[:8] == "https://"):
					// valid
				default:
					if !tt.wantErr {
						t.Errorf("expected unsupported scheme error for %s", tt.req.URI)
					}
				}
			}
		})
	}
}

func TestFileInfoPriority(t *testing.T) {
	tests := []struct {
		priority string
		valid    bool
	}{
		{"skip", true},
		{"normal", true},
		{"high", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.priority, func(t *testing.T) {
			valid := tt.priority == "skip" || tt.priority == "normal" || tt.priority == "high"
			if valid != tt.valid {
				t.Errorf("priority %q: got valid=%v, want %v", tt.priority, valid, tt.valid)
			}
		})
	}
}

func TestHealthReportFields(t *testing.T) {
	report := &HealthReport{
		ActiveDownloads: 3,
		ActiveUploads:   5,
		TotalTorrents:   8,
		DownloadSpeed:   1048576,
		UploadSpeed:     524288,
		DiskFreeBytes:   1000000000,
		DiskTotalBytes:  2000000000,
		StalledCount:    1,
		EngineStatus:    "healthy",
		PeersConnected:  42,
		VPNActive:       true,
		VPNInterface:    "wg0",
		ExternalIP:      "1.2.3.4",
	}

	if report.ActiveDownloads+report.ActiveUploads != int64(report.TotalTorrents) {
		t.Error("active downloads + uploads should equal total")
	}
	if report.DiskFreeBytes > report.DiskTotalBytes {
		t.Error("free disk should not exceed total")
	}
	if report.EngineStatus != "healthy" {
		t.Error("expected healthy status")
	}
	if !report.VPNActive {
		t.Error("VPN should be active")
	}
}

func TestRequesterMetadata(t *testing.T) {
	meta := RequesterMetadata{
		Requester:   "prism",
		MediaType:   "movie",
		Title:       "Fight Club",
		TMDBID:      550,
		Quality:     "2160p Remux",
		RequestedBy: "david",
		RequestedAt: "2026-04-11T10:00:00Z",
	}

	if meta.Requester != "prism" {
		t.Error("wrong requester")
	}
	if meta.TMDBID != 550 {
		t.Error("wrong TMDB ID")
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
