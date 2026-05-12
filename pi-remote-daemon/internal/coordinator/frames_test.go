// SPDX-License-Identifier: MIT
package coordinator_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TheTechChild/pi-remote-daemon/internal/coordinator"
	pb "github.com/TheTechChild/pi-remote-daemon/internal/proto/daemon-coordinator"
	"github.com/TheTechChild/pi-remote-daemon/internal/session"
)

// marshalAsMap is a test helper that JSON-encodes a value and decodes it
// into a generic map so tests can assert on the literal wire shape (the
// generated structs have `Type` / `V` typed as interface{}; matching that
// in a typed struct compare is awkward).
func marshalAsMap(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err, "marshal")
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out), "unmarshal")
	return out
}

// F1: TestNewMachineRegister_SetsTypeAndV — generated struct has Type/V
// as interface{}; helper must set them to the schema's const values.
func TestNewMachineRegister_SetsTypeAndV(t *testing.T) {
	in := coordinator.MachineRegisterInput{
		MachineID:          "macbook-pro",
		MachineDisplayName: "MacBook Pro",
		DaemonVersion:      "1.0.0",
		Capabilities:       []string{"spawn", "mirror"},
	}
	frame := coordinator.NewMachineRegister(in)
	require.NotNil(t, frame, "NewMachineRegister must not return nil")

	got := marshalAsMap(t, frame)
	require.Equal(t, "machine_register", got["type"])
	require.EqualValues(t, 1, got["v"])
}

// F2: TestNewMachineRegister_CopiesConfigFields — fields propagate from
// input; capabilities default to {spawn, mirror} when empty.
func TestNewMachineRegister_CopiesConfigFields(t *testing.T) {
	t.Run("explicit capabilities", func(t *testing.T) {
		in := coordinator.MachineRegisterInput{
			MachineID:          "macbook-pro",
			MachineDisplayName: "MacBook Pro",
			DaemonVersion:      "1.0.0",
			Capabilities:       []string{"spawn"},
		}
		frame := coordinator.NewMachineRegister(in)
		require.NotNil(t, frame)
		got := marshalAsMap(t, frame)
		require.Equal(t, "macbook-pro", got["machine_id"])
		require.Equal(t, "MacBook Pro", got["machine_display_name"])
		require.Equal(t, "1.0.0", got["daemon_version"])
		require.Equal(t, []any{"spawn"}, got["capabilities"])
	})

	t.Run("default capabilities when empty", func(t *testing.T) {
		in := coordinator.MachineRegisterInput{
			MachineID:          "macbook-pro",
			MachineDisplayName: "MacBook Pro",
			DaemonVersion:      "1.0.0",
			Capabilities:       nil,
		}
		frame := coordinator.NewMachineRegister(in)
		require.NotNil(t, frame)
		got := marshalAsMap(t, frame)
		require.Equal(t, []any{"spawn", "mirror"}, got["capabilities"])
	})
}

// F3: TestNewSessionStarted_SetsTypeAndV — round-trips cleanly through
// the generated UnmarshalJSON (which enforces schema requireds).
func TestNewSessionStarted_SetsTypeAndV(t *testing.T) {
	sess := session.Session{
		SessionID:   "s-1",
		PID:         12345,
		CWD:         "/home/user/proj",
		ProjectName: "proj",
		Hostname:    "host.local",
		Model:       "anthropic/claude-sonnet-4-20250514",
		StartedAt:   time.UnixMilli(1730000000000),
	}
	frame := coordinator.NewSessionStarted(sess, "macbook-pro", "host.local")
	require.NotNil(t, frame)

	b, err := json.Marshal(frame)
	require.NoError(t, err)

	var rt pb.SessionStartedJson
	require.NoError(t, json.Unmarshal(b, &rt), "must round-trip through generated UnmarshalJSON")

	got := marshalAsMap(t, frame)
	require.Equal(t, "session_started", got["type"])
	require.EqualValues(t, 1, got["v"])
	require.Equal(t, "s-1", got["session_id"])
	require.Equal(t, "macbook-pro", got["machine_id"])
}

// F4: TestNewSessionStarted_PopulatesMetadata — metadata fields lifted
// from the session; spawn_token is nil-pointer (omitted) when empty.
func TestNewSessionStarted_PopulatesMetadata(t *testing.T) {
	t.Run("spawn token absent", func(t *testing.T) {
		sess := session.Session{
			SessionID:   "s-1",
			PID:         12345,
			CWD:         "/home/user/proj",
			ProjectName: "proj",
			Hostname:    "host.local",
			Model:       "anthropic/claude-sonnet-4-20250514",
			StartedAt:   time.UnixMilli(1730000000000),
			SpawnToken:  "",
		}
		frame := coordinator.NewSessionStarted(sess, "macbook-pro", "host.local")
		require.NotNil(t, frame)
		got := marshalAsMap(t, frame)
		md, ok := got["metadata"].(map[string]any)
		require.True(t, ok, "metadata is a JSON object")
		require.Equal(t, "/home/user/proj", md["cwd"])
		require.Equal(t, "proj", md["project_name"])
		require.Equal(t, "host.local", md["hostname"])
		require.Equal(t, "anthropic/claude-sonnet-4-20250514", md["model"])
		require.EqualValues(t, 1730000000000, md["started_at"])
		_, hasSpawn := md["spawn_token"]
		require.False(t, hasSpawn, "empty spawn_token must be omitted from the wire frame")
	})

	t.Run("spawn token present", func(t *testing.T) {
		sess := session.Session{
			SessionID:   "s-1",
			PID:         12345,
			CWD:         "/x",
			ProjectName: "p",
			Hostname:    "h",
			Model:       "m",
			StartedAt:   time.UnixMilli(0),
			SpawnToken:  "abc123",
		}
		frame := coordinator.NewSessionStarted(sess, "macbook-pro", "h")
		require.NotNil(t, frame)
		got := marshalAsMap(t, frame)
		md, ok := got["metadata"].(map[string]any)
		require.True(t, ok, "metadata is a JSON object")
		require.Equal(t, "abc123", md["spawn_token"])
	})
}

