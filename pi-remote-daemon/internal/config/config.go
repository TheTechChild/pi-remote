// SPDX-License-Identifier: MIT
package config

// Phase-0 skeleton TOML config loader. Actual TOML parsing lands in M-config
// alongside the other internals; this stub keeps cmd/main compilable.

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

// Load reads the daemon configuration. If path is empty, default search
// locations are tried (per SPEC.md § 7.3). Phase-0 returns a defaulted struct
// so the binary boots without any on-disk config.
func Load(path string) (*Config, error) {
	_ = path // suppressed: real loader implemented in a later milestone.
	return &Config{
		MachineID:          "",
		MachineDisplayName: "",
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
	}, nil
}
