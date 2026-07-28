// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/nethardening"
)

func TestSetEgressPolicy_Sets(t *testing.T) {
	// Reset to zero value after the test.
	t.Cleanup(func() { egressPolicy = nethardening.Policy{} })

	p := nethardening.Policy{AllowLoopback: true, AllowPrivate: true}
	SetEgressPolicy(p)

	if egressPolicy != p {
		t.Errorf("SetEgressPolicy did not apply: got %+v, want %+v", egressPolicy, p)
	}
}

func TestNewHTTPClient_Returns(t *testing.T) {
	c := NewHTTPClient(5 * time.Second)
	if c == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	if c.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", c.Timeout)
	}
	if c.Transport == nil {
		t.Error("transport is nil")
	}
}

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	c := NewHTTPClient(0)
	if c == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	if c.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s (default)", c.Timeout)
	}
}

func TestValidateProviderURL_Valid(t *testing.T) {
	if err := ValidateProviderURL("https://example.com"); err != nil {
		t.Errorf("valid URL rejected: %v", err)
	}
}

func TestValidateProviderURL_Invalid(t *testing.T) {
	// ftp scheme with no resolvable host or a deliberately bad URL should be
	// rejected. nethardening.Policy rejects URLs missing a host.
	if err := ValidateProviderURL("ftp://"); err == nil {
		t.Error("expected error for ftp:// with empty host")
	}
}

func TestValidateProviderURL_Empty(t *testing.T) {
	// An empty string is a valid no-op parse (url.Parse("") succeeds),
	// but nethardening rejects it because the host is empty.
	err := ValidateProviderURL("")
	if err == nil {
		t.Error("expected error for empty URL (missing host)")
	}
}
