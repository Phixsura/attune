// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// stubProvider is a minimal Provider for registry tests.
type stubProvider struct{ name string }

func (p stubProvider) Provider() string { return p.name }

func (p stubProvider) Check(_ context.Context, _ Connection) (CheckResult, error) {
	return CheckResult{OK: true}, nil
}

func (p stubProvider) ParseWebhook([]byte, map[string]string, []byte) (SyncPayload, error) {
	return SyncPayload{}, nil
}

func (p stubProvider) PullCohort(_ context.Context, _ Connection, _ string) (SyncPayload, error) {
	return SyncPayload{}, nil
}

func TestRegisterLookupAndProvidersAreDeterministic(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	Register("beta", "Beta Provider", func() Provider { return stubProvider{name: "beta"} })
	Register("alpha", "Alpha Provider", func() Provider { return stubProvider{name: "alpha"} })

	entries := Providers()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Provider != "alpha" || entries[1].Provider != "beta" {
		t.Errorf("entries not sorted: %v", entries)
	}

	p, ok := Lookup("alpha")
	if !ok || p.Provider() != "alpha" {
		t.Errorf("Lookup(alpha) = %v, %v", p, ok)
	}

	_, ok = Lookup("missing")
	if ok {
		t.Error("Lookup(missing) should return false")
	}
}

func TestRegisterRejectsInvalidEntries(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	factory := func() Provider { return stubProvider{name: "x"} }

	cases := []struct {
		name    string
		display string
		factory Factory
	}{
		{"UPPER", "Display", factory},
		{"has space", "Display", factory},
		{"valid", "", factory},
		{"valid", "Display", nil},
		{"valid", "Has\ttab", factory},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.display, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic")
				}
			}()
			Register(tc.name, tc.display, tc.factory)
		})
	}
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	factory := func() Provider { return stubProvider{name: "dup"} }
	Register("dup", "Dup", factory)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register("dup", "Dup Again", factory)
}

func TestValidateProviderToken(t *testing.T) {
	if err := ValidateProviderToken("amplitude"); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	if err := ValidateProviderToken("mix-panel_v2"); err != nil {
		t.Errorf("valid token rejected: %v", err)
	}
	if err := ValidateProviderToken("HAS SPACE"); err == nil {
		t.Error("invalid token accepted")
	}
	if err := ValidateProviderToken(""); err == nil {
		t.Error("empty token accepted")
	}
}

func TestUnavailableError(t *testing.T) {
	err := UnavailableError("posthog")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "cohort sync provider unavailable: posthog" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestIsUnavailableError_True(t *testing.T) {
	base := UnavailableError("posthog")
	wrapped := fmt.Errorf("outer: %w", base)

	if !IsUnavailableError(base) {
		t.Error("IsUnavailableError should return true for a direct UnavailableError")
	}
	if !IsUnavailableError(wrapped) {
		t.Error("IsUnavailableError should return true for a wrapped UnavailableError")
	}
}

func TestIsUnavailableError_False(t *testing.T) {
	if IsUnavailableError(nil) {
		t.Error("IsUnavailableError(nil) should return false")
	}
	if IsUnavailableError(errors.New("something else")) {
		t.Error("IsUnavailableError should return false for unrelated errors")
	}
	if IsUnavailableError(fmt.Errorf("wrapped other: %w", errors.New("nope"))) {
		t.Error("IsUnavailableError should return false for a wrapped unrelated error")
	}
}
