package torrent

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/beacon-stack/haul/internal/events"
)

// HealthReport is the structured health data for Pulse dashboard.
type HealthReport struct {
	ActiveDownloads int64  `json:"active_downloads"`
	ActiveUploads   int64  `json:"active_uploads"`
	TotalTorrents   int    `json:"total_torrents"`
	DownloadSpeed   int64  `json:"download_speed_bps"`
	UploadSpeed     int64  `json:"upload_speed_bps"`
	DiskFreeBytes   int64  `json:"disk_free_bytes"`
	DiskTotalBytes  int64  `json:"disk_total_bytes"`
	StalledCount    int    `json:"stalled_count"`
	EngineStatus    string `json:"engine_status"`
	PeersConnected  int    `json:"peers_connected"`
	VPNActive       bool   `json:"vpn_active"`
	VPNInterface    string `json:"vpn_interface,omitempty"`
	ExternalIP      string `json:"external_ip,omitempty"`
}

// ── VPN Detection ────────────────────────────────────────────────────────────

var (
	vpnActive  bool
	vpnIface   string
	externalIP string
	vpnCheckMu sync.RWMutex
)

// CheckVPN detects VPN status by looking for tunnel interfaces and checking
// the external IP. Call periodically (every 60s).
func CheckVPN() {
	iface, found := detectTunnelInterface()

	ip := fetchExternalIP()

	vpnCheckMu.Lock()
	vpnActive = found
	vpnIface = iface
	externalIP = ip
	vpnCheckMu.Unlock()
}

// GetVPNStatus returns the cached VPN status.
func GetVPNStatus() (active bool, iface, ip string) {
	vpnCheckMu.RLock()
	defer vpnCheckMu.RUnlock()
	return vpnActive, vpnIface, externalIP
}

// detectTunnelInterface checks for common VPN tunnel interfaces.
func detectTunnelInterface() (string, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", false
	}

	tunnelPrefixes := []string{"tun", "wg", "tap", "ppp", "pia"}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		for _, prefix := range tunnelPrefixes {
			if strings.HasPrefix(name, prefix) {
				return iface.Name, true
			}
		}
	}
	return "", false
}

// fetchExternalIP gets the public IP from a fast lookup service.
func fetchExternalIP() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

// GetHealth returns a structured health report.
func (s *Session) GetHealth() *HealthReport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var activeDown, activeUp int64
	var totalDownSpeed, totalUpSpeed int64
	var stalledCount int
	var totalPeers int

	for _, mt := range s.torrents {
		if mt.paused || !mt.ready.Load() {
			continue
		}
		stats := mt.t.Stats()
		totalPeers += stats.ActivePeers

		if mt.t.BytesMissing() > 0 {
			activeDown++
			totalDownSpeed += stats.ConnStats.BytesReadData.Int64()
			// Stall classification lives in internal/core/torrent/stall.go.
		} else {
			activeUp++
			totalUpSpeed += stats.ConnStats.BytesWrittenData.Int64()
		}
	}

	// Disk space for download directory.
	diskFree, diskTotal := getDiskSpace(s.cfg.DownloadDir)

	return &HealthReport{
		ActiveDownloads: activeDown,
		ActiveUploads:   activeUp,
		TotalTorrents:   len(s.torrents),
		DownloadSpeed:   totalDownSpeed,
		UploadSpeed:     totalUpSpeed,
		DiskFreeBytes:   diskFree,
		DiskTotalBytes:  diskTotal,
		StalledCount:    stalledCount,
		EngineStatus:    "healthy",
		PeersConnected:  totalPeers,
		VPNActive:       func() bool { a, _, _ := GetVPNStatus(); return a }(),
		VPNInterface:    func() string { _, i, _ := GetVPNStatus(); return i }(),
		ExternalIP:      func() string { _, _, ip := GetVPNStatus(); return ip }(),
	}
}

// PublishHealth publishes a health update event. Call periodically.
func (s *Session) PublishHealth(ctx context.Context) {
	health := s.GetHealth()
	s.bus.Publish(ctx, events.Event{
		Type: events.TypeHealthUpdate,
		Data: map[string]any{
			"active_downloads": health.ActiveDownloads,
			"active_uploads":   health.ActiveUploads,
			"total_torrents":   health.TotalTorrents,
			"download_speed":   health.DownloadSpeed,
			"upload_speed":     health.UploadSpeed,
			"disk_free_bytes":  health.DiskFreeBytes,
			"disk_total_bytes": health.DiskTotalBytes,
			"stalled_count":    health.StalledCount,
			"peers_connected":  health.PeersConnected,
			"engine_status":    health.EngineStatus,
		},
	})
}

func getDiskSpace(path string) (free, total int64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	free = int64(stat.Bavail) * int64(stat.Bsize)
	total = int64(stat.Blocks) * int64(stat.Bsize)
	return
}
