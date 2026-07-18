// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures build request pointers

package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
)

type fakeQualityActionStore struct {
	listOpts  repofeedback.QualityActionListOpts
	listRows  []repofeedback.QualityAction
	listErr   error
	upsert    *repofeedback.QualityActionUpsert
	upsertErr error
}

func (s *fakeQualityActionStore) ListQualityActions(_ context.Context, opts repofeedback.QualityActionListOpts) ([]repofeedback.QualityAction, error) {
	s.listOpts = opts
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listRows, nil
}

func (s *fakeQualityActionStore) UpsertQualityActionStatus(_ context.Context, in repofeedback.QualityActionUpsert) (*repofeedback.QualityAction, error) {
	s.upsert = ptrext.Of(in)
	if s.upsertErr != nil {
		return nil, s.upsertErr
	}
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	return ptrext.Of(repofeedback.QualityAction{
		ID:                "11111111-1111-1111-1111-111111111111",
		TenantID:          in.TenantID,
		ActionKey:         in.ActionKey,
		Signal:            in.Signal,
		Status:            in.Status,
		Severity:          in.Severity,
		TargetPath:        in.TargetPath,
		MetricLabel:       in.MetricLabel,
		MetricValue:       in.MetricValue,
		RecommendationKey: in.RecommendationKey,
		EvidenceJSON:      in.EvidenceJSON,
		CreatedAt:         now,
		LastSeenAt:        now,
		UpdatedAt:         now,
		UpdatedBy:         in.ActorUserID,
	}), nil
}

func TestQualityActionHandlerList(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	ack := now.Add(-time.Minute)
	store := &fakeQualityActionStore{
		listRows: []repofeedback.QualityAction{{
			ID:                "action-1",
			ActionKey:         "search.zero_result",
			Signal:            "zero_result",
			Status:            repofeedback.QualityActionStatusOpen,
			Severity:          repofeedback.QualityActionSeverityAlert,
			RecommendationKey: "control_tower.actions.zero_result",
			EvidenceJSON:      `{"metric":"21%"}`,
			CreatedAt:         now,
			LastSeenAt:        now,
			AcknowledgedAt:    &ack,
			UpdatedAt:         now,
		}},
	}

	w := httptest.NewRecorder()
	bindQualityActionListHandler(NewQualityActionHandler(store))(w, dispatchtest.Request(
		http.MethodGet,
		"/quality-actions?status=open&limit=10",
		"",
	))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	require.Equal(t, "tenant-1", store.listOpts.TenantID)
	require.Equal(t, repofeedback.QualityActionStatusOpen, store.listOpts.Status)
	require.Equal(t, 10, store.listOpts.Limit)
	actions := body["actions"].([]any)
	require.Len(t, actions, 1)
	require.Equal(t, "search.zero_result", actions[0].(map[string]any)["actionKey"])
	require.Equal(t, ack.Format(time.RFC3339), actions[0].(map[string]any)["acknowledgedAt"])
}

func TestQualityActionHandlerListFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *QualityActionHandler
		url     string
		want    int
	}{
		{
			name:    "bad status",
			handler: NewQualityActionHandler(&fakeQualityActionStore{}),
			url:     "/quality-actions?status=bad",
			want:    http.StatusBadRequest,
		},
		{
			name:    "bad limit",
			handler: NewQualityActionHandler(&fakeQualityActionStore{}),
			url:     "/quality-actions?limit=0",
			want:    http.StatusBadRequest,
		},
		{
			name:    "non integer limit",
			handler: NewQualityActionHandler(&fakeQualityActionStore{}),
			url:     "/quality-actions?limit=nope",
			want:    http.StatusBadRequest,
		},
		{
			name:    "store error",
			handler: NewQualityActionHandler(&fakeQualityActionStore{listErr: errors.New("db down")}),
			url:     "/quality-actions?status=all",
			want:    http.StatusInternalServerError,
		},
		{
			name:    "missing store",
			handler: NewQualityActionHandler(nil),
			url:     "/quality-actions",
			want:    http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			bindQualityActionListHandler(tt.handler)(w, dispatchtest.Request(http.MethodGet, tt.url, ""))
			require.Equal(t, tt.want, w.Code)
		})
	}
}

func TestQualityActionHandlerUpdate(t *testing.T) {
	t.Parallel()
	store := &fakeQualityActionStore{}
	body := `{
		"action_key":"search.zero_result",
		"signal":"zero_result",
		"status":"acknowledged",
		"severity":"alert",
		"target_path":"/analytics/search-quality",
		"metric_label":"Zero result",
		"metric_value":"21%",
		"recommendation_key":"control_tower.actions.zero_result",
		"evidence_json":"{\"query\":\"export\"}"
	}`

	w := httptest.NewRecorder()
	bindQualityActionUpdateHandler(NewQualityActionHandler(store))(w, dispatchtest.Request(
		http.MethodPost,
		"/quality-actions/update",
		body,
	))

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, store.upsert)
	require.Equal(t, "tenant-1", store.upsert.TenantID)
	require.Equal(t, "user-1", store.upsert.ActorUserID)
	require.Equal(t, repofeedback.QualityActionStatusAcknowledged, store.upsert.Status)
	require.JSONEq(t, `{"query":"export"}`, store.upsert.EvidenceJSON)
}

func TestQualityActionHandlerUpdateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler *QualityActionHandler
		body    string
		want    int
	}{
		{
			name:    "missing store",
			handler: NewQualityActionHandler(nil),
			body:    `{}`,
			want:    http.StatusInternalServerError,
		},
		{
			name:    "invalid body",
			handler: NewQualityActionHandler(&fakeQualityActionStore{}),
			body:    `{"action_key":"Bad Key"}`,
			want:    http.StatusBadRequest,
		},
		{
			name:    "store error",
			handler: NewQualityActionHandler(&fakeQualityActionStore{upsertErr: errors.New("db down")}),
			body: `{
				"action_key":"search.zero_result",
				"signal":"zero_result",
				"status":"resolved",
				"severity":"watch",
				"target_path":"/analytics/search-quality",
				"evidence_json":"{}"
			}`,
			want: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			bindQualityActionUpdateHandler(tt.handler)(w, dispatchtest.Request(
				http.MethodPost,
				"/quality-actions/update",
				tt.body,
			))
			require.Equal(t, tt.want, w.Code)
		})
	}
}

func TestQualityActionUpdateValidation(t *testing.T) {
	t.Parallel()
	auth := ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"})

	_, err := qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey: "Bad Key",
		Signal:    "zero_result",
	}))
	require.EqualError(t, err, "action_key must be 1-120 chars using lowercase letters, numbers, dot, colon, underscore, or dash")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       "zero_result",
		Status:       "acknowledged",
		Severity:     "alert",
		TargetPath:   "analytics/search-quality",
		EvidenceJson: "{}",
	}))
	require.EqualError(t, err, "target_path must be an absolute Console path")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       "zero_result",
		Status:       "bad",
		Severity:     "alert",
		TargetPath:   "/analytics/search-quality",
		EvidenceJson: "{}",
	}))
	require.EqualError(t, err, "status must be open, acknowledged, resolved, or dismissed")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       strings.Repeat("x", 81),
		Status:       "acknowledged",
		Severity:     "alert",
		TargetPath:   "/analytics/search-quality",
		EvidenceJson: "{}",
	}))
	require.EqualError(t, err, "signal is required and must be at most 80 characters")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       "zero_result",
		Status:       "acknowledged",
		Severity:     "bad",
		TargetPath:   "/analytics/search-quality",
		EvidenceJson: "{}",
	}))
	require.EqualError(t, err, "severity must be alert, watch, normal, or insufficient_data")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       "zero_result",
		Status:       "acknowledged",
		Severity:     "alert",
		TargetPath:   "/analytics/search-quality",
		EvidenceJson: "[]",
	}))
	require.EqualError(t, err, "evidence_json must be a JSON object")

	_, err = qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:    "search.zero_result",
		Signal:       "zero_result",
		Status:       "acknowledged",
		Severity:     "alert",
		TargetPath:   "/analytics/search-quality",
		EvidenceJson: strings.Repeat("x", qualityActionMaxEvidenceBytes+1),
	}))
	require.EqualError(t, err, "evidence_json must be at most 4096 bytes")

	got, err := qualityActionUpdateFromRequest(auth, ptrext.Of(attunev1.UpdateQualityActionRequest{
		ActionKey:         " search.zero_result ",
		Signal:            " zero_result ",
		TargetPath:        "/analytics/search-quality",
		MetricLabel:       strings.Repeat("m", 130),
		MetricValue:       " 42% ",
		RecommendationKey: strings.Repeat("r", 170),
		EvidenceJson:      "null",
	}))
	require.NoError(t, err)
	require.Equal(t, repofeedback.QualityActionStatusOpen, got.Status)
	require.Equal(t, repofeedback.QualityActionSeverityWatch, got.Severity)
	require.Len(t, got.MetricLabel, 120)
	require.Len(t, got.RecommendationKey, 160)
	require.JSONEq(t, `{}`, got.EvidenceJSON)
}

func TestNormalizeQualityActionEvidenceEmpty(t *testing.T) {
	t.Parallel()

	got, err := normalizeQualityActionEvidence("   ")

	require.NoError(t, err)
	require.JSONEq(t, `{}`, got)
}

func bindQualityActionListHandler(h *QualityActionHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.QualityActionHandler.ListQualityActions",
		dispatcher.Query(
			func() *attunev1.ListQualityActionsRequest {
				return ptrext.Of(attunev1.ListQualityActionsRequest{})
			},
			BindListQualityActionsRequest,
		),
		h.ListQualityActions,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListQualityActionsRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func bindQualityActionUpdateHandler(h *QualityActionHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.QualityActionHandler.UpdateQualityAction",
		dispatcher.JSON(func() *attunev1.UpdateQualityActionRequest {
			return ptrext.Of(attunev1.UpdateQualityActionRequest{})
		}),
		h.UpdateQualityAction,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.UpdateQualityActionRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}
