// SPDX-License-Identifier: MIT

// Package ptymux holds pty-stream transformations applied between tmux
// %output and the coordinator.
package ptymux

// Sanitizer strips OSC 0/1/2 (xterm title-set) escape sequences from a
// pty byte stream per SPEC.md § D8: a malicious process inside a session
// could otherwise spoof the terminal title rendered on attached phones.
// All other bytes — including non-title OSC sequences like OSC 52 — pass
// through unchanged.
//
// The state machine is incremental: sequences split across %output chunk
// boundaries are handled correctly. One Sanitizer per pty stream; not
// safe for concurrent use.
type Sanitizer struct {
	state   sanState
	pending []byte // bytes held while deciding whether a sequence is a title-set
	param   []byte // OSC numeric parameter digits
}

type sanState uint8

const (
	stNormal     sanState = iota
	stEsc                 // saw ESC
	stOscNum              // inside ESC ] collecting numeric param
	stOscNumEsc           // possible ESC \ terminator while collecting param
	stDropTitle           // inside OSC 0/1/2: discard until terminator
	stDropEsc             // possible ESC \ terminator while discarding
	stPassOsc             // inside a non-title OSC: emit until terminator
	stPassOscEsc          // possible ESC \ terminator while emitting
)

const (
	esc = 0x1b
	bel = 0x07
)

// Sanitize returns b with title-set sequences removed. The returned slice
// is freshly allocated only when the stream is mid-sequence or a sequence
// occurs; the common all-clean fast path returns b unchanged.
func (s *Sanitizer) Sanitize(b []byte) []byte {
	// Fast path: nothing pending and no ESC in sight.
	if s.state == stNormal && !containsEsc(b) {
		return b
	}

	out := make([]byte, 0, len(b)+len(s.pending))
	for _, c := range b {
		switch s.state {
		case stNormal:
			if c == esc {
				s.state = stEsc
				s.pending = append(s.pending[:0], c)
			} else {
				out = append(out, c)
			}
		case stEsc:
			switch c {
			case ']':
				s.state = stOscNum
				s.pending = append(s.pending, c)
				s.param = s.param[:0]
			case esc:
				// ESC ESC: emit the first, keep waiting on the second.
				out = append(out, esc)
			default:
				out = append(out, esc, c)
				s.state = stNormal
			}
		case stOscNum:
			switch {
			case c >= '0' && c <= '9' && len(s.param) < 8:
				s.param = append(s.param, c)
				s.pending = append(s.pending, c)
			case c == ';':
				if isTitleParam(s.param) {
					s.state = stDropTitle
					s.pending = s.pending[:0]
				} else {
					out = append(out, s.pending...)
					out = append(out, c)
					s.state = stPassOsc
				}
			case c == bel:
				// Param-only OSC, e.g. ESC ] 0 BEL (degenerate title set).
				if isTitleParam(s.param) {
					s.state = stNormal
					s.pending = s.pending[:0]
				} else {
					out = append(out, s.pending...)
					out = append(out, c)
					s.state = stNormal
				}
			case c == esc:
				s.state = stOscNumEsc
			default:
				// Not a numeric-parameter OSC (e.g. OSC P, OSC 52 with
				// non-digit lead): pass through until its terminator.
				out = append(out, s.pending...)
				out = append(out, c)
				s.state = stPassOsc
			}
		case stOscNumEsc:
			if c == '\\' {
				if isTitleParam(s.param) {
					s.pending = s.pending[:0]
				} else {
					out = append(out, s.pending...)
					out = append(out, esc, '\\')
				}
				s.state = stNormal
			} else {
				out = append(out, s.pending...)
				out = append(out, esc, c)
				s.state = stNormal
			}
		case stDropTitle:
			switch c {
			case bel:
				s.state = stNormal
			case esc:
				s.state = stDropEsc
			} // other bytes: discarded title content
		case stDropEsc:
			if c == '\\' {
				s.state = stNormal
			} else {
				// Stray ESC inside a title: keep discarding.
				s.state = stDropTitle
			}
		case stPassOsc:
			out = append(out, c)
			switch c {
			case bel:
				s.state = stNormal
			case esc:
				s.state = stPassOscEsc
			}
		case stPassOscEsc:
			out = append(out, c)
			if c == '\\' || c != esc {
				s.state = stPassOsc
			}
			if c == '\\' {
				s.state = stNormal
			}
		}
	}
	if s.state == stNormal {
		s.pending = s.pending[:0]
	}
	return out
}

func isTitleParam(p []byte) bool {
	return len(p) == 1 && (p[0] == '0' || p[0] == '1' || p[0] == '2')
}

func containsEsc(b []byte) bool {
	for _, c := range b {
		if c == esc {
			return true
		}
	}
	return false
}
