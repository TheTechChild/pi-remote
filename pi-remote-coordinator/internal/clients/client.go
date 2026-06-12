// SPDX-License-Identifier: MIT

// Package clients holds the coordinator-side registry of registered Pi
// remote app clients (SPEC.md § 8.5). Workstream C only seeds a stub
// fixture used by the stub auth path; real /v1/clients/register and the
// X25519 device-binding ship later.
package clients

import "time"

// Client is the coordinator's view of one registered remote app device
// (SPEC.md §§ 11.3, 19.2).
type Client struct {
	ID                string
	DeviceDisplayName string
	LastSeen          time.Time

	// UnifiedPushEndpoint is the full per-device push URL (ntfy topic
	// included) supplied at registration. Empty for clients that have
	// not registered for push.
	UnifiedPushEndpoint string

	// X25519PubKey is the device's crypto_box public key; push payloads
	// are sealed to it (SPEC.md § 10.4). Zero-valued when unset.
	X25519PubKey [32]byte

	RegisteredAt time.Time

	// Preferences holds per-reason push toggles (SPEC.md § 19.6) set via
	// POST /v1/clients/<id>/preferences. nil = no overrides; the § 19.6
	// defaults apply (see internal/push.ReasonEnabled).
	Preferences map[string]bool
}
