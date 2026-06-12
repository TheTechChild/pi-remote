// SPDX-License-Identifier: MIT

// Package config loads the coordinator's on-disk TOML configuration per
// SPEC.md § 8.3.
//
// Precedence (highest wins): CLI flags > PI_REMOTE_* env vars > TOML file
// > built-in defaults. Load applies the file and env layers; the flag
// layer (-listen) is applied by cmd/pi-remote-coordinator after Load.
//
// Same #48 design decisions as the daemon loader: no hot-reload, no
// ${VAR} interpolation, unknown TOML keys are errors, validation
// accumulates.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config models /config/coordinator.toml. See SPEC.md § 8.3.
type Config struct {
	Server     ServerConfig     `toml:"server"`
	Cloudflare CloudflareConfig `toml:"cloudflare"`
	Ntfy       NtfyConfig       `toml:"ntfy"`
	Broker     BrokerConfig     `toml:"broker"`
	Push       PushConfig       `toml:"push"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
}

type CloudflareConfig struct {
	AccessAud            string `toml:"access_aud"`
	ServiceTokenAudience string `toml:"service_token_audience"`
}

type NtfyConfig struct {
	URL       string `toml:"url"`
	AuthToken string `toml:"auth_token"`
}

type BrokerConfig struct {
	TotalCacheBytes        int64 `toml:"total_cache_bytes"`
	SessionCacheFloorBytes int64 `toml:"session_cache_floor_bytes"`
}

type PushConfig struct {
	CoordinatorKeypairPath string `toml:"coordinator_keypair_path"`
}

func defaults() *Config {
	return &Config{
		Server: ServerConfig{Listen: ":8080"},
		Broker: BrokerConfig{
			TotalCacheBytes:        52_428_800, // 50MB (SPEC.md § 18.1)
			SessionCacheFloorBytes: 1_048_576,  // 1MB
		},
		Push: PushConfig{
			CoordinatorKeypairPath: "/data/coordinator-keypair.box",
		},
	}
}

// Load reads the coordinator configuration. If path is non-empty the
// named file must exist and parse; if path is empty the default
// locations are searched — /config/coordinator.toml first (the Docker
// volume layout from SPEC § 8.3), then the same per-user/system pi-remote
// directories the daemon uses. A fully-default config is returned when
// no file exists, so local development needs none. PI_REMOTE_* env vars
// are layered on top.
func Load(path string) (*Config, error) {
	cfg := defaults()

	resolved, explicit := path, path != ""
	if !explicit {
		resolved = searchConfigPath()
	}
	if resolved != "" {
		md, err := toml.DecodeFile(resolved, cfg)
		if err != nil {
			if explicit || !os.IsNotExist(err) {
				return nil, fmt.Errorf("config %s: %w", resolved, err)
			}
		} else if undecoded := md.Undecoded(); len(undecoded) > 0 {
			keys := make([]string, len(undecoded))
			for i, k := range undecoded {
				keys[i] = k.String()
			}
			return nil, fmt.Errorf("config %s: unknown keys (typo?): %s",
				resolved, strings.Join(keys, ", "))
		}
	}

	if err := applyEnv(cfg, os.Getenv); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// searchConfigPath returns the first existing default config location.
func searchConfigPath() string {
	candidates := []string{"/config/coordinator.toml"}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "pi-remote", "coordinator.toml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "pi-remote", "coordinator.toml"))
	}
	candidates = append(candidates, "/etc/pi-remote/coordinator.toml")

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// applyEnv overlays the flat PI_REMOTE_* taxonomy documented in
// SPEC.md § 8.3.
func applyEnv(cfg *Config, getenv func(string) string) error {
	for env, dst := range map[string]*string{
		"PI_REMOTE_LISTEN":                   &cfg.Server.Listen,
		"PI_REMOTE_ACCESS_AUD":               &cfg.Cloudflare.AccessAud,
		"PI_REMOTE_SERVICE_TOKEN_AUDIENCE":   &cfg.Cloudflare.ServiceTokenAudience,
		"PI_REMOTE_NTFY_URL":                 &cfg.Ntfy.URL,
		"PI_REMOTE_NTFY_AUTH_TOKEN":          &cfg.Ntfy.AuthToken,
		"PI_REMOTE_COORDINATOR_KEYPAIR_PATH": &cfg.Push.CoordinatorKeypairPath,
	} {
		if v := getenv(env); v != "" {
			*dst = v
		}
	}

	for env, dst := range map[string]*int64{
		"PI_REMOTE_TOTAL_CACHE_BYTES":         &cfg.Broker.TotalCacheBytes,
		"PI_REMOTE_SESSION_CACHE_FLOOR_BYTES": &cfg.Broker.SessionCacheFloorBytes,
	} {
		if v := getenv(env); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("%s=%q: not an integer: %w", env, v, err)
			}
			*dst = n
		}
	}
	return nil
}

// Validate checks the merged config, accumulating every problem into a
// single error.
func Validate(cfg *Config) error {
	var errs []error

	if cfg.Server.Listen == "" {
		errs = append(errs, errors.New("server.listen must not be empty"))
	}
	if cfg.Broker.TotalCacheBytes <= 0 {
		errs = append(errs, fmt.Errorf("broker.total_cache_bytes = %d, must be > 0", cfg.Broker.TotalCacheBytes))
	}
	if cfg.Broker.SessionCacheFloorBytes <= 0 {
		errs = append(errs, fmt.Errorf("broker.session_cache_floor_bytes = %d, must be > 0", cfg.Broker.SessionCacheFloorBytes))
	}
	if cfg.Broker.SessionCacheFloorBytes > 0 && cfg.Broker.TotalCacheBytes > 0 &&
		cfg.Broker.SessionCacheFloorBytes > cfg.Broker.TotalCacheBytes {
		errs = append(errs, fmt.Errorf("broker.session_cache_floor_bytes (%d) exceeds total_cache_bytes (%d)",
			cfg.Broker.SessionCacheFloorBytes, cfg.Broker.TotalCacheBytes))
	}

	return errors.Join(errs...)
}

// ValidateCFAccess checks the fields the real CF Access middleware needs.
// Called by main only when -auth=cfaccess: the stub path must keep
// working with an empty [cloudflare] section.
func ValidateCFAccess(cfg *Config) error {
	var errs []error
	if cfg.Cloudflare.AccessAud == "" {
		errs = append(errs, errors.New("cloudflare.access_aud required for -auth=cfaccess"))
	}
	if cfg.Cloudflare.ServiceTokenAudience == "" {
		errs = append(errs, errors.New("cloudflare.service_token_audience required for -auth=cfaccess"))
	}
	return errors.Join(errs...)
}
