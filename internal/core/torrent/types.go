package torrent

import (
	"time"
)

// Status represents the current state of a torrent.
type Status string

const (
	StatusDownloading Status = "downloading"
	StatusSeeding     Status = "seeding"
	StatusPaused      Status = "paused"
	StatusChecking    Status = "checking"
	StatusQueued      Status = "queued"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
)

// Info is the external representation of a torrent.
type Info struct {
	InfoHash     string     `json:"info_hash"`
	Name         string     `json:"name"`
	Status       Status     `json:"status"`
	SavePath     string     `json:"save_path"`
	Category     string     `json:"category"`
	Tags         []string   `json:"tags"`
	Size         int64      `json:"size"`
	Downloaded   int64      `json:"downloaded"`
	Uploaded     int64      `json:"uploaded"`
	Progress     float64    `json:"progress"`
	DownloadRate int64      `json:"download_rate"`
	UploadRate   int64      `json:"upload_rate"`
	Seeds        int        `json:"seeds"`
	Peers        int        `json:"peers"`
	SeedRatio    float64    `json:"seed_ratio"`
	ETA          int64      `json:"eta"`
	AddedAt      time.Time  `json:"added_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ContentPath  string     `json:"content_path"`
	Requester    string     `json:"requester,omitempty"` // "pilot" | "prism" | "manual" | ""

	// Stalled is true when the backend's stall detector classifies this
	// torrent as inactive — see internal/core/torrent/stall.go for the
	// thresholds. The frontend uses this to render a distinct "Stalled"
	// status badge and color, replacing the old "download_rate == 0"
	// frontend-side heuristic which flipped on every brief connection blip.
	// Always false for non-downloading statuses.
	Stalled bool `json:"stalled"`

	// StalledAt is non-nil when the stall watcher escalated past level 3
	// and auto-paused the torrent. Distinct from Stalled (which is
	// transient and only meaningful while downloading): once StalledAt
	// is set, the torrent is permanently marked as needing user
	// attention until they resume it. Resume clears StalledAt and
	// removes the auto-applied 'stalled' tag.
	StalledAt *time.Time `json:"stalled_at,omitempty"`
}

// PeerInfo is the external representation of a single connected peer.
// Built on demand by Session.Peers — not part of the bulk torrent list to
// keep the hot-path response small.
type PeerInfo struct {
	Addr         string  `json:"addr"`          // "1.2.3.4:54321"
	Client       string  `json:"client"`        // "qBittorrent 4.5.0", "unknown" if the peer hasn't sent a client name
	Network      string  `json:"network"`       // "tcp" or "utp"
	Encrypted    bool    `json:"encrypted"`     // the peer prefers / supports encryption
	Progress     float64 `json:"progress"`      // 0..1 — fraction of pieces the peer has
	DownloadRate int64   `json:"download_rate"` // bytes/sec we're receiving from them
	UploadRate   int64   `json:"upload_rate"`   // bytes/sec we're sending them (best-effort; 0 until anacrolix exposes per-peer upload rate)
	Downloaded   int64   `json:"downloaded"`    // total useful data bytes read from this peer
	Uploaded     int64   `json:"uploaded"`      // total data bytes written to this peer
}

// PieceStateRun is a run-length-encoded entry describing a series of
// consecutive pieces in the same state. Mirrors anacrolix's PieceStateRuns
// output but serialised as plain JSON.
type PieceStateRun struct {
	Length int    `json:"length"`
	State  string `json:"state"` // "complete" | "partial" | "checking" | "missing"
}

// PiecesInfo is a snapshot of a torrent's piece-level state. The UI draws
// it as a piece bar that flashes as pieces arrive.
type PiecesInfo struct {
	NumPieces int             `json:"num_pieces"`
	PieceSize int64           `json:"piece_size"`
	Runs      []PieceStateRun `json:"runs"`
}

// TrackerInfo is a single configured tracker from the torrent's metainfo.
// It carries no live announce state (last announce time, reported
// peers/seeds, errors).
type TrackerInfo struct {
	Tier int    `json:"tier"`
	URL  string `json:"url"`
}

// AddRequest is the input for adding a new torrent.
type AddRequest struct {
	// URI is a magnet link, HTTP URL to a .torrent file, or empty if File is set.
	URI string `json:"uri"`
	// File is raw .torrent file bytes. Mutually exclusive with URI.
	File []byte `json:"-"`
	// Category to assign.
	Category string `json:"category"`
	// SavePath overrides the default download directory.
	SavePath string `json:"save_path"`
	// Tags to assign.
	Tags []string `json:"tags"`
	// Paused starts the torrent in paused state.
	Paused bool `json:"paused"`
	// Metadata holds optional media context from the requesting service (Pilot/Prism).
	Metadata *RequesterMetadata `json:"metadata,omitempty"`
}
