// SPDX-License-Identifier: MIT

// Package config loads the daemon's on-disk TOML configuration per
// SPEC.md § 7.3.
//
// Precedence (highest wins): CLI flags > PI_REMOTE_* env vars > TOML file
// > built-in defaults. Load applies the file and env layers; the flag
// layer is applied by cmd/pi-remote-daemon after Load returns; Finalize
// and Validate run last so they observe the fully-merged config.
//
// Design decisions (issue #48):
//   - No hot-reload (SIGHUP) in v1.
//   - No ${VAR} interpolation inside the TOML; the env layer covers that
//     use case.
//   - Unknown TOML keys are errors (typo safety beats forward compat for
//     an operator-managed file).
//   - Validation accumulates all problems into one error rather than
//     failing on the first.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/google/uuid"
)

// Config models the on-disk daemon.toml file. See SPEC.md § 7.3.
type Config struct {
	MachineID          string            `toml:"machine_id"`
	MachineDisplayName string            `toml:"machine_display_name"`
	Coordinator        CoordinatorConfig `toml:"coordinator"`
	Socket             SocketConfig      `toml:"socket"`
	Tmux               TmuxConfig        `toml:"tmux"`
	Logging            LoggingConfig     `toml:"logging"`
}

type CoordinatorConfig struct {
	URL                    string `toml:"url"`
	ServiceTokenIDFile     string `toml:"service_token_id_file"`
	ServiceTokenSecretFile string `toml:"service_token_secret_file"`
}

type SocketConfig struct {
	Path string `toml:"path"`
}

type TmuxConfig struct {
	Binary        string `toml:"binary"`
	SessionPrefix string `toml:"session_prefix"`
}

