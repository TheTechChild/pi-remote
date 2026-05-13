// SPDX-License-Identifier: MIT
package coordinator

import "time"

// Clock abstracts time.Now and time.Sleep so the reconnect-backoff loop
// is deterministically testable. The production implementation
// (realClock) delegates to the stdlib; the test stub controls advance
// explicitly.
//
// Pattern: the multiplex needs a "wall clock" (just Now); the client
// needs a "wall clock plus sleep" because backoff requires sleep. Two
// interfaces keep each consumer's needs honest.
type Clock interface {
	Now() time.Time
	// Sleep blocks until the duration has passed in clock time. The
	// real impl is time.Sleep; the fake impl notifies the test that a
	// sleep was requested and waits for the test to advance.
	Sleep(d time.Duration)
}

// realClock is the production implementation. Stateless; zero value is
// usable.
type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// RealClock returns the production Clock implementation.
func RealClock() Clock { return realClock{} }
