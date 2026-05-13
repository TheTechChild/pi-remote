// SPDX-License-Identifier: MIT

// Package coordinator implements the daemon's WebSocket client to the
// coordinator endpoint (SPEC § 7.8, § 10.2). It is split into three files:
//
//   - frames.go : typed-frame helpers wrapping the generated wire types.
//                 Pure data, no I/O. Source of truth for the literal
//                 "type" / "v" values the schemas require.
//   - auth.go   : service-token credential loading from D13/D14 file paths.
//                 Pure file I/O, no network.
//   - client.go : dial loop, reconnect, send/recv split. Owns the network.
//
// All wire frames are JSON text frames per SPEC § 10 and the Batch 2 plan.
// Pty bytes ride as base64 inside session_pty.bytes — no binary frames.
package coordinator

import (
	"time"

	pb "github.com/TheTechChild/pi-remote-daemon/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// MachineRegisterInput is the daemon-side description of the machine
// announced to the coordinator on connect. Lifted from config.MachineID /
// config.MachineDisplayName + the daemon's build-time version.
type MachineRegisterInput struct {
	MachineID          string
	MachineDisplayName string
	DaemonVersion      string
	Capabilities       []string // empty -> default {"spawn", "mirror"}
}

// EndedReason is the typed alias for the upstream session_ended.reason
// enum. Re-exported from the generated proto so callers in other packages
// (notably session) can name the value without importing the generated
// package directly.
type EndedReason = pb.SessionEndedJsonReason

// Reason constants. Re-exported for readability; the underlying values are
// the generated proto's enum.
const (
	ReasonProcessExit         EndedReason = pb.SessionEndedJsonReasonProcessExit
	ReasonExtensionDisconnect EndedReason = pb.SessionEndedJsonReasonExtensionDisconnect
	ReasonTmuxServerLost      EndedReason = pb.SessionEndedJsonReasonTmuxServerLost
	ReasonKilled              EndedReason = pb.SessionEndedJsonReasonKilled
	ReasonSpawnFailed         EndedReason = pb.SessionEndedJsonReasonSpawnFailed
)

// defaultCapabilities is what NewMachineRegister applies when the input
// slice is nil or empty. Matches the SPEC § 10.2 worked example.
var defaultCapabilities = []string{"spawn", "mirror"}

// NewMachineRegister builds the first-frame announcement. Per SPEC § 10.2
// this must be the first JSON frame the daemon writes after the WebSocket
// open. Capabilities default to {"spawn", "mirror"} when the input slice
// is empty.
func NewMachineRegister(in MachineRegisterInput) *pb.MachineRegisterJson {
	caps := in.Capabilities
	if len(caps) == 0 {
		caps = defaultCapabilities
	}
	enumCaps := make([]pb.MachineRegisterJsonCapabilitiesElem, len(caps))
	for i, c := range caps {
		enumCaps[i] = pb.MachineRegisterJsonCapabilitiesElem(c)
	}
	return &pb.MachineRegisterJson{
		Type:               "machine_register",
		V:                  1,
		MachineId:          in.MachineID,
		MachineDisplayName: in.MachineDisplayName,
		DaemonVersion:      in.DaemonVersion,
		Capabilities:       enumCaps,
	}
}

// NewSessionStarted projects a session.Session into the upstream
// session_started frame. Spawn token round-trips as a *string (nil when
// the session is user-spawned, not coordinator-spawned).
func NewSessionStarted(s session.Session, machineID, hostnameFallback string) *pb.SessionStartedJson {
	host := s.Hostname
	if host == "" {
		host = hostnameFallback
	}
	md := pb.SessionStartedJsonMetadata{
		Cwd:         s.CWD,
		ProjectName: s.ProjectName,
		Hostname:    host,
		Model:       s.Model,
		StartedAt:   int(s.StartedAt.UnixMilli()),
	}
	if s.SpawnToken != "" {
		tok := s.SpawnToken
		md.SpawnToken = pb.SessionStartedJsonMetadataSpawnToken(&tok)
	}
	return &pb.SessionStartedJson{
		Type:      "session_started",
		V:         1,
		SessionId: s.SessionID,
		MachineId: machineID,
		Metadata:  md,
	}
}

// NewSessionEvent wraps an extension-daemon event (kind + data) as a
// daemon-coordinator session_event with the session-scoped seq and a
// daemon-side timestamp.
func NewSessionEvent(sessionID string, seq uint64, kind string, ts time.Time, data map[string]any) *pb.SessionEventJson {
	if data == nil {
		data = map[string]any{}
	}
	return &pb.SessionEventJson{
		Type:      "session_event",
		V:         1,
		SessionId: sessionID,
		Seq:       int(seq),
		Kind:      pb.SessionEventJsonKind(kind),
		Ts:        int(ts.UnixMilli()),
		Data:      pb.SessionEventJsonData(data),
	}
}

// NewSessionEnded builds the upstream session_ended frame. Reason is the
// typed enum constrained by the schema; callers use the Reason* constants.
func NewSessionEnded(sessionID string, seq uint64, reason EndedReason) *pb.SessionEndedJson {
	return &pb.SessionEndedJson{
		Type:      "session_ended",
		V:         1,
		SessionId: sessionID,
		Seq:       int(seq),
		Reason:    reason,
	}
}

// NewMachineSuspending builds the machine_suspending frame emitted just
// before the daemon's WebSocket close. The trigger is stubbed in this
// batch (M7 lands the OS-level detection); the frame helper exists so the
// coordinator endpoint can accept it on reconnect smoke.
func NewMachineSuspending(ts time.Time) *pb.MachineSuspendingJson {
	return &pb.MachineSuspendingJson{
		Type: "machine_suspending",
		V:    1,
		Ts:   int(ts.UnixMilli()),
	}
}

// NewSessionResume rebuilds the session announcement on coordinator
// reconnect. Per SPEC § 7.8 the daemon emits one per still-live session
// after machine_register, carrying the registry's LastSeq as
// last_seq_emitted.
//
// session_resume's metadata is a free-form map[string]any in the schema
// (unlike session_started's typed struct), so we project the same field
// set explicitly.
func NewSessionResume(s session.Session, machineID, hostnameFallback string, lastSeq uint64) *pb.SessionResumeJson {
	host := s.Hostname
	if host == "" {
		host = hostnameFallback
	}
	md := pb.SessionResumeJsonMetadata{
		"cwd":          s.CWD,
		"project_name": s.ProjectName,
		"hostname":     host,
		"model":        s.Model,
		"started_at":   s.StartedAt.UnixMilli(),
	}
	if s.SpawnToken != "" {
		md["spawn_token"] = s.SpawnToken
	}
	_ = machineID // schema does not carry machine_id on session_resume
	return &pb.SessionResumeJson{
		Type:           "session_resume",
		V:              1,
		SessionId:      s.SessionID,
		LastSeqEmitted: int(lastSeq),
		Metadata:       md,
	}
}
