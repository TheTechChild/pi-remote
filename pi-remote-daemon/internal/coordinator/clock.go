// SPDX-License-Identifier: MIT
package coordinator

import (
	"context"
	"time"
)

// Clock abstracts time.Now and time.Sleep so the reconnect-backoff loop
// is deterministically testable. The production implementation
// (realClock) delegates to the stdlib; the test stub controls advance
// explicitly.
//
// Sleep is context-aware: it returns early with ctx.Err() if the
// context is canceled mid-sleep. Without this, the run loop cannot
// shut down cleanly during a backoff window.
type Clock interface {
	Now() time.Time
	// Sleep blocks until d has passed in clock time or ctx is canceled.
	// Returns nil on full sleep, ctx.Err() on early exit.
	Sleep(ctx context.Context, d time.Duration) error
}

// realClock is the production implementation. Stateless; zero value is
// usable.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// RealClock returns the production Clock implementation.
func RealClock() Clock { return realClock{} }
