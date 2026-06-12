// SPDX-License-Identifier: MIT
package ptymux

import (
	"bytes"
	"testing"
)

func sanitizeAll(t *testing.T, chunks ...[]byte) []byte {
	t.Helper()
	var s Sanitizer
	var out []byte
	for _, c := range chunks {
		out = append(out, s.Sanitize(c)...)
	}
	return out
}

func TestSanitize_StripsTitleSetBEL(t *testing.T) {
	in := []byte("before\x1b]0;evil title\x07after")
	if got := sanitizeAll(t, in); !bytes.Equal(got, []byte("beforeafter")) {
		t.Errorf("got %q", got)
	}
	// OSC 1 and 2 forms too.
	for _, p := range []string{"1", "2"} {
		in := []byte("a\x1b]" + p + ";t\x07b")
		if got := sanitizeAll(t, in); !bytes.Equal(got, []byte("ab")) {
			t.Errorf("OSC %s: got %q", p, got)
		}
	}
}

func TestSanitize_StripsTitleSetST(t *testing.T) {
	in := []byte("before\x1b]2;evil\x1b\\after")
	if got := sanitizeAll(t, in); !bytes.Equal(got, []byte("beforeafter")) {
		t.Errorf("got %q", got)
	}
}

func TestSanitize_PassThroughCleanBytes(t *testing.T) {
	in := []byte("plain text with colors \x1b[31mred\x1b[0m and unicode ☃")
	if got := sanitizeAll(t, in); !bytes.Equal(got, in) {
		t.Errorf("clean stream mutated: %q", got)
	}
}

func TestSanitize_PassThroughNonTitleOSC(t *testing.T) {
	// OSC 52 (clipboard) must survive; only 0/1/2 are title-sets.
	in := []byte("a\x1b]52;c;aGVsbG8=\x07b")
	if got := sanitizeAll(t, in); !bytes.Equal(got, in) {
		t.Errorf("OSC 52 mutated: %q", got)
	}
	// OSC 10 (two digits, starts with '1') is not a title-set.
	in2 := []byte("a\x1b]10;?\x07b")
	if got := sanitizeAll(t, in2); !bytes.Equal(got, in2) {
		t.Errorf("OSC 10 mutated: %q", got)
	}
}

func TestSanitize_SplitAcrossChunks(t *testing.T) {
	// Title sequence split at every possible boundary.
	full := []byte("AB\x1b]0;sneaky\x07CD")
	want := []byte("ABCD")
	for cut := 1; cut < len(full); cut++ {
		got := sanitizeAll(t, full[:cut], full[cut:])
		if !bytes.Equal(got, want) {
			t.Errorf("cut at %d: got %q", cut, got)
		}
	}
}

func TestSanitize_LoneEscThenText(t *testing.T) {
	in := []byte("x\x1b[1mY")
	if got := sanitizeAll(t, in); !bytes.Equal(got, in) {
		t.Errorf("CSI mutated: %q", got)
	}
}

func BenchmarkSanitize_CleanStream(b *testing.B) {
	var s Sanitizer
	chunk := bytes.Repeat([]byte("the quick brown fox \x1b[32mgreen\x1b[0m jumps\n"), 32)
	b.SetBytes(int64(len(chunk)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Sanitize(chunk)
	}
}
