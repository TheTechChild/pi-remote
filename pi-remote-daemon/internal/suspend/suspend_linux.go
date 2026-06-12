// SPDX-License-Identifier: MIT
//go:build linux

package suspend

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

type linuxWatcher struct{}

// New returns the Linux logind watcher (SPEC.md § D4).
func New() Watcher { return linuxWatcher{} }

func (linuxWatcher) Watch(ctx context.Context, onSuspend, onResume func()) error {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("suspend: system bus: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.login1.Manager"),
		dbus.WithMatchMember("PrepareForSleep"),
	); err != nil {
		return fmt.Errorf("suspend: match PrepareForSleep: %w", err)
	}

	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)

	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				return fmt.Errorf("suspend: D-Bus signal channel closed")
			}
			if len(sig.Body) != 1 {
				continue
			}
			sleeping, ok := sig.Body[0].(bool)
			if !ok {
				continue
			}
			if sleeping {
				onSuspend()
			} else {
				onResume()
			}
		}
	}
}
