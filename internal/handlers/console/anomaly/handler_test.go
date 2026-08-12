// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
)

type fakeStore struct {
	events        []anomalyrepo.Event
	cfg           anomalyrepo.Config
	slices        []anomalyrepo.StoredCustomSlice
	upserts       []anomalyrepo.Config
	counts        map[string]int64
	sliceKeyCount int
	firstBucket   time.Time
}

func (f *fakeStore) FirstBucketDate(context.Context, string) (time.Time, bool, error) {
	if f.firstBucket.IsZero() {
		return time.Time{}, false, nil
	}
	return f.firstBucket, true, nil
}

func (f *fakeStore) ListEventsBySliceType(_ context.Context, _, status, sliceType string, _ int) ([]anomalyrepo.Event, error) {
	var out []anomalyrepo.Event
	for _, e := range f.events {
		if (status == "" || e.Status == status) && (sliceType == "" || e.SliceType == sliceType) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) FilterLiveFeedbackIDs(_ context.Context, _ string, ids []int64) ([]int64, error) {
	return ids, nil // handler tests treat all sample ids as live
}

func (f *fakeStore) CountRecentSliceKeys(context.Context, string, int) (int, error) {
	return f.sliceKeyCount, nil
}

func (f *fakeStore) GetEvent(_ context.Context, _ string, id uuid.UUID) (*anomalyrepo.Event, error) {
	for i := range f.events {
		if f.events[i].ID == id {
			return &f.events[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeStore) BaselineCounts(_ context.Context, _, _, key string, dates []time.Time) ([]int64, error) {
	out := make([]int64, len(dates))
	for i, d := range dates {
		out[i] = f.counts[key+"|"+d.Format("2006-01-02")]
	}
	return out, nil
}

func (f *fakeStore) CountOn(_ context.Context, _, _, key string, date time.Time) (int64, []int64, error) {
	return f.counts[key+"|"+date.Format("2006-01-02")], nil, nil
}

func (f *fakeStore) GetConfig(context.Context, string) (anomalyrepo.Config, error) {
	return f.cfg, nil
}

func (f *fakeStore) UpsertConfig(_ context.Context, cfg anomalyrepo.Config, _ string) error {
	f.upserts = append(f.upserts, cfg)
	return nil
}

func (f *fakeStore) ListCustomSlices(context.Context, string) ([]anomalyrepo.StoredCustomSlice, error) {
	return f.slices, nil
}

func (f *fakeStore) ReplaceCustomSlices(_ context.Context, _ string, s []anomalyrepo.StoredCustomSlice) error {
	f.slices = s
	return nil
}

type fakeTenants struct{}

func (fakeTenants) GetTimezone(context.Context, string) (string, error) { return "UTC", nil }

func authedCtx() *dispatcher.RequestContext[*session.AuthCtx] {
	return ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "t1", UserID: "u1"}),
	})
}

// wantValidation asserts err is a dispatcher.Error with the given code.
func wantValidation(t *testing.T, err error, code attunev1.ErrorCode) {
	t.Helper()
	var de *dispatcher.Error
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("want %v dispatcher error, got %v", code, err)
	}
}

func sampleEvent() anomalyrepo.Event {
	return anomalyrepo.Event{
		ID: uuid.New(), TenantID: "t1", SliceType: "total", SliceKey: "total",
		SliceDisplay: "All feedback", Direction: "spike",
		FirstBucketDate: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		LastBucketDate:  time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		Observed:        40, ExpectedMed: 12, ExpectedLow: 6, ExpectedHigh: 21,
		ZScore: 8.1, Status: "open",
		EvidenceJSON: `{"sample_ids":[7,8],"contribution":[{"dim":"source","value":"zendesk","share":0.9}]}`,
		CreatedAt:    time.Now(),
	}
}

func TestListAnomaliesFiltersAndMaps(t *testing.T) {
	store := ptrext.Of(fakeStore{events: []anomalyrepo.Event{sampleEvent()}, cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	res, err := h.ListAnomalies(authedCtx(), ptrext.Of(attunev1.ListAnomaliesRequest{Status: "open"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Body.Events) != 1 {
		t.Fatalf("want 1 event, got %d", len(res.Body.Events))
	}
	ev := res.Body.Events[0]
	if ev.Direction != "spike" || ev.Observed != 40 || ev.FirstBucketDate != "2026-08-09" {
		t.Fatalf("bad mapping: %+v", ev)
	}
}

func TestListAnomaliesRejectsBadStatus(t *testing.T) {
	h := NewHandler(ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}), fakeTenants{})
	_, err := h.ListAnomalies(authedCtx(), ptrext.Of(attunev1.ListAnomaliesRequest{Status: "bogus"}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
}

func TestGetAnomalySeriesValidation(t *testing.T) {
	h := NewHandler(ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}), fakeTenants{})

	_, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)

	_, err = h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total", Days: ptrext.Of(int32(500)),
	}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
}