// F5: TestNewSessionEvent_SetsTypeAndV — basic envelope check.
func TestNewSessionEvent_SetsTypeAndV(t *testing.T) {
	frame := coordinator.NewSessionEvent(
		"s-1", 42, "agent_start", time.UnixMilli(1730000000123),
		map[string]any{},
	)
	require.NotNil(t, frame)
	got := marshalAsMap(t, frame)
	require.Equal(t, "session_event", got["type"])
	require.EqualValues(t, 1, got["v"])
	require.Equal(t, "s-1", got["session_id"])
	require.EqualValues(t, 42, got["seq"])
	require.Equal(t, "agent_start", got["kind"])
	require.EqualValues(t, 1730000000123, got["ts"])
}

// F6: TestNewSessionEvent_RoundTripsExtensionEvent — preserves kind/data
// for the schema-enum kinds we'll commonly see.
func TestNewSessionEvent_RoundTripsExtensionEvent(t *testing.T) {
	cases := []struct {
		kind string
		data map[string]any
	}{
		{"agent_start", map[string]any{}},
		{"tool_failure", map[string]any{"toolName": "Read", "error": "ENOENT"}},
		{"queue_update", map[string]any{"pending": float64(3)}},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			frame := coordinator.NewSessionEvent("s-1", 1, c.kind, time.UnixMilli(1), c.data)
			require.NotNil(t, frame)
			got := marshalAsMap(t, frame)
			require.Equal(t, c.kind, got["kind"])
			require.Equal(t, c.data, got["data"], "data must round-trip byte-for-byte")
		})
	}
}

// F7: TestNewSessionEnded_SetsReasonExtensionDisconnect — the typed
// constant we use for the ext-side disconnect path (per the acked
// correction to the plan).
func TestNewSessionEnded_SetsReasonExtensionDisconnect(t *testing.T) {
	frame := coordinator.NewSessionEnded("s-1", 7, coordinator.ReasonExtensionDisconnect)
	require.NotNil(t, frame)
	got := marshalAsMap(t, frame)
	require.Equal(t, "session_ended", got["type"])
	require.EqualValues(t, 1, got["v"])
	require.Equal(t, "s-1", got["session_id"])
	require.EqualValues(t, 7, got["seq"])
	require.Equal(t, "extension_disconnect", got["reason"])
}

// F8: TestNewSessionEnded_ConstantsMatchGeneratedEnum — sentinel test
// that fails at compile time if the generated enum changes shape. Belt
// and braces against silent schema drift.
func TestNewSessionEnded_ConstantsMatchGeneratedEnum(t *testing.T) {
	require.Equal(t, pb.SessionEndedJsonReasonProcessExit, coordinator.ReasonProcessExit)
	require.Equal(t, pb.SessionEndedJsonReasonExtensionDisconnect, coordinator.ReasonExtensionDisconnect)
	require.Equal(t, pb.SessionEndedJsonReasonTmuxServerLost, coordinator.ReasonTmuxServerLost)
	require.Equal(t, pb.SessionEndedJsonReasonKilled, coordinator.ReasonKilled)
	require.Equal(t, pb.SessionEndedJsonReasonSpawnFailed, coordinator.ReasonSpawnFailed)
}

// F9: TestNewMachineSuspending_SetsTimestamp — minimal envelope frame.
func TestNewMachineSuspending_SetsTimestamp(t *testing.T) {
	frame := coordinator.NewMachineSuspending(time.UnixMilli(1730000000123))
	require.NotNil(t, frame)
	got := marshalAsMap(t, frame)
	require.Equal(t, "machine_suspending", got["type"])
	require.EqualValues(t, 1, got["v"])
	require.EqualValues(t, 1730000000123, got["ts"])
}

// F10: TestNewSessionResume_SetsLastSeqEmitted — round-trips through the
// generated UnmarshalJSON (which validates last_seq_emitted >= 0) and
// matches the session metadata shape.
func TestNewSessionResume_SetsLastSeqEmitted(t *testing.T) {
	sess := session.Session{
		SessionID:   "s-1",
		PID:         12345,
		CWD:         "/x",
		ProjectName: "p",
		Hostname:    "h",
		Model:       "m",
		StartedAt:   time.UnixMilli(1730000000000),
	}
	frame := coordinator.NewSessionResume(sess, "macbook-pro", "h", 42)
	require.NotNil(t, frame)

	b, err := json.Marshal(frame)
	require.NoError(t, err)

	var rt pb.SessionResumeJson
	require.NoError(t, json.Unmarshal(b, &rt), "must round-trip through generated UnmarshalJSON")

	got := marshalAsMap(t, frame)
	require.Equal(t, "session_resume", got["type"])
	require.EqualValues(t, 1, got["v"])
	require.Equal(t, "s-1", got["session_id"])
	require.EqualValues(t, 42, got["last_seq_emitted"])

	md, ok := got["metadata"].(map[string]any)
	require.True(t, ok, "metadata is a JSON object")
	require.Equal(t, "/x", md["cwd"])
	require.Equal(t, "p", md["project_name"])
}
