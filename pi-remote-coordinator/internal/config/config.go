// SPDX-License-Identifier: MIT
package config

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

// Load reads the coordinator configuration. If path is empty, defaults are
// returned. Phase-0 does not parse on-disk TOML; that lands in a later milestone.
func Load(path string) (*Config, error) {
	_ = path
	return &Config{
		Server: ServerConfig{Listen: ":8080"},
		Broker: BrokerConfig{
			TotalCacheBytes:        52_428_800, // 50MB (SPEC.md § 18.1)
			SessionCacheFloorBytes: 1_048_576,  // 1MB
		},
		Push: PushConfig{
			CoordinatorKeypairPath: "/data/coordinator-keypair.box",
		},
	}, nil
}
