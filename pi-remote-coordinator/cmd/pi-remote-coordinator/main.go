// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/auth"
	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	"github.com/TheTechChild/pi-remote-coordinator/internal/config"
	httpapi "github.com/TheTechChild/pi-remote-coordinator/internal/http"
	"github.com/TheTechChild/pi-remote-coordinator/internal/machines"
	"github.com/TheTechChild/pi-remote-coordinator/internal/sessions"
)

// Version is set at build time via -ldflags; default value is for local builds.
var Version = "0.0.0-dev"

func main() {
	var (
		cfgPath  string
		authMode string
		listen   string
	)
	flag.StringVar(&cfgPath, "config", "", "path to coordinator.toml; empty = use defaults")
	flag.StringVar(&authMode, "auth", "stub", "auth implementation: stub | cfaccess")
	flag.StringVar(&listen, "listen", "", "override listen address (e.g. :8080)")
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
	if listen != "" {
		cfg.Server.Listen = listen
	}

	// Build the auth middleware.
	var mw auth.Middleware
	clientOpts := []clients.Option{}
	switch authMode {
	case "stub":
		mw = auth.NewStub()
		clientOpts = append(clientOpts, clients.WithStubFixture())
	case "cfaccess":
		cf, err := auth.NewCFAccess(cfg.Cloudflare)
		if err != nil {
			slog.Error("cfaccess init failed", "err", err)
			os.Exit(1)
		}
		mw = cf
	default:
		slog.Error("unknown -auth value", "value", authMode)
		os.Exit(1)
	}

	deps := httpapi.Deps{
		Auth:     mw,
		Machines: machines.NewRegistry(),
		Sessions: sessions.NewRegistry(),
		Clients:  clients.NewRegistry(clientOpts...),
		Logger:   logger,
	}
	mux := httpapi.NewMux(deps)
	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		slog.Info("pi-remote-coordinator listening",
			"version", Version, "listen", cfg.Server.Listen, "auth", authMode)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
}
