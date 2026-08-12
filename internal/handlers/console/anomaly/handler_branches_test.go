// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
)

// slicesDownStore succeeds on config writes but fails the custom-slice
// replace, isolating that 500 branch.
type slicesDownStore struct{ fakeStore }

func (s *slicesDownStore) ReplaceCustomSlices(context.Context, string, []anomalyrepo.StoredCustomSlice) error {
	return errStore
}

// reloadDownStore lets the write path succeed but fails the reload read.
type reloadDownStore struct {
	fakeStore
	reads int
}

func (s *reloadDownStore) GetConfig(ctx context.Context, tenantID string) (anomalyrepo.Config, error) {
	s.reads++
	// GetConfig is only reached at post-write reload time — fail it there.
	return anomalyrepo.Config{}, errStore
}

func TestUpdateAnomalyConfigNilConfig(t *testing.T) {
	h := NewHandler(ptrext.Of(fakeStore{}), fakeTenants{})
	_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
}

func TestUpdateAnomalyConfigSlicesWriteFailure(t *testing.T) {
	store := ptrext.Of(slicesDownStore{fakeStore: fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}})
	h := NewHandler(store, fakeTenants{})
	_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: validProtoConfig(),
	}))
	wantInternal(t, err)
}

func TestUpdateAnomalyConfigReloadFailure(t *testing.T) {
	store := ptrext.Of(reloadDownStore{fakeStore: fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}})
	h := NewHandler(store, fakeTenants{})
	_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: validProtoConfig(),
	}))
	wantInternal(t, err)
}

func TestRecordAuditDefaultsActorType(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})
	audit := ptrext.Of(recordingAudit{})
	h.SetAuditLogger(audit)
	// authedCtx has empty UserType → actorType falls back to "admin".
	_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: validProtoConfig(),
	}))
	if err != nil || len(audit.events) != 1 {
		t.Fatalf("audit must record: err=%v events=%d", err, len(audit.events))
	}
	if audit.events[0].Actor.Type != "admin" {
		t.Fatalf("empty user type must default to admin, got %q", audit.events[0].Actor.Type)
	}
}

func TestConfigValidationUnknownSliceTypes(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})
	for _, mut := range []func(c *attunev1.AnomalyConfig){
		func(c *attunev1.AnomalyConfig) { c.EnabledSliceTypes = []string{"bogus"} },
		func(c *attunev1.AnomalyConfig) { c.DropEnabledSliceTypes = []string{"bogus"} },
	} {
		cfg := validProtoConfig()
		mut(cfg)
		_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{Config: cfg}))
		wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
	}
}

func TestCustomSliceNameAndIDValidation(t *testing.T) {
	goodDef := `{"conditions":[{"field":"source","values":["api"]}]}`
	cases := []struct {
		name  string
		slice *attunev1.AnomalyCustomSlice
		want  string
	}{
		{"empty name", ptrext.Of(attunev1.AnomalyCustomSlice{Name: "", DefinitionJson: goodDef}), "1-80"},
		{"long name", ptrext.Of(attunev1.AnomalyCustomSlice{Name: strings.Repeat("x", 81), DefinitionJson: goodDef}), "1-80"},
		{"bad id", ptrext.Of(attunev1.AnomalyCustomSlice{Name: "a", Id: "not-a-uuid", DefinitionJson: goodDef}), "invalid id"},
	}
	for _, tc := range cases {
		_, err := customSlicesFromProto([]*attunev1.AnomalyCustomSlice{tc.slice})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want %q, got %v", tc.name, tc.want, err)
		}
	}
	// Provided valid id is kept.
	out, err := customSlicesFromProto([]*attunev1.AnomalyCustomSlice{ptrext.Of(attunev1.AnomalyCustomSlice{
		Name: "a", Id: "11111111-2222-3333-4444-555555555555", DefinitionJson: goodDef,
	})})
	if err != nil || out[0].ID.String() != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("valid id must round-trip: %v %+v", err, out)
	}
}

func TestValidateDefinitionJSONEdges(t *testing.T) {
	if err := validateDefinitionJSON("not-json"); err == nil {
		t.Fatal("invalid JSON must fail")
	}
	if err := validateDefinitionJSON(`{"conditions":[{"field":"cluster","values":["x"]}]}`); err == nil {
		t.Fatal("cluster is not operator-whitelisted")
	}
}

