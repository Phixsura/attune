// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/survey"
	svc "github.com/Phixsura/attune/internal/service/survey"
)

func TestExportNPSCampaignRunEvidenceReturnsAggregateCSVAndAudit(t *testing.T) {
	t.Parallel()
	campaignID := uuid.New()
	runID := uuid.New()
	evidence := npsEvidenceExportFixture(campaignID, runID)
	generatedAt := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	data, err := svc.BuildNPSCampaignRunEvidenceCSV(evidence, generatedAt)
	if err != nil {
		t.Fatalf("BuildNPSCampaignRunEvidenceCSV() error = %v", err)
	}
	digest := sha256.Sum256(data)
	fake := ptrext.Of(fakeSurveyService{npsEvidenceExport: repo.NPSCampaignRunEvidenceExport{
		ID:             uuid.New(),
		TenantID:       "tenant-1",
		CampaignID:     campaignID,
		RunID:          runID,
		ReportVersion:  "1",
		GeneratedAt:    generatedAt,
		Artifact:       data,
		ArtifactSHA256: "sha256:" + fmt.Sprintf("%x", digest),
		CreatedByType:  "admin",
		CreatedBy:      "user-1",
	}})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)
	req := npsEvidenceExportRequest(campaignID, runID)
	res := httptest.NewRecorder()

	h.ExportNPSCampaignRunEvidence(res, req)
	assertNPSExportResponse(t, res, campaignID, runID)
	assertNPSExportAudit(t, audit, runID, digest)
}

func TestDownloadNPSCampaignRunEvidenceUsesPersistedArtifactAndAudit(t *testing.T) {
	t.Parallel()
	campaignID := uuid.New()
	runID := uuid.New()
	exportID := uuid.New()
	artifact := []byte("report_version,run_id\n1," + runID.String() + "\n")
	digest := sha256.Sum256(artifact)
	fake := ptrext.Of(fakeSurveyService{npsEvidenceExport: repo.NPSCampaignRunEvidenceExport{
		ID:             exportID,
		CampaignID:     campaignID,
		RunID:          runID,
		ReportVersion:  "1",
		Artifact:       artifact,
		ArtifactSHA256: "sha256:" + fmt.Sprintf("%x", digest),
	}})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)
	req := npsEvidenceExportRequest(campaignID, runID)
	routeContext := chi.RouteContext(req.Context())
	routeContext.URLParams.Add("export_id", exportID.String())
	res := httptest.NewRecorder()

	h.DownloadNPSCampaignRunEvidence(res, req)
	if res.Code != http.StatusOK || !bytes.Equal(res.Body.Bytes(), artifact) {
		t.Fatalf("download response = status %d body %q", res.Code, res.Body.Bytes())
	}
	if got := res.Header().Get("Digest"); got != "sha-256="+base64.StdEncoding.EncodeToString(digest[:]) {
		t.Fatalf("download digest = %q", got)
	}
	if len(audit.events) != 1 || audit.events[0].TargetID != exportID.String() {
		t.Fatalf("download audit = %+v", audit.events)
	}
	after, ok := audit.events[0].After.(map[string]any)
	if !ok || after["operation"] != "download" || after["artifact_sha256"] != "sha256:"+fmt.Sprintf("%x", digest) {
		t.Fatalf("download audit after = %#v", audit.events[0].After)
	}
}

func TestCreateNPSCampaignRunEvidenceExportIsIdempotentAndAuditsOnlyCreation(t *testing.T) {
	t.Parallel()
	campaignID := uuid.New()
	runID := uuid.New()
	requestKey := uuid.New()
	generatedAt := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	artifact := []byte("report_version,run_id\n1," + runID.String() + "\n")
	digest := sha256.Sum256(artifact)
	fake := ptrext.Of(fakeSurveyService{npsEvidenceExport: repo.NPSCampaignRunEvidenceExport{
		ID:             uuid.New(),
		CampaignID:     campaignID,
		RunID:          runID,
		ReportVersion:  "1",
		GeneratedAt:    generatedAt,
		CreatedAt:      generatedAt,
		ExpiresAt:      generatedAt.Add(30 * 24 * time.Hour),
		Artifact:       artifact,
		ArtifactSHA256: "sha256:" + fmt.Sprintf("%x", digest),
	}})
	audit := ptrext.Of(fakeSurveyAudit{})
	h := NewHandler(fake)
	h.SetAuditLogger(audit)
	req := ptrext.Of(attunev1.CreateNpsCampaignRunEvidenceExportRequest{
		CampaignId: campaignID.String(), RunId: runID.String(), ClientRequestKey: requestKey.String(),
	})
	first, err := h.CreateNPSCampaignRunEvidenceExport(surveyHandlerContext(), req)
	if err != nil || first.Status != http.StatusCreated || first.Body.GetExpiresAt() == "" {
		t.Fatalf("first create = status %d body %#v err=%v", first.Status, first.Body, err)
	}
	fake.npsEvidenceExportReplayed = true
	second, err := h.CreateNPSCampaignRunEvidenceExport(surveyHandlerContext(), req)
	if err != nil || second.Status != http.StatusOK || second.Body.GetId() != first.Body.GetId() {
		t.Fatalf("replay = status %d body %#v err=%v", second.Status, second.Body, err)
	}
	if fake.npsEvidenceExportCalls != 2 || fake.npsEvidenceExportRequestKey != requestKey {
		t.Fatalf("idempotency input = calls %d key %s", fake.npsEvidenceExportCalls, fake.npsEvidenceExportRequestKey)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want one creation event", len(audit.events))
	}
}

