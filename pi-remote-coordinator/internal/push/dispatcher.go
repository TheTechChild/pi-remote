// SPDX-License-Identifier: MIT
package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/TheTechChild/pi-remote-coordinator/internal/clients"
	protopush "github.com/TheTechChild/pi-remote-coordinator/internal/proto/push"
)

// Poster abstracts NtfyPoster for tests.
type Poster interface {
	Post(ctx context.Context, endpoint string, sealed []byte) error
}

// Notification is one push-worthy occurrence, before per-client fan-out.
type Notification struct {
	Reason             string // canonical push reason (prefs.go)
	SessionID          string
	MachineID          string
	MachineDisplayName string
	ProjectName        string
	ProjectDisplayName *string
	Summary            string
	Ts                 int64 // unix millis; 0 = now
}

// Dispatcher implements the push-decision pipeline (SPEC.md §§ 8.7-8.8):
// for each registered client, skip when the reason is filtered by
// preferences (§ 19.6), skip when the client is foreground-attached to
// the session (§ 9.7), then seal the payload to the client's key and
// POST it to the client's UnifiedPush endpoint.
type Dispatcher struct {
	Clients *clients.Registry
	Focus   *FocusTracker
	Keypair *Keypair
	Poster  Poster
	Log     *slog.Logger

	// Timeout bounds each dispatch fan-out. Defaults to 15s.
	Timeout time.Duration
}

// Dispatch fans n out to every eligible registered client. It is
// synchronous; callers on hot paths should invoke it from a goroutine.
func (d *Dispatcher) Dispatch(n Notification) {
	if !ValidReason(n.Reason) {
		d.Log.Warn("push: unknown reason, dropping", "reason", n.Reason)
		return
	}
	if n.Ts == 0 {
		n.Ts = time.Now().UnixMilli()
	}

	timeout := d.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	payload := protopush.PushPayloadJson{
		V:                  1,
		Kind:               protopush.PushPayloadJsonKindNeedsAttention,
		MachineId:          n.MachineID,
		MachineDisplayName: n.MachineDisplayName,
		SessionId:          n.SessionID,
		ProjectName:        n.ProjectName,
		ProjectDisplayName: n.ProjectDisplayName,
		Reason:             protopush.PushPayloadJsonReason(n.Reason),
		Summary:            n.Summary,
		Ts:                 int(n.Ts),
		DeepLink:           "pi-remote://session/" + n.SessionID,
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		d.Log.Error("push: marshal payload", "err", err)
		return
	}

	var zeroKey [32]byte
	for _, c := range d.Clients.List() {
		if c.UnifiedPushEndpoint == "" || c.X25519PubKey == zeroKey {
			continue // not registered for push
		}
		if !ReasonEnabled(c.Preferences, n.Reason) {
			continue // filtered by § 19.6 preferences
		}
		if d.Focus.IsFocused(c.ID, n.SessionID) {
			continue // foreground-attached: suppress (§ 9.7)
		}

		pub := c.X25519PubKey
		sealed, err := d.Keypair.Seal(plaintext, &pub)
		if err != nil {
			d.Log.Error("push: seal", "client_id", c.ID, "err", err)
			continue
		}
		if err := d.Poster.Post(ctx, c.UnifiedPushEndpoint, sealed); err != nil {
			d.Log.Warn("push: post failed", "client_id", c.ID, "err", err)
			continue
		}
		d.Log.Info("push dispatched",
			"client_id", c.ID, "session_id", n.SessionID, "reason", n.Reason)
	}
}
