// SPDX-License-Identifier: MIT
//go:build !linux && (!darwin || !cgo)

package suspend

import "context"

type stubWatcher struct{}

// New returns a no-op watcher on platforms without suspend detection
// (and on darwin built without cgo).
func New() Watcher { return stubWatcher{} }

func (stubWatcher) Watch(ctx context.Context, _, _ func()) error {
	<-ctx.Done()
	return nil
}
