// SPDX-License-Identifier: MIT
package config

import "testing"

func TestLoadDefaultsAreNonEmpty(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Socket.Path == "" {
		t.Errorf("expected default socket path, got empty string")
	}
	if cfg.Tmux.Binary != "tmux" {
		t.Errorf("expected default tmux binary 'tmux', got %q", cfg.Tmux.Binary)
	}
}
