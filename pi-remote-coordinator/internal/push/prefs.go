// SPDX-License-Identifier: MIT
package push

// Canonical push reasons (push_payload.json enum) with the SPEC.md § 19.6
// default toggles. machine_suspended exists in the payload enum but has
// no § 19.6 default and is not dispatched in v1.
var defaultReasonEnabled = map[string]bool{
	"agent_idle":          true,
	"extension_dialog":    true,
	"tool_failure":        true,
	"queue_update":        false,
	"compaction_complete": false,
	"extension_error":     false,
	"unresponsive":        true,
	"session_ended":       false,
	"machine_suspended":   false,
}

// ValidReason reports whether reason is in the canonical enum.
func ValidReason(reason string) bool {
	_, ok := defaultReasonEnabled[reason]
	return ok
}

// ReasonEnabled applies a client's preference overrides on top of the
// § 19.6 defaults. prefs may be nil (no overrides stored).
func ReasonEnabled(prefs map[string]bool, reason string) bool {
	if v, ok := prefs[reason]; ok {
		return v
	}
	return defaultReasonEnabled[reason]
}