type LoggingConfig struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// defaults returns the built-in configuration: a daemon that listens on
// its Unix socket and mirrors via tmux, with no coordinator link until
// one is configured.
func defaults() *Config {
	return &Config{
		Socket: SocketConfig{
			Path: "~/.pi-remote/daemon.sock",
		},
		Tmux: TmuxConfig{
			Binary:        "tmux",
			SessionPrefix: "pi-remote-",
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// Load reads the daemon configuration. If path is non-empty the named
// file must exist and parse; if path is empty the SPEC § 7.3 locations
// are searched (per-user first, then system-wide) and a fully-default
// config is returned when none exists — local development needs no file.
// PI_REMOTE_* env vars are layered on top of whatever the file produced.
func Load(path string) (*Config, error) {
	cfg := defaults()

	resolved, explicit := path, path != ""
	if !explicit {
		resolved = searchConfigPath()
	}
	if resolved != "" {
		expanded, err := ExpandPath(resolved)
		if err != nil {
			return nil, fmt.Errorf("config path %q: %w", resolved, err)
		}
		md, err := toml.DecodeFile(expanded, cfg)
		if err != nil {
			if explicit || !os.IsNotExist(err) {
				return nil, fmt.Errorf("config %s: %w", expanded, err)
			}
			// A searched (non-explicit) candidate vanished between the
			// stat and the read; treat as absent.
		} else if undecoded := md.Undecoded(); len(undecoded) > 0 {
			keys := make([]string, len(undecoded))
			for i, k := range undecoded {
				keys[i] = k.String()
			}
			return nil, fmt.Errorf("config %s: unknown keys (typo?): %s",
				expanded, strings.Join(keys, ", "))
		}
	}

	applyEnv(cfg, os.Getenv)
	return cfg, nil
}

// searchConfigPath returns the first existing default config location:
// $XDG_CONFIG_HOME/pi-remote/daemon.toml (or ~/.config/...), then
// /etc/pi-remote/daemon.toml. Empty string when none exists.
func searchConfigPath() string {
	var candidates []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		candidates = append(candidates, filepath.Join(xdg, "pi-remote", "daemon.toml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "pi-remote", "daemon.toml"))
	}
	candidates = append(candidates, "/etc/pi-remote/daemon.toml")

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// envVars maps each PI_REMOTE_* variable to the config field it
// overrides. The taxonomy is flat (one var per leaf) and documented in
// SPEC.md § 7.3; adding a var means adding a row there too.
func applyEnv(cfg *Config, getenv func(string) string) {
	for env, dst := range map[string]*string{
		"PI_REMOTE_MACHINE_ID":                &cfg.MachineID,
		"PI_REMOTE_MACHINE_DISPLAY_NAME":      &cfg.MachineDisplayName,
		"PI_REMOTE_COORDINATOR_URL":           &cfg.Coordinator.URL,
		"PI_REMOTE_SERVICE_TOKEN_ID_FILE":     &cfg.Coordinator.ServiceTokenIDFile,
		"PI_REMOTE_SERVICE_TOKEN_SECRET_FILE": &cfg.Coordinator.ServiceTokenSecretFile,
		"PI_REMOTE_SOCKET_PATH":               &cfg.Socket.Path,
		"PI_REMOTE_TMUX_BINARY":               &cfg.Tmux.Binary,
		"PI_REMOTE_TMUX_SESSION_PREFIX":       &cfg.Tmux.SessionPrefix,
		"PI_REMOTE_LOG_LEVEL":                 &cfg.Logging.Level,
		"PI_REMOTE_LOG_FILE":                  &cfg.Logging.File,
	} {
		if v := getenv(env); v != "" {
			*dst = v
		}
	}
}

// Finalize fills the identity fields that have generated/derived
// defaults. Call after every override layer has been applied:
//
//   - MachineID: read from the persisted state file, or first-run
//     generate a UUIDv7 (SPEC § D17) and persist it so the identity is
//     stable across restarts (SPEC § 7.8).
//   - MachineDisplayName: defaults to the OS hostname.
func Finalize(cfg *Config) error {
	if cfg.MachineID == "" {
		id, err := ensureMachineID()
		if err != nil {
			return fmt.Errorf("machine_id: %w", err)
		}
		cfg.MachineID = id
	}
	if cfg.MachineDisplayName == "" {
		host, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("machine_display_name default: %w", err)
		}
		cfg.MachineDisplayName = host
	}
	return nil
}

// Validate checks the fully-merged config, accumulating every problem
// into a single error (friendlier for operators than fail-fast).
func Validate(cfg *Config) error {
	var errs []error

	if cfg.Coordinator.URL != "" {
		if cfg.Coordinator.ServiceTokenIDFile == "" {
			errs = append(errs, errors.New("coordinator.url is set but coordinator.service_token_id_file is empty"))
		}
		if cfg.Coordinator.ServiceTokenSecretFile == "" {
			errs = append(errs, errors.New("coordinator.url is set but coordinator.service_token_secret_file is empty"))
		}
	}
	if cfg.Socket.Path == "" {
		errs = append(errs, errors.New("socket.path must not be empty"))
	}
	if cfg.Tmux.Binary == "" {
		errs = append(errs, errors.New("tmux.binary must not be empty"))
	}
	switch cfg.Logging.Level {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level %q invalid (debug|info|warn|error)", cfg.Logging.Level))
	}

	return errors.Join(errs...)
}

// machineIDPath resolves where the first-run-generated machine_id
// persists (issue #48 design decision: a state file, never a write-back
// into the operator-managed TOML):
//
//   - PI_REMOTE_STATE_DIR/machine_id when set (tests, custom layouts)
//   - /var/lib/pi-remote/machine_id when running as root (system install)
//   - $XDG_STATE_HOME/pi-remote/machine_id, defaulting to
//     ~/.local/state/pi-remote/machine_id (per-user install; same layout
//     on macOS for uniformity)
func machineIDPath() (string, error) {
	if dir := os.Getenv("PI_REMOTE_STATE_DIR"); dir != "" {
		return filepath.Join(dir, "machine_id"), nil
	}
	if os.Geteuid() == 0 {
		return "/var/lib/pi-remote/machine_id", nil
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "pi-remote", "machine_id"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "pi-remote", "machine_id"), nil
}

// ensureMachineID returns the persisted machine_id, generating and
// persisting a UUIDv7 on first run.
func ensureMachineID() (string, error) {
	p, err := machineIDPath()
	if err != nil {
		return "", err
	}

	if b, err := os.ReadFile(p); err == nil {
		id := strings.TrimSpace(string(b))
		if id != "" {
			return id, nil
		}
		// Empty state file: fall through and regenerate.
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", p, err)
	}

	v7, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate UUIDv7: %w", err)
	}
	id := v7.String()

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	// 0644: the machine_id is an identifier, not a secret (the service
	// token files are the credentials, and those are 0600 per D13).
	if err := os.WriteFile(p, []byte(id+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("persist %s: %w", p, err)
	}
	return id, nil
}

// ExpandPath resolves a leading ~ to the user's home directory. Relative
// (non-~) paths and absolute paths pass through.
func ExpandPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:]), nil
	}
	return "", fmt.Errorf("unsupported ~user path: %s", p)
}