func npsEvidenceExportFixture(campaignID, runID uuid.UUID) svc.NPSCampaignRunEvidence {
	return svc.NPSCampaignRunEvidence{
		Run: repo.NPSCampaignRun{
			ID:                                       runID,
			CampaignID:                               campaignID,
			Sequence:                                 7,
			Status:                                   repo.NPSRunClosed,
			ScheduledAt:                              time.Date(2026, 8, 1, 2, 3, 4, 0, time.UTC),
			MeasurementKey:                           "nps:v4:opaque",
			MeasurementReadiness:                     repo.NPSMeasurementQualified,
			EvaluatedCount:                           12,
			EligibleCount:                            10,
			InvitationCount:                          8,
			DeliveredCount:                           7,
			StartedCount:                             6,
			CompletedCount:                           4,
			HostedVisitRate:                          0.75,
			CompletionRate:                           0.666667,
			CompletedResponseRate:                    0.5,
			NPS:                                      25,
			DetractorCount:                           1,
			PassiveCount:                             1,
			PromoterCount:                            2,
			MinimumCompletedResponses:                3,
			MinimumResponseRatePercent:               40,
			SamplePlanningPopulationCount:            10,
			SamplePlanningRequiredCompletedResponses: 4,
			SamplePlanningInvitationTarget:           8,
			CollectionDays:                           14,
			MaximumRunRecipients:                     100,
			ContactCooldownDays:                      90,
			RecurrenceSamplingPercent:                25,
		},
		Analytics: repo.Analytics{ScoreDistribution: []repo.ScoreBucket{
			{Score: 0, Count: 1}, {Score: 7, Count: 1}, {Score: 9, Count: 2},
		}},
	}
}

func npsEvidenceExportRequest(campaignID, runID uuid.UUID) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("campaign_id", campaignID.String())
	routeContext.URLParams.Add("run_id", runID.String())
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, routeContext)
	ctx = session.WithAuthCtx(ctx, ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"}))
	return req.WithContext(ctx)
}

func assertNPSExportResponse(t *testing.T, res *httptest.ResponseRecorder, campaignID, runID uuid.UUID) [32]byte {
	t.Helper()

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := res.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("missing content disposition")
	}
	digest := sha256.Sum256(res.Body.Bytes())
	wantDigest := "sha-256=" + base64.StdEncoding.EncodeToString(digest[:])
	if got := res.Header().Get("Digest"); got != wantDigest {
		t.Fatalf("digest = %q, want %q", got, wantDigest)
	}
	if got := res.Header().Get("ETag"); got == "" {
		t.Fatal("missing ETag")
	}
	if got := res.Header().Get("Content-Length"); got == "" || got != fmt.Sprintf("%d", res.Body.Len()) {
		t.Fatalf("content length = %q, want %d", got, res.Body.Len())
	}
	if got := res.Header().Get("Last-Modified"); got == "" {
		t.Fatal("missing Last-Modified")
	}
	assertNPSExportCSV(t, res.Body.Bytes(), campaignID, runID)
	return digest
}

func assertNPSExportCSV(t *testing.T, data []byte, campaignID, runID uuid.UUID) {
	t.Helper()
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(rows) != 2 || len(rows[0]) != len(rows[1]) {
		t.Fatalf("CSV rows = %d/%d, want one header and one row", len(rows), len(rows[0]))
	}
	if rows[0][0] != "report_version" || rows[1][0] != "3" {
		t.Fatalf("CSV version = %q/%q", rows[0][0], rows[1][0])
	}
	if rows[1][2] != campaignID.String() || rows[1][3] != runID.String() {
		t.Fatalf("CSV scope = %q/%q", rows[1][2], rows[1][3])
	}
	if rows[0][21] != "nps_available" || rows[1][21] != "true" || rows[0][22] != "detractor_count" || rows[0][26] != "quality_flagged_response_count" {
		t.Fatalf("CSV NPS availability columns = %q/%q/%q", rows[0][21], rows[1][21], rows[0][22])
	}
}

func assertNPSExportAudit(t *testing.T, audit *fakeSurveyAudit, runID uuid.UUID, digest [32]byte) {
	t.Helper()
	if len(audit.events) != 1 || audit.events[0].Action != "survey.nps_run_evidence_export" {
		t.Fatalf("audit events = %+v", audit.events)
	}
	if audit.events[0].TargetID != runID.String() {
		t.Fatalf("audit target = %q", audit.events[0].TargetID)
	}
	after, ok := audit.events[0].After.(map[string]any)
	if !ok || after["artifact_sha256"] != "sha256:"+fmt.Sprintf("%x", digest) {
		t.Fatalf("audit artifact hash = %#v", audit.events[0].After)
	}
}

func TestNPSEvidenceCSVDoesNotExportRespondentFields(t *testing.T) {
	t.Parallel()
	data, err := npsEvidenceCSV(svc.NPSCampaignRunEvidence{
		Run:       repo.NPSCampaignRun{CampaignID: uuid.New(), ID: uuid.New(), ScheduledAt: time.Now()},
		Analytics: repo.Analytics{},
	}, time.Now())
	if err != nil {
		t.Fatalf("npsEvidenceCSV() error = %v", err)
	}
	if bytes.Contains(data, []byte("email")) || bytes.Contains(data, []byte("comment")) || bytes.Contains(data, []byte("subject")) {
		t.Fatalf("CSV contains respondent fields: %s", data)
	}
}
