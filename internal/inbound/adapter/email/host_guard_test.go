// SPDX-License-Identifier: Apache-2.0

package email

import (
	"errors"
	"testing"
)

func TestValidateOutboundHost(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"empty", "", true},
		// Link-local explicitly (AWS / GCP IMDS sit on 169.254.169.254).
		{"link-local v4", "169.254.169.254", true},
		// IPv6 link-local prefix fe80::/10.
		{"link-local v6", "fe80::1", true},
		// 0.0.0.0 / :: — unspecified.
		{"unspecified v4", "0.0.0.0", true},
		// Loopback + RFC1918 are allowed by design (on-prem IMAP).
		{"loopback v4", "127.0.0.1", false},
		{"loopback v6", "::1", false},
		{"rfc1918 10/8", "10.1.2.3", false},
		{"rfc1918 192.168/16", "192.168.1.1", false},
		// Public IP — fine.
		{"public", "8.8.8.8", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateOutboundHost(tc.host)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateOutboundHost(%q) = nil; want error", tc.host)
				}
				if !errors.Is(err, ErrBlockedHost) {
					t.Errorf("ValidateOutboundHost(%q) err = %v; want errors.Is ErrBlockedHost", tc.host, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateOutboundHost(%q) = %v; want nil", tc.host, err)
			}
		})
	}
}
