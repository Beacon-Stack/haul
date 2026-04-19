package config

import (
	"github.com/beacon-stack/haul/internal/version"
)

// Config holds all application configuration.
// Values are loaded from config.yaml and can be overridden by
// HAUL_* environment variables (e.g. HAUL_SERVER_PORT=8484).
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Log      LogConfig      `mapstructure:"log"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Torrent   TorrentConfig       `mapstructure:"torrent"`
	Schedule  SpeedScheduleConfig `mapstructure:"schedule"`
	Webhooks  []WebhookConfig     `mapstructure:"webhooks"`
	Pulse     PulseConfig         `mapstructure:"pulse"`

	// ConfigFile is the path of the config file that was loaded, if any.
	ConfigFile string `mapstructure:"-"`
}

// ServerConfig controls the HTTP server.
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// DatabaseConfig selects and configures the database driver.
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"`
	Path   string `mapstructure:"path"`
	DSN    Secret `mapstructure:"dsn"`
	// PasswordFile is a path to a file containing the Postgres password,
	// typically a Docker secret mounted at /run/secrets/*. When non-empty,
	// its contents replace the password component of DSN at load time.
	PasswordFile string `mapstructure:"password_file"`
}

// LogConfig controls log output format and verbosity.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// AuthConfig holds the API key used to authenticate requests.
type AuthConfig struct {
	APIKey Secret `mapstructure:"api_key"`
}

