// SPDX-License-Identifier: Apache-2.0

package email

import (
	"errors"
	"net"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

// TestIMAPEgressPolicy pins the deliberate IMAP egress policy (the actual
// package var imap.go wires into the dialer): loopback + RFC1918 allowed
// (on-prem co-located IMAP), cloud-metadata / link-local always refused, with
// dial-time rebinding-proof enforcement.
func TestIMAPEgressPolicy(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true},    // cloud metadata (link-local) — always blocked
		{"fe80::1", true},            // link-local v6 — always blocked
		{"127.0.0.1", false},         // loopback — allowed for co-located IMAP
		{"10.0.0.5", false},          // RFC1918 — allowed for on-prem IMAP
		{"64:ff9b::a9fe:a9fe", true}, // NAT64-wrapped metadata — blocked
		{"8.8.8.8", false},           // public — allowed
	}
	for _, c := range cases {
		err := imapEgressPolicy.CheckIP(net.ParseIP(c.ip))
		if (err != nil) != c.blocked {
			t.Fatalf("CheckIP(%s) blocked=%v, want %v (err=%v)", c.ip, err != nil, c.blocked, err)
		}
	}

	// The dialer enforces it at connect time (rebinding-proof).
	if _, err := imapEgressPolicy.Dialer().Dial("tcp", "169.254.169.254:993"); err == nil {
		t.Fatal("dial to metadata IP should be blocked")
	} else {
		var blocked *nethardening.BlockedError
		if !errors.As(err, &blocked) {
			t.Fatalf("expected *BlockedError, got %T: %v", err, err)
		}
	}
}
