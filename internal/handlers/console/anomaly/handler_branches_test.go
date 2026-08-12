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
