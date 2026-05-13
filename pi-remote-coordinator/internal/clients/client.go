// SPDX-License-Identifier: MIT

// Package clients holds the coordinator-side registry of registered Pi
// remote app clients (SPEC.md § 8.5). Workstream C only seeds a stub
// fixture used by the stub auth path; real /v1/clients/register and the
// X25519 device-binding ship later.
package clients

import "time"

// Client is the coordinator's view of one registered remote app device.
// Workstream C deliberately omits the X25519 pubkey fields — they land
// with the push/registration milestones.
type Client struct {
	ID                string
	DeviceDisplayName string
	LastSeen          time.Time
}
