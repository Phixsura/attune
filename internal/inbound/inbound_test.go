// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
)

// Compile-time contract sanity. The framework root must keep these five
// interfaces stable; breaking them is the contract change that triggers a
// repo-wide audit.

type stubAdapter struct{ ch string }

func (s *stubAdapter) Channel() string                               { return s.ch }
func (s *stubAdapter) Start(_ context.Context, _ inbound.Deps) error { return nil }
func (s *stubAdapter) Shutdown(_ context.Context) error              { return nil }

type stubWithTimeout struct{ stubAdapter }

func (stubWithTimeout) ShutdownTimeout() time.Duration { return 3 * time.Second }

func TestAdapterInterface_Compiles(t *testing.T) {
	var _ inbound.Adapter = (*stubAdapter)(nil)
	var _ inbound.Adapter = (*stubWithTimeout)(nil)
	var _ inbound.ShutdownTimeouter = (*stubWithTimeout)(nil)
}

func TestIngestPortInterface_Compiles(t *testing.T) {
	var _ inbound.IngestPort = inbound.IngestFunc(func(_ context.Context, _ string, _ uuid.UUID, _ domain.IngestInput) (int64, error) {
		return 0, nil
	})
}

func TestIngestFunc_DelegatesCorrectly(t *testing.T) {
	t.Parallel()
	var calledTenant string
	var calledKeyID uuid.UUID
	var calledInput domain.IngestInput

	f := inbound.IngestFunc(func(_ context.Context, tenant string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
		calledTenant = tenant
		calledKeyID = keyID
		calledInput = in
		return 42, nil
	})

	in := domain.IngestInput{Source: "test", Content: "body"}
	testKeyID := uuid.New()
	id, err := f.Ingest(context.Background(), "demo", testKeyID, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Errorf("got id %d, want 42", id)
	}
	if calledTenant != "demo" {
		t.Errorf("tenant = %q, want %q", calledTenant, "demo")
	}
	if calledKeyID != testKeyID {
		t.Errorf("keyID = %v, want %v", calledKeyID, testKeyID)
	}
	if calledInput.Source != "test" {
		t.Errorf("input.Source = %q, want %q", calledInput.Source, "test")
	}
}
