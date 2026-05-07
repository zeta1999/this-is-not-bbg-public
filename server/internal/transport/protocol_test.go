package transport

import "testing"

// TestVersionMismatch covers the U8 handshake decision matrix.
func TestVersionMismatch(t *testing.T) {
	cases := []struct {
		name             string
		peerMajor        uint32
		peerMinor        uint32
		wantMismatched   bool
		wantAccept       bool
	}{
		{"exact match", ProtocolMajor, ProtocolMinor, false, true},
		{"major higher", ProtocolMajor + 1, ProtocolMinor, true, false},
		{"major lower", 0, ProtocolMinor, true, false},
		{"minor higher", ProtocolMajor, ProtocolMinor + 5, true, true},
		{"minor lower", ProtocolMajor, 0, false, true},
	}
	// The "minor lower" case: ProtocolMinor is 0 today, so 0 ↔ 0
	// is exact-match. Adjust expectation defensively.
	for i := range cases {
		if cases[i].peerMajor == ProtocolMajor && cases[i].peerMinor == ProtocolMinor {
			cases[i].wantMismatched = false
			cases[i].wantAccept = true
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, mismatched, accept := VersionMismatch(tc.peerMajor, tc.peerMinor)
			if mismatched != tc.wantMismatched {
				t.Errorf("mismatched: got %v, want %v (reason=%q)",
					mismatched, tc.wantMismatched, reason)
			}
			if accept != tc.wantAccept {
				t.Errorf("accept: got %v, want %v (reason=%q)",
					accept, tc.wantAccept, reason)
			}
			if mismatched && reason == "" {
				t.Errorf("mismatched=true but reason is empty")
			}
			if !mismatched && reason != "" {
				t.Errorf("matched but reason=%q", reason)
			}
		})
	}
}

// TestUintToStr is a fast sanity check on the tiny formatter helper
// (avoids pulling fmt into the decision-matrix path).
func TestUintToStr(t *testing.T) {
	cases := map[uint32]string{0: "0", 1: "1", 12: "12", 12345: "12345"}
	for in, want := range cases {
		if got := uintToStr(in); got != want {
			t.Errorf("uintToStr(%d) = %q, want %q", in, got, want)
		}
	}
}
