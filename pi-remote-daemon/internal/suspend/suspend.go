// SPDX-License-Identifier: MIT

// Package suspend delivers OS sleep/wake transitions to the daemon
// (SPEC.md §§ 7.7, 17). Platform implementations:
//
//   - darwin: cgo IOKit IORegisterForSystemPower. The sleep notification
//     is acknowledged only after the onSuspend callback returns, giving
//     the daemon its window to emit machine_suspending and close the
//     coordinator WebSocket before the OS freezes the process (D3).
//   - linux:  org.freedesktop.login1 PrepareForSleep over the system
//     D-Bus (D4).
//   - other:  a no-op watcher that blocks until ctx is canceled.
package suspend

import "context"

// Watcher delivers suspend/resume transitions until ctx is canceled.
// Callbacks run on the watcher's goroutine and should return quickly;
// onSuspend in particular executes inside the OS's pre-sleep grace
// window.
type Watcher interface {
	Watch(ctx context.Context, onSuspend, onResume func()) error
}
