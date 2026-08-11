// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestBindListAnomaliesRequest(t *testing.T) {
	req := ptrext.Of(attunev1.ListAnomaliesRequest{})
	r := httptest.NewRequest(http.MethodGet, "/anomalies?status=open&slice_type=source&limit=25", nil)
	if err := BindListAnomaliesRequest(r, req); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if req.Status != "open" || req.SliceType != "source" || req.GetLimit() != 25 {
		t.Fatalf("bind mismatch: %+v", req)
	}

	bad := httptest.NewRequest(http.MethodGet, "/anomalies?limit=abc", nil)
	if err := BindListAnomaliesRequest(bad, ptrext.Of(attunev1.ListAnomaliesRequest{})); err == nil {
		t.Fatal("non-integer limit must fail")
	}
}

func TestBindGetAnomalySeriesRequest(t *testing.T) {
	req := ptrext.Of(attunev1.GetAnomalySeriesRequest{})
	r := httptest.NewRequest(http.MethodGet, "/anomalies/series?slice_type=total&slice_key=total&days=30", nil)
	if err := BindGetAnomalySeriesRequest(r, req); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if req.SliceType != "total" || req.SliceKey != "total" || req.GetDays() != 30 {
		t.Fatalf("bind mismatch: %+v", req)
	}

	bad := httptest.NewRequest(http.MethodGet, "/anomalies/series?days=x", nil)
	if err := BindGetAnomalySeriesRequest(bad, ptrext.Of(attunev1.GetAnomalySeriesRequest{})); err == nil {
		t.Fatal("non-integer days must fail")
	}
}

func TestBindGetAnomalyEvidenceRequest(t *testing.T) {
	req := ptrext.Of(attunev1.GetAnomalyEvidenceRequest{})
	r := httptest.NewRequest(http.MethodGet, "/anomalies/abc-123/evidence", nil)
	r.SetPathValue("event_id", "abc-123")
	if err := BindGetAnomalyEvidenceRequest(r, req); err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	if req.EventId != "abc-123" {
		t.Fatalf("path value not bound: %+v", req)
	}
}

func TestGetAnomalyConfigRoundTrip(t *testing.T) {
	store := ptrext.Of(fakeStore{
		cfg: anomalyrepo.DefaultConfig("t1"),
		slices: []anomalyrepo.StoredCustomSlice{{
			Name:           "api criticals",
			DefinitionJSON: `{"conditions":[{"field":"source","values":["api"]}]}`,
			Enabled:        true,
		}},
	})
	h := NewHandler(store, fakeTenants{})

	res, err := h.GetAnomalyConfig(authedCtx(), ptrext.Of(attunev1.GetAnomalyConfigRequest{}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := res.Body.Config
	if cfg.Sensitivity != "medium" || cfg.MinCount != 10 || !cfg.DetectionEnabled {
		t.Fatalf("defaults mismatch: %+v", cfg)
	}
	if len(cfg.CustomSlices) != 1 || cfg.CustomSlices[0].Name != "api criticals" {
		t.Fatalf("slices mismatch: %+v", cfg.CustomSlices)
	}
}

// recordingAudit captures audit events.
type recordingAudit struct{ events []auditlogsvc.Event }

func (r *recordingAudit) Record(_ context.Context, e auditlogsvc.Event) error {
	r.events = append(r.events, e)
	return nil
}

func TestUpdateAnomalyConfigRecordsAudit(t *testing.T) {
	store := ptrext.Of(fakeStore{cfg: anomalyrepo.DefaultConfig("t1")})
	h := NewHandler(store, fakeTenants{})
	audit := ptrext.Of(recordingAudit{})
	h.SetAuditLogger(audit)

	ctx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{TenantID: "t1", UserID: "u1", UserType: "oidc"}),
	})
	_, err := h.UpdateAnomalyConfig(ctx, ptrext.Of(attunev1.UpdateAnomalyConfigRequest{Config: validProtoConfig()}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(audit.events))
	}
	ev := audit.events[0]
	if ev.Action != "anomaly_config.update" || ev.TenantID != "t1" || ev.TargetType != "anomaly_config" {
		t.Fatalf("audit event mismatch: %+v", ev)
	}
}

func TestCustomSlicesFromProtoEdges(t *testing.T) {
	// Too many slices.
	many := make([]*attunev1.AnomalyCustomSlice, 21)
	for i := range many {
		many[i] = ptrext.Of(attunev1.AnomalyCustomSlice{
			Name:           string(rune('a' + i)),
			DefinitionJson: `{"conditions":[{"field":"source","values":["api"]}]}`,
		})
	}
	if _, err := customSlicesFromProto(many); err == nil {
		t.Fatal("21 slices must fail")
	}

	// Duplicate names.
	dup := []*attunev1.AnomalyCustomSlice{
		{Name: "x", DefinitionJson: `{"conditions":[{"field":"source","values":["api"]}]}`},
		{Name: "x", DefinitionJson: `{"conditions":[{"field":"source","values":["web"]}]}`},
	}
	if _, err := customSlicesFromProto(dup); err == nil {
		t.Fatal("duplicate names must fail")
	}

	// Existing id round-trips; bad id rejected.
	withID := []*attunev1.AnomalyCustomSlice{{
		Id: "not-a-uuid", Name: "x",
		DefinitionJson: `{"conditions":[{"field":"source","values":["api"]}]}`,
	}}
	if _, err := customSlicesFromProto(withID); err == nil {
		t.Fatal("bad id must fail")
	}

	// Dimension condition without a name.
	noName := []*attunev1.AnomalyCustomSlice{{
		Name:           "x",
		DefinitionJson: `{"conditions":[{"field":"dimension","values":["critical"]}]}`,
	}}
	if _, err := customSlicesFromProto(noName); err == nil {
		t.Fatal("dimension without name must fail")
	}

	// Too many values in one condition.
	tooMany := []*attunev1.AnomalyCustomSlice{{
		Name:           "x",
		DefinitionJson: `{"conditions":[{"field":"source","values":["a","b","c","d","e","f","g","h","i","j","k"]}]}`,
	}}
	if _, err := customSlicesFromProto(tooMany); err == nil {
		t.Fatal(">10 values must fail")
	}
}