func TestGetAnomalySeriesReplaysDetector(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1"), counts: map[string]int64{}})
	// Steady 12/day for every same-weekday across the window, spike today.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for week := 1; week <= 20; week++ {
		store.counts["total|"+today.AddDate(0, 0, -7*week).Format("2006-01-02")] = 12
	}
	store.counts["total|"+today.Format("2006-01-02")] = 40
	h := NewHandler(store, fakeTenants{})

	res, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total", Days: ptrext.Of(int32(7)),
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Body.Points) != 7 {
		t.Fatalf("want 7 points, got %d", len(res.Body.Points))
	}
	last := res.Body.Points[6]
	if !last.IsAnomalous || last.Count != 40 {
		t.Fatalf("today must be flagged anomalous: %+v", last)
	}
}

func TestGetAnomalyEvidence(t *testing.T) {
	ev := sampleEvent()
	store := ptrext.Of(fakeStore{events: []anomalyrepo.Event{ev}, cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	res, err := h.GetAnomalyEvidence(authedCtx(), ptrext.Of(attunev1.GetAnomalyEvidenceRequest{EventId: ev.ID.String()}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Body.Contributions) != 1 || res.Body.Contributions[0].Value != "zendesk" {
		t.Fatalf("bad contributions: %+v", res.Body.Contributions)
	}
	if len(res.Body.FeedbackIds) != 2 {
		t.Fatalf("want 2 feedback ids, got %v", res.Body.FeedbackIds)
	}

	_, err = h.GetAnomalyEvidence(authedCtx(), ptrext.Of(attunev1.GetAnomalyEvidenceRequest{EventId: "not-a-uuid"}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)

	_, err = h.GetAnomalyEvidence(authedCtx(), ptrext.Of(attunev1.GetAnomalyEvidenceRequest{EventId: uuid.NewString()}))
	wantValidation(t, err, attunev1.ErrorCode_NOT_FOUND)
}

func TestUpdateAnomalyConfigValidation(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	cases := []struct {
		name string
		mut  func(c *attunev1.AnomalyConfig)
	}{
		{"bad sensitivity", func(c *attunev1.AnomalyConfig) { c.Sensitivity = "extreme" }},
		{"bad min_count", func(c *attunev1.AnomalyConfig) { c.MinCount = -1 }},
		{"bad settle", func(c *attunev1.AnomalyConfig) { c.SettleDelayHours = 100 }},
		{"bad notify", func(c *attunev1.AnomalyConfig) { c.NotifyMode = "carrier-pigeon" }},
		{"drop not enabled", func(c *attunev1.AnomalyConfig) {
			c.EnabledSliceTypes = []string{"total"}
			c.DropEnabledSliceTypes = []string{"source"}
		}},
		{"bad slice def", func(c *attunev1.AnomalyConfig) {
			c.CustomSlices = []*attunev1.AnomalyCustomSlice{{Name: "x", DefinitionJson: `{"conditions":[]}`}}
		}},
		{"bad cohort uuid", func(c *attunev1.AnomalyConfig) {
			c.CustomSlices = []*attunev1.AnomalyCustomSlice{{
				Name:           "x",
				DefinitionJson: `{"conditions":[{"field":"cohort","values":["nope"]}]}`,
			}}
		}},
	}
	for _, tc := range cases {
		cfg := validProtoConfig()
		tc.mut(cfg)
		_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{Config: cfg}))
		var de *dispatcher.Error
		if !errors.As(err, &de) || de.Code != attunev1.ErrorCode_VALIDATION {
			t.Fatalf("%s: want VALIDATION, got %v", tc.name, err)
		}
	}
	if len(store.upserts) != 0 {
		t.Fatalf("invalid configs must not persist, got %d upserts", len(store.upserts))
	}
}

func TestUpdateAnomalyConfigRoundTrip(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	cfg := validProtoConfig()
	cfg.Sensitivity = "low"
	cfg.CustomSlices = []*attunev1.AnomalyCustomSlice{{
		Name:           "api criticals",
		DefinitionJson: `{"conditions":[{"field":"source","values":["api"]}]}`,
		Enabled:        true,
	}}
	if _, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{Config: cfg})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.upserts) != 1 || store.upserts[0].Sensitivity != "low" {
		t.Fatalf("config not persisted: %+v", store.upserts)
	}
	if len(store.slices) != 1 || store.slices[0].Name != "api criticals" {
		t.Fatalf("slices not persisted: %+v", store.slices)
	}
}

func validProtoConfig() *attunev1.AnomalyConfig {
	return ptrext.Of(attunev1.AnomalyConfig{
		Sensitivity:           "medium",
		MinCount:              10,
		SettleDelayHours:      3,
		EnabledSliceTypes:     anomalyrepo.AllSliceTypes(),
		DropEnabledSliceTypes: []string{"total", "source"},
		NotifyMode:            "immediate",
		DetectionEnabled:      true,
	})
}