func TestListAnomaliesSliceTypeAndLimitBranches(t *testing.T) {
	store := ptrext.Of(fakeStore{events: []anomalyrepo.Event{sampleEvent()}, cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})

	_, err := h.ListAnomalies(authedCtx(), ptrext.Of(attunev1.ListAnomaliesRequest{SliceType: "bogus"}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)

	res, err := h.ListAnomalies(authedCtx(), ptrext.Of(attunev1.ListAnomaliesRequest{Limit: ptrext.Of(int32(9999))}))
	if err != nil || len(res.Body.Events) != 1 {
		t.Fatalf("over-limit request must clamp and answer: %v", err)
	}
}

func TestValidateSliceRefBranches(t *testing.T) {
	if err := validateSliceRef("bogus", "total"); err == nil {
		t.Fatal("unknown slice type must fail")
	}
	if err := validateSliceRef("total", strings.Repeat("k", 121)); err == nil {
		t.Fatal("oversized slice key must fail")
	}
	if err := validateSliceRef("total", "total"); err != nil {
		t.Fatalf("valid ref must pass: %v", err)
	}
}

// listSlicesDownStore fails only the custom-slice read (loadConfig branch).
type listSlicesDownStore struct{ fakeStore }

func (s *listSlicesDownStore) ListCustomSlices(context.Context, string) ([]anomalyrepo.StoredCustomSlice, error) {
	return nil, errStore
}

func TestGetAnomalyConfigSlicesReadFailure(t *testing.T) {
	store := ptrext.Of(listSlicesDownStore{fakeStore: fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}})
	h := NewHandler(store, fakeTenants{})
	_, err := h.GetAnomalyConfig(authedCtx(), ptrext.Of(attunev1.GetAnomalyConfigRequest{}))
	wantInternal(t, err)
}

// baselineDownStore answers config but fails count reads (replay branch).
type baselineDownStore struct{ fakeStore }

func (s *baselineDownStore) BaselineCounts(context.Context, string, string, string, []time.Time) ([]int64, error) {
	return nil, errStore
}

func TestGetAnomalySeriesReplayFailure(t *testing.T) {
	store := ptrext.Of(baselineDownStore{fakeStore: fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}})
	h := NewHandler(store, fakeTenants{})
	_, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total",
	}))
	wantInternal(t, err)
}

// liveIDsDownStore fails only the GDPR live-id filter (fail-closed branch).
type liveIDsDownStore struct{ fakeStore }

func (s *liveIDsDownStore) FilterLiveFeedbackIDs(context.Context, string, []int64) ([]int64, error) {
	return nil, errStore
}

func TestGetAnomalyEvidenceLiveFilterFailsClosed(t *testing.T) {
	ev := sampleEvent()
	store := ptrext.Of(liveIDsDownStore{fakeStore: fakeStore{events: []anomalyrepo.Event{ev}, cfg: anomalyrepo.DefaultConfig("t1")}})
	h := NewHandler(store, fakeTenants{})
	res, err := h.GetAnomalyEvidence(authedCtx(), ptrext.Of(attunev1.GetAnomalyEvidenceRequest{
		EventId: ev.ID.String(),
	}))
	if err != nil {
		t.Fatalf("live-filter failure must degrade, not fail: %v", err)
	}
	if len(res.Body.FeedbackIds) != 0 {
		t.Fatal("failed live filter must return no ids (fail closed)")
	}
}

// overCapStore reports an over-cap series count; stored config carries the
// full slice set + 2 enabled custom slices so shrink comparisons have room.
type overCapStore struct{ fakeStore }

func (s *overCapStore) CountRecentSliceKeys(context.Context, string, int) (int, error) {
	return 10_000, nil
}

func TestSeriesCapAllowsShrinkingConfig(t *testing.T) {
	stored := anomalyrepo.DefaultConfig("t1")
	store := ptrext.Of(overCapStore{fakeStore: fakeStore{
		cfg: stored,
		slices: []anomalyrepo.StoredCustomSlice{
			{Name: "a", Enabled: true}, {Name: "b", Enabled: true},
		},
	}})
	h := NewHandler(store, fakeTenants{})

	// Shrinking: drop custom slices entirely and disable a slice type.
	shrink := validProtoConfig()
	shrink.EnabledSliceTypes = []string{"total", "source"}
	shrink.DropEnabledSliceTypes = []string{"total"}
	shrink.CustomSlices = nil
	if _, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: shrink,
	})); err != nil {
		t.Fatalf("shrinking config must bypass the series cap: %v", err)
	}

	// Growing: an over-cap tenant adding custom slices is still rejected.
	store2 := ptrext.Of(overCapStore{fakeStore: fakeStore{cfg: stored}})
	h2 := NewHandler(store2, fakeTenants{})
	grow := validProtoConfig()
	grow.CustomSlices = []*attunev1.AnomalyCustomSlice{ptrext.Of(attunev1.AnomalyCustomSlice{
		Name:           "new-slice",
		Enabled:        true,
		DefinitionJson: `{"conditions":[{"field":"source","values":["api"]}]}`,
	})}
	_, err := h2.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: grow,
	}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
}

