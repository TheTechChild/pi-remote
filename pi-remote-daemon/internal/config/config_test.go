// SPDX-License-Identifier: MIT
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points every config/state lookup at empty temp dirs so tests
// never observe the developer's real ~/.config or env.
func isolate(t *testing.T) (cfgDir, stateDir string) {
	t.Helper()
	cfgDir = t.TempDir()
	stateDir = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("PI_REMOTE_STATE_DIR", stateDir)
	for _, v := range []string{
		"PI_REMOTE_MACHINE_ID", "PI_REMOTE_MACHINE_DISPLAY_NAME",
		"PI_REMOTE_COORDINATOR_URL", "PI_REMOTE_SERVICE_TOKEN_ID_FILE",
		"PI_REMOTE_SERVICE_TOKEN_SECRET_FILE", "PI_REMOTE_SOCKET_PATH",
		"PI_REMOTE_TMUX_BINARY", "PI_REMOTE_TMUX_SESSION_PREFIX",
		"PI_REMOTE_LOG_LEVEL", "PI_REMOTE_LOG_FILE",
	} {
		t.Setenv(v, "")
	}
	return cfgDir, stateDir
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

func TestLoadDefaultsAreNonEmpty(t *testing.T) {
	isolate(t)
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
	if cfg.Logging.Level != "info" {
		t.Errorf("expected default log level info, got %q", cfg.Logging.Level)
	}
}

func TestLoadExplicitFileHappyPath(t *testing.T) {
	_, _ = isolate(t)
	p := filepath.Join(t.TempDir(), "daemon.toml")
	writeFile(t, p, `
machine_id = "macbook-pro"
machine_display_name = "MacBook Pro"

[coordinator]
url = "wss://pi-remote.example.com/v1/daemon"
service_token_id_file = "/etc/pi-remote/service_token_id"
service_token_secret_file = "/etc/pi-remote/service_token_secret"

[socket]
path = "/tmp/test-daemon.sock"

[tmux]
binary = "/opt/homebrew/bin/tmux"
session_prefix = "pi-test-"

[logging]
level = "debug"
file = "/tmp/daemon.log"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MachineID != "macbook-pro" || cfg.MachineDisplayName != "MacBook Pro" {
		t.Errorf("identity = %q/%q", cfg.MachineID, cfg.MachineDisplayName)
	}
	if cfg.Coordinator.URL != "wss://pi-remote.example.com/v1/daemon" {
		t.Errorf("coordinator.url = %q", cfg.Coordinator.URL)
	}
	if cfg.Tmux.Binary != "/opt/homebrew/bin/tmux" || cfg.Tmux.SessionPrefix != "pi-test-" {
		t.Errorf("tmux = %+v", cfg.Tmux)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level = %q", cfg.Logging.Level)
	}
}

func TestLoadPartialFileKeepsDefaults(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "daemon.toml")
	writeFile(t, p, `machine_id = "m1"`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tmux.Binary != "tmux" || cfg.Socket.Path != "~/.pi-remote/daemon.sock" {
		t.Errorf("defaults not preserved under partial file: %+v", cfg)
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
	p := filepath.Join(t.TempDir(), "daemon.toml")
	writeFile(t, p, `machine_id = [unterminated`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestLoadUnknownKeysError(t *testing.T) {
	isolate(t)
	p := filepath.Join(t.TempDir(), "daemon.toml")
	writeFile(t, p, "machine_idd = \"typo\"\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "machine_idd") {
		t.Fatalf("expected unknown-key error naming machine_idd, got %v", err)
	}
}

func TestLoadSearchesXDGConfigHome(t *testing.T) {
	cfgDir, _ := isolate(t)
	writeFile(t, filepath.Join(cfgDir, "pi-remote", "daemon.toml"), `machine_id = "from-xdg"`)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MachineID != "from-xdg" {
		t.Errorf("MachineID = %q, want from-xdg", cfg.MachineID)
	}
}

// Precedence: env beats file; flags (applied by main after Load) beat
// env — asserted here by mutating the loaded config the way main's
// applyFlagOverrides does.
func TestPrecedenceFlagBeatsEnvBeatsFile(t *testing.T) {
	cfgDir, _ := isolate(t)
	writeFile(t, filepath.Join(cfgDir, "pi-remote", "daemon.toml"), `machine_id = "from-file"`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MachineID != "from-file" {
		t.Fatalf("file layer: MachineID = %q", cfg.MachineID)
	}

	t.Setenv("PI_REMOTE_MACHINE_ID", "from-env")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MachineID != "from-env" {
		t.Fatalf("env layer: MachineID = %q, want from-env", cfg.MachineID)
	}

	// The flag layer is a post-Load mutation (see cmd applyFlagOverrides).
	cfg.MachineID = "from-flag"
	if err := Finalize(cfg); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.MachineID != "from-flag" {
		t.Errorf("flag layer: MachineID = %q, want from-flag (Finalize must not clobber)", cfg.MachineID)
	}
}

func TestEnvOverridesAllFields(t *testing.T) {
	isolate(t)
	for env, want := range map[string]struct {
		get func(*Config) string
		val string
	}{
		"PI_REMOTE_COORDINATOR_URL":           {func(c *Config) string { return c.Coordinator.URL }, "ws://x"},
		"PI_REMOTE_SERVICE_TOKEN_ID_FILE":     {func(c *Config) string { return c.Coordinator.ServiceTokenIDFile }, "/id"},
		"PI_REMOTE_SERVICE_TOKEN_SECRET_FILE": {func(c *Config) string { return c.Coordinator.ServiceTokenSecretFile }, "/sec"},
		"PI_REMOTE_SOCKET_PATH":               {func(c *Config) string { return c.Socket.Path }, "/tmp/s.sock"},
		"PI_REMOTE_TMUX_BINARY":               {func(c *Config) string { return c.Tmux.Binary }, "/bin/tmux"},
		"PI_REMOTE_TMUX_SESSION_PREFIX":       {func(c *Config) string { return c.Tmux.SessionPrefix }, "px-"},
		"PI_REMOTE_LOG_LEVEL":                 {func(c *Config) string { return c.Logging.Level }, "warn"},
		"PI_REMOTE_LOG_FILE":                  {func(c *Config) string { return c.Logging.File }, "/tmp/l.log"},
		"PI_REMOTE_MACHINE_DISPLAY_NAME":      {func(c *Config) string { return c.MachineDisplayName }, "Box"},
	} {
		t.Setenv(env, want.val)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("%s: Load: %v", env, err)
		}
		if got := want.get(cfg); got != want.val {
			t.Errorf("%s: got %q, want %q", env, got, want.val)
		}
		t.Setenv(env, "")
	}
}

func TestFinalizeGeneratesStableUUIDv7MachineID(t *testing.T) {
	_, stateDir := isolate(t)

	cfg := defaults()
	if err := Finalize(cfg); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	first := cfg.MachineID
	if first == "" {
		t.Fatal("Finalize left MachineID empty")
	}
	// UUIDv7: version nibble '7' at position 14 (8-4-4-4-12 layout).
	if len(first) != 36 || first[14] != '7' {
		t.Errorf("MachineID %q is not a UUIDv7", first)
	}

	// Persisted: a second Finalize (fresh config, same state dir) must
	// observe the identical identity (SPEC § 7.8 stability).
	cfg2 := defaults()
	if err := Finalize(cfg2); err != nil {
		t.Fatalf("second Finalize: %v", err)
	}
	if cfg2.MachineID != first {
		t.Errorf("machine_id unstable across restarts: %q then %q", first, cfg2.MachineID)
	}

	b, err := os.ReadFile(filepath.Join(stateDir, "machine_id"))
	if err != nil {
		t.Fatalf("state file: %v", err)
	}
	if strings.TrimSpace(string(b)) != first {
		t.Errorf("state file %q disagrees with MachineID %q", b, first)
	}
}

func TestFinalizeRespectsConfiguredMachineID(t *testing.T) {
	_, stateDir := isolate(t)
	cfg := defaults()
	cfg.MachineID = "operator-set"
	if err := Finalize(cfg); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.MachineID != "operator-set" {
		t.Errorf("MachineID = %q, want operator-set", cfg.MachineID)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "machine_id")); !os.IsNotExist(err) {
		t.Errorf("state file should not be created when machine_id is configured")
	}
}

func TestFinalizeDefaultsDisplayNameToHostname(t *testing.T) {
	isolate(t)
	cfg := defaults()
	if err := Finalize(cfg); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	host, _ := os.Hostname()
	if cfg.MachineDisplayName != host {
		t.Errorf("MachineDisplayName = %q, want hostname %q", cfg.MachineDisplayName, host)
	}
}

func TestValidateAccumulatesAllErrors(t *testing.T) {
	cfg := defaults()
	cfg.Coordinator.URL = "wss://x" // without token files: two errors
	cfg.Logging.Level = "loud"      // third error
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"service_token_id_file", "service_token_secret_file", "logging.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestValidateCleanConfigPasses(t *testing.T) {
	cfg := defaults()
	if err := Validate(cfg); err != nil {
		t.Errorf("default config should validate: %v", err)
	}
	cfg.Coordinator = CoordinatorConfig{
		URL:                    "wss://x",
		ServiceTokenIDFile:     "/id",
		ServiceTokenSecretFile: "/sec",
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("fully-specified coordinator should validate: %v", err)
	}
}
