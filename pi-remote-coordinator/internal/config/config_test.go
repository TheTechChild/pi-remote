// SPDX-License-Identifier: MIT
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolate(t *testing.T) (cfgDir string) {
	t.Helper()
	cfgDir = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	for _, v := range []string{
		"PI_REMOTE_LISTEN", "PI_REMOTE_ACCESS_AUD",
		"PI_REMOTE_SERVICE_TOKEN_AUDIENCE", "PI_REMOTE_NTFY_URL",
		"PI_REMOTE_NTFY_AUTH_TOKEN", "PI_REMOTE_COORDINATOR_KEYPAIR_PATH",
		"PI_REMOTE_TOTAL_CACHE_BYTES", "PI_REMOTE_SESSION_CACHE_FLOOR_BYTES",
	} {
		t.Setenv(v, "")
	}
	return cfgDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaultsMatchSpec(t *testing.T) {
	isolate(t)
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
	if cfg.Broker.SessionCacheFloorBytes != 1_048_576 {
		t.Errorf("session_cache_floor_bytes = %d, want 1048576", cfg.Broker.SessionCacheFloorBytes)
	}
	if cfg.Push.CoordinatorKeypairPath != "/data/coordinator-keypair.box" {
		t.Errorf("keypair path = %q", cfg.Push.CoordinatorKeypairPath)
	}
}

func TestLoadExplicitFileHappyPath(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "coordinator.toml")
	writeFile(t, p, `
[server]
listen = ":9090"

[cloudflare]
access_aud = "aud-clients"
service_token_audience = "aud-daemons"

[ntfy]
url = "http://ntfy:80"
auth_token = "tok"

[broker]
total_cache_bytes = 1048576
session_cache_floor_bytes = 65536

[push]
coordinator_keypair_path = "/data/kp.box"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":9090" || cfg.Cloudflare.AccessAud != "aud-clients" ||
		cfg.Cloudflare.ServiceTokenAudience != "aud-daemons" || cfg.Ntfy.URL != "http://ntfy:80" ||
		cfg.Ntfy.AuthToken != "tok" || cfg.Broker.TotalCacheBytes != 1_048_576 ||
		cfg.Broker.SessionCacheFloorBytes != 65_536 || cfg.Push.CoordinatorKeypairPath != "/data/kp.box" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "coordinator.toml")
	writeFile(t, p, "[server]\nlisten = \":9999\"\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":9999" || cfg.Broker.TotalCacheBytes != 52_428_800 {
		t.Errorf("partial merge wrong: %+v", cfg)
	}
}

func TestLoadExplicitMissingFileErrors(t *testing.T) {
	isolate(t)
	if _, err := Load(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("expected error for explicitly named missing file")
	}
}

func TestLoadMalformedTOMLErrors(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "coordinator.toml")
	writeFile(t, p, "[server\nlisten=")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestLoadUnknownKeysError(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "coordinator.toml")
	writeFile(t, p, "[server]\nlissten = \":1\"\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "lissten") {
		t.Fatalf("expected unknown-key error naming lissten, got %v", err)
	}
}

func TestLoadSearchesXDGConfigHome(t *testing.T) {
	cfgDir := isolate(t)
	writeFile(t, filepath.Join(cfgDir, "pi-remote", "coordinator.toml"), "[server]\nlisten = \":7777\"\n")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":7777" {
		t.Errorf("listen = %q, want :7777", cfg.Server.Listen)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	cfgDir := isolate(t)
	writeFile(t, filepath.Join(cfgDir, "pi-remote", "coordinator.toml"), "[server]\nlisten = \":7777\"\n")
	t.Setenv("PI_REMOTE_LISTEN", ":8888")
	t.Setenv("PI_REMOTE_NTFY_URL", "http://ntfy.env")
	t.Setenv("PI_REMOTE_TOTAL_CACHE_BYTES", "2097152")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Listen != ":8888" || cfg.Ntfy.URL != "http://ntfy.env" ||
		cfg.Broker.TotalCacheBytes != 2_097_152 {
		t.Errorf("env overlay wrong: %+v", cfg)
	}
}

func TestEnvNonIntegerCacheBytesErrors(t *testing.T) {
	isolate(t)
	t.Setenv("PI_REMOTE_TOTAL_CACHE_BYTES", "fifty-megabytes")
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for non-integer env value")
	}
}

func TestValidateAccumulates(t *testing.T) {
	cfg := defaults()
	cfg.Server.Listen = ""
	cfg.Broker.TotalCacheBytes = 0
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"server.listen", "total_cache_bytes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestValidateFloorAboveTotalErrors(t *testing.T) {
	cfg := defaults()
	cfg.Broker.TotalCacheBytes = 100
	cfg.Broker.SessionCacheFloorBytes = 200
	if err := Validate(cfg); err == nil {
		t.Fatal("expected floor>total to fail validation")
	}
}

func TestValidateCFAccess(t *testing.T) {
	cfg := defaults()
	err := ValidateCFAccess(cfg)
	if err == nil {
		t.Fatal("expected empty cloudflare section to fail cfaccess validation")
	}
	for _, want := range []string{"access_aud", "service_token_audience"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	cfg.Cloudflare = CloudflareConfig{AccessAud: "a", ServiceTokenAudience: "b"}
	if err := ValidateCFAccess(cfg); err != nil {
		t.Errorf("populated cloudflare section should pass: %v", err)
	}
}