func TestUpdateAnomalyConfigBackfillWarning(t *testing.T) {
	// fakeStore's GetConfig returns cfg as stored — simulate the post-write
	// state where config_version has advanced past backfill_version.
	cfg := anomalyrepo.DefaultConfig("t1")
	cfg.ConfigVersion = 2
	cfg.BackfillVersion = 1
	store := ptrext.Of(fakeStore{cfg: cfg})
	h := NewHandler(store, fakeTenants{})
	res, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: validProtoConfig(),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Body.Warning, "paused") {
		t.Fatalf("pending backfill must warn, got %q", res.Body.Warning)
	}
}

// overCapGetConfigDown: over-cap count and the stored-config read fails —
// the shrink check must fail closed (treat as growing → reject).
type overCapGetConfigDown struct{ overCapStore }

func (s *overCapGetConfigDown) GetConfig(context.Context, string) (anomalyrepo.Config, error) {
	return anomalyrepo.Config{}, errStore
}

// overCapListSlicesDown: stored config reads fine, custom-slice read fails.
type overCapListSlicesDown struct{ overCapStore }

func (s *overCapListSlicesDown) ListCustomSlices(context.Context, string) ([]anomalyrepo.StoredCustomSlice, error) {
	return nil, errStore
}

func TestSeriesCapShrinkCheckFailsClosed(t *testing.T) {
	shrink := validProtoConfig()
	shrink.EnabledSliceTypes = []string{"total"}
	shrink.DropEnabledSliceTypes = []string{"total"}
	shrink.CustomSlices = nil

	for name, store := range map[string]store{
		"config read down": ptrext.Of(overCapGetConfigDown{overCapStore{fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}}}),
		"slices read down": ptrext.Of(overCapListSlicesDown{overCapStore{fakeStore{cfg: anomalyrepo.DefaultConfig("t1")}}}),
	} {
		h := NewHandler(store, fakeTenants{})
		_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
			Config: shrink,
		}))
		if err == nil {
			t.Fatalf("%s: unverifiable shrink must be rejected", name)
		}
	}
}

func TestSeriesCapRejectsNewSliceType(t *testing.T) {
	// Stored config has only total enabled; submitting source is growth.
	stored := anomalyrepo.DefaultConfig("t1")
	stored.EnabledSliceTypes = []string{"total"}
	stored.DropEnabledSliceTypes = []string{"total"}
	store := ptrext.Of(overCapStore{fakeStore{cfg: stored}})
	h := NewHandler(store, fakeTenants{})
	grow := validProtoConfig()
	grow.EnabledSliceTypes = []string{"total", "source"}
	grow.DropEnabledSliceTypes = []string{"total"}
	grow.CustomSlices = nil
	_, err := h.UpdateAnomalyConfig(authedCtx(), ptrext.Of(attunev1.UpdateAnomalyConfigRequest{
		Config: grow,
	}))
	wantValidation(t, err, attunev1.ErrorCode_VALIDATION)
}

// TestGetAnomalySeriesColdStartClamp (#18): a young tenant's chart must
// mark early days insufficient instead of judging them against phantom
// zero baselines — mirroring the worker's clamp.
func TestGetAnomalySeriesColdStartClamp(t *testing.T) {
	store := ptrext.Of(fakeStore{
		cfg:    anomalyrepo.DefaultConfig("t1"),
		counts: map[string]int64{},
		// Tenant born 2 days ago: every baseline date predates it.
		firstBucket: time.Now().UTC().AddDate(0, 0, -2),
	})
	h := NewHandler(store, fakeTenants{})
	res, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total", Days: ptrext.Of(int32(3)),
	}))
	if err != nil {
		t.Fatalf("GetAnomalySeries: %v", err)
	}
	for _, p := range res.Body.Points {
		if !p.Insufficient {
			t.Fatalf("cold-start day %s must be insufficient, got %+v", p.Date, p)
		}
		if p.IsAnomalous {
			t.Fatalf("cold-start day %s must not be anomalous", p.Date)
		}
	}
}

// firstBucketDownStore fails only the cold-start lookup.
type firstBucketDownStore struct{ fakeStore }

func (s *firstBucketDownStore) FirstBucketDate(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, errStore
}

func TestGetAnomalySeriesFirstBucketFailureFailsClosed(t *testing.T) {
	store := ptrext.Of(firstBucketDownStore{fakeStore: fakeStore{cfg: anomalyrepo.DefaultConfig("t1"), counts: map[string]int64{}}})
	h := NewHandler(store, fakeTenants{})
	_, err := h.GetAnomalySeries(authedCtx(), ptrext.Of(attunev1.GetAnomalySeriesRequest{
		SliceType: "total", SliceKey: "total",
	}))
	wantInternal(t, err)
}