// TorrentConfig holds torrent engine settings.
type TorrentConfig struct {
	// ListenPort is the port for incoming peer connections. 0 = random.
	ListenPort int `mapstructure:"listen_port"`
	// DownloadDir is the default save path for new torrents.
	DownloadDir string `mapstructure:"download_dir"`
	// DataDir is the persistent directory used for engine-internal state
	// (piece-completion DB, etc). Must survive container restarts — if it
	// doesn't, torrents will restart from 0% on every startup because
	// anacrolix's in-memory completion map will be empty even though the
	// downloaded bytes are still on disk. Defaults to "/config".
	DataDir string `mapstructure:"data_dir"`
	// MaxActiveDownloads is the max concurrent downloading torrents. 0 = unlimited.
	MaxActiveDownloads int `mapstructure:"max_active_downloads"`
	// MaxActiveUploads is the max concurrent seeding torrents. 0 = unlimited.
	MaxActiveUploads int `mapstructure:"max_active_uploads"`
	// MaxActiveTorrents is the combined max concurrent active torrents. 0 = unlimited.
	MaxActiveTorrents int `mapstructure:"max_active_torrents"`
	// GlobalDownloadLimit is the global download speed limit in bytes/s. 0 = unlimited.
	GlobalDownloadLimit int `mapstructure:"global_download_limit"`
	// GlobalUploadLimit is the global upload speed limit in bytes/s. 0 = unlimited.
	GlobalUploadLimit int `mapstructure:"global_upload_limit"`
	// AltDownloadLimit is the alternative (scheduled) download speed limit. 0 = unlimited.
	AltDownloadLimit int `mapstructure:"alt_download_limit"`
	// AltUploadLimit is the alternative (scheduled) upload speed limit. 0 = unlimited.
	AltUploadLimit int `mapstructure:"alt_upload_limit"`
	// DefaultSeedRatio is the default seed ratio limit. 0 = unlimited.
	DefaultSeedRatio float64 `mapstructure:"default_seed_ratio"`
	// DefaultSeedTime is the default seed time limit in seconds. 0 = unlimited.
	DefaultSeedTime int `mapstructure:"default_seed_time"`
	// SeedLimitAction is what to do when seed limit is reached: "pause", "remove", "remove_with_data".
	SeedLimitAction string `mapstructure:"seed_limit_action"`
	// EnableDHT enables the DHT peer discovery network.
	EnableDHT bool `mapstructure:"enable_dht"`
	// EnablePEX enables Peer Exchange.
	EnablePEX bool `mapstructure:"enable_pex"`
	// EnableUTP enables uTP transport.
	EnableUTP bool `mapstructure:"enable_utp"`
	// EnableLSD enables Local Service Discovery for LAN peers.
	EnableLSD bool `mapstructure:"enable_lsd"`
	// Encryption controls protocol encryption. "prefer", "require", or "disable".
	Encryption string `mapstructure:"encryption"`
	// MaxConnections is the global maximum number of peer connections. 0 = unlimited.
	MaxConnections int `mapstructure:"max_connections"`
	// MaxConnectionsPerTorrent is the per-torrent max connections. 0 = unlimited.
	MaxConnectionsPerTorrent int `mapstructure:"max_connections_per_torrent"`
	// NetworkInterface binds peer connections to a specific interface (e.g. "tun0", "wg0").
	NetworkInterface string `mapstructure:"network_interface"`
	// IncompleteFileExtension appends this extension to incomplete files (e.g. ".haul").
	IncompleteFileExtension string `mapstructure:"incomplete_file_ext"`
	// ContentLayout controls file organization: "original", "subfolder", "no_subfolder".
	ContentLayout string `mapstructure:"content_layout"`
	// SlowTorrentThreshold is the download rate (bytes/s) below which a torrent is "slow". Default 2048.
	SlowTorrentThreshold int `mapstructure:"slow_torrent_threshold"`
	// IgnoreSlowTorrents doesn't count slow torrents toward queue limits.
	IgnoreSlowTorrents bool `mapstructure:"ignore_slow_torrents"`
	// StallTimeout is the seconds of no data before a torrent is considered stalled. Default 120.
	StallTimeout int `mapstructure:"stall_timeout"`
	// PauseOnComplete immediately pauses torrents when download finishes. No seeding.
	PauseOnComplete bool `mapstructure:"pause_on_complete"`
	// PreAllocate pre-allocates disk space for new torrents.
	PreAllocate bool `mapstructure:"pre_allocate"`
	// OnAddCommand runs this shell command when a torrent is added.
	// Supports %h (hash), %n (name), %c (category).
	OnAddCommand string `mapstructure:"on_add_command"`
	// OnCompleteCommand runs this shell command when a torrent completes.
	// Supports %h (hash), %n (name), %p (path), %c (category).
	OnCompleteCommand string `mapstructure:"on_complete_command"`
	// AsyncIOThreads is the number of threads for async disk I/O. Default: 10.
	AsyncIOThreads int `mapstructure:"async_io_threads"`
	// FilePoolSize is the max number of simultaneously open files. Default: 100.
	FilePoolSize int `mapstructure:"file_pool_size"`
	// AnnounceToAllTrackers announces to all trackers, not just the first working one. Default: false.
	AnnounceToAllTrackers bool `mapstructure:"announce_to_all_trackers"`
	// RenameOnComplete renames files on download completion when media metadata is available.
	RenameOnComplete bool `mapstructure:"rename_on_complete"`
	// EpisodeFormat is the filename template for TV episodes.
	EpisodeFormat string `mapstructure:"episode_format"`
	// SeriesFolderFormat is the series root folder template.
	SeriesFolderFormat string `mapstructure:"series_folder_format"`
	// SeasonFolderFormat is the season sub-folder template.
	SeasonFolderFormat string `mapstructure:"season_folder_format"`
	// MovieFormat is the filename template for movies.
	MovieFormat string `mapstructure:"movie_format"`
	// MovieFolderFormat is the movie root folder template.
	MovieFolderFormat string `mapstructure:"movie_folder_format"`
	// ColonReplacement controls how colons in titles are handled: "delete", "dash", "space-dash".
	ColonReplacement string `mapstructure:"colon_replacement"`
}

// SpeedScheduleConfig holds alt-speed scheduling settings.
type SpeedScheduleConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	FromHour int    `mapstructure:"from_hour"`
	FromMin  int    `mapstructure:"from_min"`
	ToHour   int    `mapstructure:"to_hour"`
	ToMin    int    `mapstructure:"to_min"`
	Days     string `mapstructure:"days"` // "all", "weekday", "weekend"
}

// WebhookConfig defines an outbound webhook target.
type WebhookConfig struct {
	URL    string   `mapstructure:"url"`
	Events []string `mapstructure:"events"`
}

// PulseConfig holds optional Pulse integration settings.
type PulseConfig struct {
	URL    string `mapstructure:"url"`
	APIKey Secret `mapstructure:"api_key"`
	// APIKeyFile points at a file (typically /run/secrets/*) containing
	// Pulse's API key. When non-empty, its contents replace APIKey at
	// load time.
	APIKeyFile string `mapstructure:"api_key_file"`
}

// AppName returns the application name constant for use in config paths.
func AppName() string {
	return version.AppName
}
