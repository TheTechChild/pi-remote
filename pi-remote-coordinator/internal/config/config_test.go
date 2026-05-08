// SPDX-License-Identifier: MIT
package config

import "testing"

func TestLoadDefaultsMatchSpec(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Broker.TotalCacheBytes != 52_428_800 {
		t.Errorf("total_cache_bytes = %d, want 52428800", cfg.Broker.TotalCacheBytes)
	}
}
