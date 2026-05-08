// SPDX-License-Identifier: MIT
package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/TheTechChild/pi-remote-daemon/internal/config"
)

// Version is set at build time via -ldflags; default value is for local builds.
var Version = "0.0.0-dev"

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to daemon.toml; empty = search default locations")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	slog.Info("pi-remote-daemon",
		"version", Version,
		"machine_id", cfg.MachineID,
		"socket", cfg.Socket.Path,
	)

	// Phase 0: skeleton only. Real responsibilities (Unix socket listener,
	// tmux control mode, coordinator websocket, suspend detection) are
	// implemented in milestones M1-M10.
	os.Exit(0)
}
