// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/TheTechChild/pi-remote-daemon/internal/config"
	"github.com/TheTechChild/pi-remote-daemon/internal/coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
	"github.com/TheTechChild/pi-remote-daemon/internal/socket"
	"github.com/TheTechChild/pi-remote-daemon/internal/tmux"
)

// Version is set at build time via -ldflags; default value is for local builds.
var Version = "0.0.0-dev"

// flagOverrides bundles the override CLI flags: the top of the
// flag > env > file > defaults precedence chain (SPEC § 7.3, #48).
// They let `go run` work against a local coordinator without populating
// /etc/pi-remote/.
type flagOverrides struct {
	coordinatorURL  string
	machineID       string
	tokenIDFile     string
	tokenSecretFile string
}

func main() {
	var (
		cfgPath string
		ovr     flagOverrides
	)
	flag.StringVar(&cfgPath, "config", "", "path to daemon.toml; empty = search default locations")
	flag.StringVar(&ovr.coordinatorURL, "coordinator-url", "", "override coordinator WebSocket URL (e.g. ws://localhost:8080/v1/daemon); dev affordance, see #48")
	flag.StringVar(&ovr.machineID, "machine-id", "", "override machine_id; dev affordance, see #48")
	flag.StringVar(&ovr.tokenIDFile, "service-token-id-file", "", "override path to the CF service-token ID file; dev affordance, see #48")
	flag.StringVar(&ovr.tokenSecretFile, "service-token-secret-file", "", "override path to the CF service-token secret file; dev affordance, see #48")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	applyFlagOverrides(cfg, ovr)
	// Finalize + Validate run after every override layer (flag > env >
	// file > defaults) so first-run machine_id generation and validation
	// observe the merged result.
	if err := config.Finalize(cfg); err != nil {
		slog.Error("config finalize failed", "err", err)
		os.Exit(1)
	}
	if err := config.Validate(cfg); err != nil {
		slog.Error("config invalid", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		slog.Error("daemon exited with error", "err", err)
		os.Exit(1)
	}
}

// applyFlagOverrides mutates cfg in place with any non-empty flag
// values. Empty flag = no override = keep what the loader returned.
// Public only for the integration test (same _test package).
func applyFlagOverrides(cfg *config.Config, ovr flagOverrides) {
	if ovr.coordinatorURL != "" {
		cfg.Coordinator.URL = ovr.coordinatorURL
	}
	if ovr.machineID != "" {
		cfg.MachineID = ovr.machineID
	}
	if ovr.tokenIDFile != "" {
		cfg.Coordinator.ServiceTokenIDFile = ovr.tokenIDFile
	}
	if ovr.tokenSecretFile != "" {
		cfg.Coordinator.ServiceTokenSecretFile = ovr.tokenSecretFile
	}
}

// run owns the daemon's accept loop and shutdown sequence. Extracted from
// main so signal-driven shutdown is testable from a black-box _test package.
func run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	socketPath, err := expandPath(cfg.Socket.Path)
	if err != nil {
		return fmt.Errorf("expand socket path: %w", err)
	}

	registry := session.NewRegistry()
	ln, err := socket.Listen(socketPath)
	if err != nil {
		return err
	}
	log.Info("pi-remote-daemon",
		"version", Version,
		"machine_id", cfg.MachineID,
		"socket", ln.Path(),
	)
	log.Info("socket listening", "path", ln.Path())

	tmuxClient := tmux.NewClient(cfg.Tmux.Binary, cfg.Tmux.SessionPrefix, registry, nil, log)
	if err := tmuxClient.Start(ctx); err != nil {
		return fmt.Errorf("start tmux client: %w", err)
	}
	defer func() { _ = tmuxClient.Close() }()

	handler := socket.NewHandler(registry, tmuxClient, log)
	conns := newConnTracker()

	var wg sync.WaitGroup
	acceptDone := make(chan struct{})
	go acceptLoop(ln, handler, conns, &wg, log, acceptDone)

	// Wire the coordinator client + session multiplex if the daemon is
	// configured to talk to a coordinator. The bootstrap is
	// circular-free thanks to lazy late-binding: the multiplex needs a
	// Coord, the client needs a LiveSnapshot. We construct the client
	// first with a placeholder LiveSnapshot, then construct the
	// multiplex over the client, then patch the placeholder to point at
	// the multiplex's real LiveSessions method.
	coordCtx, coordCancel := context.WithCancel(context.Background())
	defer coordCancel()
	var coordDone chan struct{}
	if cfg.Coordinator.URL != "" {
		var mux *session.Multiplex
		coordCfg := coordinator.Config{
			URL:        cfg.Coordinator.URL,
			IDFile:     cfg.Coordinator.ServiceTokenIDFile,
			SecretFile: cfg.Coordinator.ServiceTokenSecretFile,
			MachineRegister: coordinator.MachineRegisterInput{
				MachineID:          cfg.MachineID,
				MachineDisplayName: cfg.MachineDisplayName,
				DaemonVersion:      Version,
			},
			LiveSnapshot: func() []session.LiveSession {
				if mux == nil {
					return nil
				}
				return mux.LiveSessions()
			},
			Spawner: tmuxClient,
			Clock:   coordinator.RealClock(),
			Logger:  log,
		}
		client := coordinator.NewClient(coordCfg)
		frames := coordinator.FrameBuilder{MachineID: cfg.MachineID}
		mux = session.NewMultiplex(registry, client, frames, cfg.MachineID, nil)
		tmuxClient.SetMultiplex(mux)
		coordDone = make(chan struct{})
		go func() {
			defer close(coordDone)
			_ = client.Run(coordCtx)
		}()
		log.Info("coordinator client started", "url", cfg.Coordinator.URL)
	} else {
		log.Info("coordinator URL not configured; skipping client startup")
	}

	<-ctx.Done()
	log.Info("shutdown signal received, closing listener")

	if cerr := ln.Close(); cerr != nil {
		log.Warn("listener close error", "err", cerr)
	}
	<-acceptDone
	// Close in-flight connections so their Serve goroutines unblock and
	// exit. Without this the wg.Wait below would deadlock waiting for an
	// extension that has nothing more to say.
	conns.closeAll()
	wg.Wait()

	// Stop the coordinator client (if started) and wait for it to
	// finish so its goroutine doesn't leak past process exit.
	coordCancel()
	if coordDone != nil {
		<-coordDone
	}

	log.Info("daemon stopped")
	return nil
}

// acceptLoop runs until ln.Accept returns a non-temporary error (i.e., the
// listener is closed during shutdown).
func acceptLoop(ln *socket.Listener, h *socket.Handler, conns *connTracker, wg *sync.WaitGroup, log *slog.Logger, done chan<- struct{}) {
	defer close(done)
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Warn("accept error", "err", err)
			return
		}
		conns.add(c)
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer conns.remove(c)
			h.Serve(c)
		}(c)
	}
}

// connTracker holds the set of active extension connections so shutdown can
// drop them all at once. Membership is short-lived: Serve removes its own
// entry on return.
type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func newConnTracker() *connTracker {
	return &connTracker{conns: make(map[net.Conn]struct{})}
}

func (t *connTracker) add(c net.Conn) {
	t.mu.Lock()
	t.conns[c] = struct{}{}
	t.mu.Unlock()
}

func (t *connTracker) remove(c net.Conn) {
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

func (t *connTracker) closeAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for c := range t.conns {
		_ = c.Close()
	}
}

// expandPath resolves a leading ~ to the user's home directory. Relative
// (non-~) paths and absolute paths pass through.
func expandPath(p string) (string, error) {
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
