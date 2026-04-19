package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"

	"github.com/beacon-stack/haul/internal/version"
	"github.com/beacon-stack/pulse/pkg/secretfile"
)

const (
	DefaultHost = "0.0.0.0"
	DefaultPort = 8484
	DefaultDB   = "postgres"
	DefaultLog  = "info"
	DefaultFmt  = "json"
)

// Load reads configuration from a YAML file and environment variables.
// If cfgFile is empty, the following paths are searched in order:
//
//	/config/config.yaml              (Docker volume mount)
//	$HOME/.config/haul/config.yaml
//	/etc/haul/config.yaml
//	./config.yaml
func Load(cfgFile string) (*Config, error) {
	name := version.AppName
	envPrefix := strings.ToUpper(name)

	v := viper.New()

	v.SetDefault("server.host", DefaultHost)
	v.SetDefault("server.port", DefaultPort)
	v.SetDefault("database.driver", DefaultDB)
	v.SetDefault("database.dsn", "")
	v.SetDefault("log.level", DefaultLog)
	v.SetDefault("log.format", DefaultFmt)
	v.SetDefault("torrent.listen_port", 6881)
	v.SetDefault("torrent.enable_dht", true)
	v.SetDefault("torrent.enable_pex", true)
	v.SetDefault("torrent.enable_utp", true)
	v.SetDefault("torrent.enable_lsd", true)
	v.SetDefault("torrent.encryption", "prefer")
	v.SetDefault("torrent.max_connections", 500)
	v.SetDefault("torrent.max_connections_per_torrent", 100)
	v.SetDefault("torrent.content_layout", "original")
	v.SetDefault("torrent.seed_limit_action", "pause")
	v.SetDefault("torrent.slow_torrent_threshold", 2048)
	v.SetDefault("torrent.ignore_slow_torrents", false)
	v.SetDefault("torrent.stall_timeout", 120)
	v.SetDefault("torrent.async_io_threads", 10)
	v.SetDefault("torrent.file_pool_size", 100)
	v.SetDefault("torrent.announce_to_all_trackers", false)
	// torrent.pause_on_complete and torrent.rename_on_complete must be
	// registered via SetDefault so Viper's AutomaticEnv picks up
	// HAUL_TORRENT_PAUSE_ON_COMPLETE / HAUL_TORRENT_RENAME_ON_COMPLETE.
	// Without this, the env vars are silently dropped — exactly the same
	// Viper gotcha that broke HAUL_PULSE_URL earlier. Any new torrent.*
	// boolean that ships via env var must be added here.
	v.SetDefault("torrent.pause_on_complete", false)
	v.SetDefault("torrent.rename_on_complete", false)
	// DataDir must be on a persistent volume — see TorrentConfig.DataDir
	// comment for why. `/config` is the Docker volume mount path.
	v.SetDefault("torrent.data_dir", "/config")
	v.SetDefault("schedule.enabled", false)
	v.SetDefault("schedule.from_hour", 8)
	v.SetDefault("schedule.to_hour", 20)
	v.SetDefault("schedule.days", "all")

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("/config")
		if home != "" {
			v.AddConfigPath(filepath.Join(home, ".config", name))
		}
		v.AddConfigPath("/etc/" + name)
		v.AddConfigPath(".")
	}

	v.SetEnvPrefix(envPrefix)
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	_ = v.BindEnv("auth.api_key", envPrefix+"_AUTH_API_KEY")
	_ = v.BindEnv("database.path", envPrefix+"_DATABASE_PATH")
	_ = v.BindEnv("database.dsn", envPrefix+"_DATABASE_DSN")
	_ = v.BindEnv("database.password_file", envPrefix+"_DATABASE_PASSWORD_FILE")
	_ = v.BindEnv("pulse.url", envPrefix+"_PULSE_URL")
	_ = v.BindEnv("pulse.api_key", envPrefix+"_PULSE_API_KEY")
	_ = v.BindEnv("pulse.api_key_file", envPrefix+"_PULSE_API_KEY_FILE")
	_ = v.BindEnv("torrent.download_dir", envPrefix+"_TORRENT_DOWNLOAD_DIR")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}
	configFileUsed := v.ConfigFileUsed()

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			secretDecodeHook,
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	)); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.Database.PasswordFile != "" {
		merged, err := secretfile.OverrideDSNPassword(cfg.Database.DSN.Value(), cfg.Database.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("applying database password file: %w", err)
		}
		cfg.Database.DSN = Secret(merged)
	}

	if cfg.Pulse.APIKeyFile != "" {
		contents, err := secretfile.Read(cfg.Pulse.APIKeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading Pulse API key file: %w", err)
		}
		cfg.Pulse.APIKey = Secret(contents)
	}

	// Default download directory.
	if cfg.Torrent.DownloadDir == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			cfg.Torrent.DownloadDir = filepath.Join(home, "Downloads")
		} else {
			cfg.Torrent.DownloadDir = "/downloads"
		}
	}

	cfg.ConfigFile = configFileUsed
	return &cfg, nil
}

// EnsureAPIKey generates a random API key if none is configured.
func EnsureAPIKey(cfg *Config) (generated bool, err error) {
	if !cfg.Auth.APIKey.IsEmpty() {
		return false, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return false, fmt.Errorf("generating API key: %w", err)
	}
	cfg.Auth.APIKey = Secret(hex.EncodeToString(b))
	return true, nil
}

func secretDecodeHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if to == reflect.TypeOf(Secret("")) && from.Kind() == reflect.String {
		return Secret(data.(string)), nil
	}
	return data, nil
}
