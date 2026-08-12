// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
)

var errStore = errors.New("store down")

// downStore fails every read/write to exercise the 500 branches.
type downStore struct{}

func (downStore) ListEventsBySliceType(context.Context, string, string, string, int) ([]anomalyrepo.Event, error) {
	return nil, errStore
}

func (downStore) FilterLiveFeedbackIDs(context.Context, string, []int64) ([]int64, error) {
	return nil, errStore
}

func (downStore) CountRecentSliceKeys(context.Context, string, int) (int, error) {
	return 0, errStore
}

func (downStore) GetEvent(context.Context, string, uuid.UUID) (*anomalyrepo.Event, error) {
	return nil, errStore
}

func (downStore) BaselineCounts(context.Context, string, string, string, []time.Time) ([]int64, error) {
	return nil, errStore
}

func (downStore) CountOn(context.Context, string, string, string, time.Time) (int64, []int64, error) {
	return 0, nil, errStore
}

func (downStore) GetConfig(context.Context, string) (anomalyrepo.Config, error) {
	return anomalyrepo.Config{}, errStore
}

func (downStore) UpsertConfig(context.Context, anomalyrepo.Config, string) error { return errStore }

func (downStore) ListCustomSlices(context.Context, string) ([]anomalyrepo.StoredCustomSlice, error) {
	return nil, errStore
}

func (downStore) ReplaceCustomSlices(context.Context, string, []anomalyrepo.StoredCustomSlice) error {
	return errStore
}

// badTenants errors so tenantLocation exercises its fallback.
type badTenants struct{}

func (badTenants) GetTimezone(context.Context, string) (string, error) { return "", errStore }

// bogusTZTenants returns an unloadable zone name.
type bogusTZTenants struct{}

func (bogusTZTenants) GetTimezone(context.Context, string) (string, error) { return "Not/AZone", nil }

func wantInternal(t *testing.T, err error) {
	t.Helper()
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Code != attunev1.ErrorCode_INTERNAL {
		t.Fatalf("want INTERNAL dispatcher error, got %v", err)
	}
}

func TestHandlersReturnInternalOnStoreFailure(t *testing.T) {
	h := NewHandler(downStore{}, badTenants{})

	_, err := h.ListAnomalies(authedCtx(), ptrext.Of(attunev1.ListAnomaliesRequest{}))
	wantInternal(t, err)

	_, err = h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total",
	}))
	wantInternal(t, err)

	_, err = h.GetAnomalyConfig(authedCtx(), ptrext.Of(attunev1.GetAnomalyConfigRequest{}))
	wantInternal(t, err)

	_, err = h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: validProtoConfig(),
	}))
	wantInternal(t, err)
}

func TestTenantLocationFallsBackToUTC(t *testing.T) {
	// Store works; timezone reads fail or resolve to garbage — the series
	// replay still answers (UTC fallback).
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1"), counts: map[string]int64{}})
	for _, tenants := range []tenantReader{badTenants{}, bogusTZTenants{}} {
		h := NewHandler(store, tenants)
		res, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
			SliceType: "total", SliceKey: "total", Days: ptrext.Of(int32(3)),
		}))
		if err != nil {
			t.Fatalf("series must answer under tz fallback: %v", err)
		}
		if len(res.Body.Points) != 3 {
			t.Fatalf("want 3 points, got %d", len(res.Body.Points))
		}
	}
}

func TestGetAnomalyEvidenceToleratesCorruptEvidence(t *testing.T) {
	ev := sampleEvent()
	ev.EvidenceJSON = "not-json"
	store := ptrext.Of(fakeStore{events: []anomalyrepo.Event{ev}, cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	res, err := h.GetAnomalyEvidence(authedCtx(), ptrext.Of(attunev1.GetAnomalyEvidenceRequest{
		EventId: ev.ID.String(),
	}))
	if err != nil {
		t.Fatalf("corrupt evidence must degrade, not fail: %v", err)
	}
	if len(res.Body.Contributions) != 0 || len(res.Body.FeedbackIds) != 0 {
		t.Fatalf("corrupt evidence must yield empty fields: %+v", res.Body)
	}
}

func TestEventToProtoResolvedAt(t *testing.T) {
	ev := sampleEvent()
	resolved := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	ev.Status = "resolved"
	ev.ResolvedAt = ptrext.Of(resolved)
	got := eventToProto(ptrext.Of(ev))
	if got.ResolvedAt != "2026-08-11T03:00:00Z" {
		t.Fatalf("resolved_at mapping wrong: %q", got.ResolvedAt)
	}
}
