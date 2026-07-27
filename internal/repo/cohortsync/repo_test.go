// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestMembershipUpsertEmptySlice(t *testing.T) {
	r := New(nil) // pool not used for empty input
	added, updated, err := r.UpsertMemberships(context.Background(), "t1", uuid.New(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if added != 0 || updated != 0 {
		t.Errorf("got added=%d updated=%d, want 0,0", added, updated)
	}
}

func TestErrorSentinels(t *testing.T) {
	if ErrSourceNotFound == nil || ErrCohortNotFound == nil || ErrRunNotFound == nil || ErrConflict == nil {
		t.Error("sentinel errors must not be nil")
	}
	if ErrSourceNotFound.Error() == "" {
		t.Error("ErrSourceNotFound has empty message")
	}
}

func TestDefaultLimit(t *testing.T) {
	if defaultLimit != 50 {
		t.Errorf("defaultLimit = %d, want 50", defaultLimit)
	}
}
