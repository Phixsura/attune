// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package feedback

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	signalgraphrepo "github.com/Phixsura/attune/internal/repo/signalgraph"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
	signalgraphsvc "github.com/Phixsura/attune/internal/service/signalgraph"
)

type fakeFeedbackRepo struct {
	listRows   []feedbackrepo.ConsoleListRow
	listErr    error
	listOpts   feedbackrepo.ConsoleListOpts
	listTenant string

	getRow    *feedbackrepo.ConsoleDetailRow
	getErr    error
	getID     int64
	getTenant string

	identityRows   []feedbackrepo.IdentityReviewRow
	identityErr    error
	identityTenant string
	identityLimit  int

	usageRows []feedbackrepo.UsageBucket
	usageErr  error
	urgent    int64
	urgentErr error
	topValues map[string][]feedbackrepo.ValueCount
	topErr    error

	workbench       *feedbackrepo.TerminalFailureWorkbench
	workbenchErr    error
	workbenchTenant string
	workbenchFrom   time.Time
	workbenchTo     time.Time

	triageCenter feedbackrepo.FeedbackTriageCommandCenter
	triageErr    error
	triageTenant string
	triageNow    time.Time

	assignmentEscalations       feedbackrepo.AssignmentEscalationQueue
	assignmentEscalationsErr    error
	assignmentEscalationsTenant string
	assignmentEscalationsNow    time.Time
	assignmentEscalationsLimit  int

	qualityRefreshOpts feedbackrepo.ClassificationQualityRefreshOpts
	qualityRefreshErr  error
	qualityAggregate   feedbackrepo.ClassificationQualitySignalAggregate
	qualityAggErrs     []error
	qualityValues      []feedbackrepo.ClassificationQualityValueAggregate
	qualitySeries      []feedbackrepo.ClassificationQualitySeriesBucket
	qualitySeriesErr   error
	qualitySamples     []feedbackrepo.ClassificationQualitySample
	qualitySamplesErr  error
	qualityTenant      string
	qualitySampleIDs   []int64
	qualityAggOpts     []feedbackrepo.ClassificationQualityQueryOpts
	qualitySeriesOpts  *feedbackrepo.ClassificationQualityQueryOpts

	classificationReviewLearning     feedbackrepo.ClassificationReviewLearning
	classificationReviewLearningErr  error
	classificationReviewLearningOpts feedbackrepo.ClassificationReviewLearningOpts
	classificationReviewRecord       feedbackrepo.ClassificationReviewEvent
	classificationReviewRecordErr    error
	classificationReviewRecordInput  feedbackrepo.ClassificationReviewRecord

	signalTrace       feedbackrepo.SignalTrace
	signalTraceErr    error
	signalTraceTenant string
	signalTraceID     int64
	signalTraceLimit  int
}

func (f *fakeFeedbackRepo) ListForConsole(
	_ context.Context, tenantID string, opts feedbackrepo.ConsoleListOpts,
) ([]feedbackrepo.ConsoleListRow, error) {
	f.listTenant = tenantID
	f.listOpts = opts
	return f.listRows, f.listErr
}

func (f *fakeFeedbackRepo) GetForConsole(
	_ context.Context, tenantID string, id int64,
) (*feedbackrepo.ConsoleDetailRow, error) {
	f.getTenant = tenantID
	f.getID = id
	return f.getRow, f.getErr
}

func (f *fakeFeedbackRepo) IdentityReviewRows(
	_ context.Context, tenantID string, limit int,
) ([]feedbackrepo.IdentityReviewRow, error) {
	f.identityTenant = tenantID
	f.identityLimit = limit
	return f.identityRows, f.identityErr
}

type fakeIdentityGraph struct {
	in              signalgraphsvc.MergeIdentityReviewInput
	result          signalgraphsvc.MergeIdentityReviewResult
	err             error
	splitIn         signalgraphsvc.SplitIdentityReviewInput
	splitResult     signalgraphsvc.SplitIdentityReviewResult
	splitErr        error
	recent          []signalgraphrepo.RecentMerge
	recentErr       error
	roster          signalgraphrepo.SubjectRoster
	rosterErr       error
	detail          signalgraphrepo.SubjectDetail
	detailErr       error
	detailTenant    string
	detailSubjectID string
	detailLimit     int
}

func (f *fakeIdentityGraph) MergeIdentityReview(
	_ context.Context,
	in signalgraphsvc.MergeIdentityReviewInput,
) (signalgraphsvc.MergeIdentityReviewResult, error) {
	f.in = in
	return f.result, f.err
}

func (f *fakeIdentityGraph) SplitIdentityReview(
	_ context.Context,
	in signalgraphsvc.SplitIdentityReviewInput,
) (signalgraphsvc.SplitIdentityReviewResult, error) {
	f.splitIn = in
	return f.splitResult, f.splitErr
}

func (f *fakeIdentityGraph) RecentIdentityMerges(
	_ context.Context,
	_ string,
	_ int,
) ([]signalgraphrepo.RecentMerge, error) {
	return f.recent, f.recentErr
}

func (f *fakeIdentityGraph) SubjectRoster(
	_ context.Context,
	_ string,
	_ int,
) (signalgraphrepo.SubjectRoster, error) {
	return f.roster, f.rosterErr
}

func (f *fakeIdentityGraph) SubjectDetail(
	_ context.Context,
	tenantID string,
	subjectID string,
	eventLimit int,
) (signalgraphrepo.SubjectDetail, error) {
	f.detailTenant = tenantID
	f.detailSubjectID = subjectID
	f.detailLimit = eventLimit
	return f.detail, f.detailErr
}

func (f *fakeFeedbackRepo) UsageByDay(
	_ context.Context, _ string, _, _ time.Time,
) ([]feedbackrepo.UsageBucket, error) {
	return f.usageRows, f.usageErr
}

func (f *fakeFeedbackRepo) UrgentCount(_ context.Context, _ string, _, _ time.Time) (int64, error) {
	return f.urgent, f.urgentErr
}

func (f *fakeFeedbackRepo) TopValuesByDim(
	_ context.Context, _ string, dim string, _ bool, _, _ time.Time, _ int,
) ([]feedbackrepo.ValueCount, error) {
	return f.topValues[dim], f.topErr
}

func (f *fakeFeedbackRepo) TerminalFailureWorkbench(
	_ context.Context, tenantID string, from, to time.Time,
) (*feedbackrepo.TerminalFailureWorkbench, error) {
	f.workbenchTenant = tenantID
	f.workbenchFrom = from
	f.workbenchTo = to
	return f.workbench, f.workbenchErr
}

func (f *fakeFeedbackRepo) FeedbackTriageCommandCenter(
	_ context.Context,
	tenantID string,
	now time.Time,
) (feedbackrepo.FeedbackTriageCommandCenter, error) {
	f.triageTenant = tenantID
	f.triageNow = now
	return f.triageCenter, f.triageErr
}

func (f *fakeFeedbackRepo) FeedbackAssignmentEscalations(
	_ context.Context,
	tenantID string,
	now time.Time,
	limit int,
) (feedbackrepo.AssignmentEscalationQueue, error) {
	f.assignmentEscalationsTenant = tenantID
	f.assignmentEscalationsNow = now
	f.assignmentEscalationsLimit = limit
	return f.assignmentEscalations, f.assignmentEscalationsErr
}

func (f *fakeFeedbackRepo) RefreshClassificationQuality(
	_ context.Context, opts feedbackrepo.ClassificationQualityRefreshOpts,
) error {
	f.qualityRefreshOpts = opts
	return f.qualityRefreshErr
}

func (f *fakeFeedbackRepo) ClassificationQualityAggregates(
	_ context.Context, opts feedbackrepo.ClassificationQualityQueryOpts,
) (feedbackrepo.ClassificationQualitySignalAggregate, []feedbackrepo.ClassificationQualityValueAggregate, error) {
	f.qualityTenant = opts.TenantID
	call := len(f.qualityAggOpts)
	f.qualityAggOpts = append(f.qualityAggOpts, opts)
	if call < len(f.qualityAggErrs) && f.qualityAggErrs[call] != nil {
		return feedbackrepo.ClassificationQualitySignalAggregate{}, nil, f.qualityAggErrs[call]
	}
	return f.qualityAggregate, f.qualityValues, nil
}

func (f *fakeFeedbackRepo) ClassificationQualitySeries(
	_ context.Context, opts feedbackrepo.ClassificationQualityQueryOpts,
) ([]feedbackrepo.ClassificationQualitySeriesBucket, error) {
	f.qualitySeriesOpts = &opts
	return f.qualitySeries, f.qualitySeriesErr
}

func (f *fakeFeedbackRepo) ClassificationQualitySamples(
	_ context.Context, tenantID string, ids []int64,
) ([]feedbackrepo.ClassificationQualitySample, error) {
	f.qualityTenant = tenantID
	f.qualitySampleIDs = append([]int64(nil), ids...)
	return f.qualitySamples, f.qualitySamplesErr
}

func (f *fakeFeedbackRepo) ClassificationReviewLearning(
	_ context.Context,
	opts feedbackrepo.ClassificationReviewLearningOpts,
) (feedbackrepo.ClassificationReviewLearning, error) {
	f.classificationReviewLearningOpts = opts
	return f.classificationReviewLearning, f.classificationReviewLearningErr
}

func (f *fakeFeedbackRepo) RecordClassificationReview(
	_ context.Context,
	in feedbackrepo.ClassificationReviewRecord,
) (feedbackrepo.ClassificationReviewEvent, error) {
	f.classificationReviewRecordInput = in
	return f.classificationReviewRecord, f.classificationReviewRecordErr
}

func (f *fakeFeedbackRepo) FeedbackSignalTrace(
	_ context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
) (feedbackrepo.SignalTrace, error) {
	f.signalTraceTenant = tenantID
	f.signalTraceID = feedbackID
	f.signalTraceLimit = limit
	return f.signalTrace, f.signalTraceErr
}

func (f *fakeFeedbackRepo) RetryEnrichment(
	_ context.Context, _ string, _ int64,
) (*feedbackrepo.RetryResult, error) {
	return nil, nil
}

type fakeTenantConfigRepo struct {
	cfg tenantrepo.EnrichConfig
	err error
}

func (f *fakeTenantConfigRepo) GetEnrichConfig(_ context.Context, _ string) (tenantrepo.EnrichConfig, error) {
	return f.cfg, f.err
}

func identityEvidenceHasKind(keys []any, kind string) bool {
	for _, raw := range keys {
		item, ok := raw.(map[string]any)
		if ok && item["kind"] == kind {
			return true
		}
	}
	return false
}

type identitySubjectDetailFixture struct {
	graph             *fakeIdentityGraph
	subjectID         uuid.UUID
	identityID        uuid.UUID
	revokedIdentityID uuid.UUID
	eventID           uuid.UUID
}

func newIdentitySubjectDetailFixture(now time.Time) identitySubjectDetailFixture {
	subjectID := uuid.New()
	identityID := uuid.New()
	revokedIdentityID := uuid.New()
	eventID := uuid.New()
	return identitySubjectDetailFixture{
		graph: &fakeIdentityGraph{
			detail: signalgraphrepo.SubjectDetail{
				Subject: signalgraphrepo.Subject{
					ID:                   subjectID,
					DisplayName:          "ada@example.com",
					PrimaryIdentityKind:  "email",
					PrimaryIdentityValue: "ada@example.com",
					Status:               "active",
					IdentityCount:        1,
					EvidenceCount:        2,
					CreatedAt:            now,
					UpdatedAt:            now,
				},
				Identities: []signalgraphrepo.SubjectIdentity{
					{
						ID:               identityID,
						Kind:             "email",
						Value:            "ada@example.com",
						Source:           "review",
						Confidence:       "reviewed",
						EvidenceCount:    2,
						FirstFeedbackID:  201,
						LatestFeedbackID: 202,
						CreatedAt:        now,
						UpdatedAt:        now,
					},
					{
						ID:            revokedIdentityID,
						Kind:          "support_id",
						Value:         "zd-9",
						Source:        "review",
						Confidence:    "reviewed",
						EvidenceCount: 1,
						Revoked:       true,
						RevokedAt:     sql.NullTime{Time: now.Add(time.Hour), Valid: true},
						CreatedAt:     now,
						UpdatedAt:     now.Add(time.Hour),
					},
				},
				Events: []signalgraphrepo.SubjectEvent{{
					ID:            eventID,
					Action:        "review_merge",
					IdentityKind:  "email",
					IdentityValue: "ada@example.com",
					FeedbackIDs:   []int64{201, 202},
					Evidence: []signalgraphrepo.SubjectEventEvidence{{
						ID:         201,
						Source:     "web",
						UserID:     "web-1",
						Content:    "checkout fails for Ada",
						SourceMeta: []byte(`{"email":"ada@example.com"}`),
						CreatedAt:  now.Add(-time.Hour),
					}},
					EvidenceCount: 2,
					Note:          "reviewed",
					CreatedBy:     "user-1",
					CreatedAt:     now,
				}},
			},
		},
		subjectID:         subjectID,
		identityID:        identityID,
		revokedIdentityID: revokedIdentityID,
		eventID:           eventID,
	}
}

func TestHTTPDispatchSmoke(t *testing.T) {
	t.Parallel()

	dims := domain.DimensionSet{
		{Name: "severity", Kind: domain.DimSingle},
		{Name: "labels", Kind: domain.DimMulti},
	}
	tenants := &fakeTenantConfigRepo{cfg: tenantrepo.EnrichConfig{Dimensions: dims}}

	t.Run("list", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			listRows: []feedbackrepo.ConsoleListRow{{
				ID:               123,
				Content:          "payment failed",
				Source:           "api",
				UserID:           "u-1",
				PageURL:          "https://example.com/pay",
				EnrichedTitle:    "Payment failed",
				EnrichedAttrs:    []byte(`{"severity":"critical","labels":["billing"]}`),
				IsUrgent:         true,
				EnrichmentStatus: "done",
				CreatedAt:        time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
				AccountContext: feedbackrepo.AccountContext{
					AccountKey:     "acct:acme",
					AccountDisplay: "Acme Corp",
					Source:         "source_meta",
				},
			}},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return &attunev1.ListFeedbackRequest{} }, BindListRequest),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback?limit=1&q=pay&urgent=true&severity=critical&labels=billing&account_key=acct:acme",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.listTenant)
		require.Equal(t, 1, repo.listOpts.Limit)
		require.Equal(t, "pay", repo.listOpts.Q)
		require.NotNil(t, repo.listOpts.Urgent)
		require.True(t, *repo.listOpts.Urgent)
		require.NotNil(t, repo.listOpts.AccountKey)
		require.Equal(t, "acct:acme", *repo.listOpts.AccountKey)
		require.Len(t, repo.listOpts.Attrs, 2)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "123", body["nextCursor"])
		items := body["items"].([]any)
		require.Equal(t, "123", items[0].(map[string]any)["id"])
		account := items[0].(map[string]any)["accountContext"].(map[string]any)
		require.Equal(t, "acct:acme", account["accountKey"])
		require.Equal(t, "Acme Corp", account["accountDisplay"])
	})

	t.Run("get", func(t *testing.T) {
		enrichedAt := time.Date(2026, 6, 9, 2, 3, 4, 0, time.UTC)
		repo := &fakeFeedbackRepo{
			getRow: &feedbackrepo.ConsoleDetailRow{
				ConsoleListRow: feedbackrepo.ConsoleListRow{
					ID:               123,
					Content:          "payment failed",
					Source:           "api",
					UserID:           "u-1",
					PageURL:          "https://example.com/pay",
					EnrichedTitle:    "Payment failed",
					EnrichedAttrs:    []byte(`{"severity":"critical"}`),
					IsUrgent:         true,
					EnrichmentStatus: "done",
					CreatedAt:        time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
					AccountContext: feedbackrepo.AccountContext{
						AccountKey:     "acct:acme",
						AccountDisplay: "Acme Corp",
						Source:         "source_meta",
					},
				},
				SourceMeta:        []byte(`{"browser":"safari","email":"ada@example.com","externalId":"ext-42","contact":{"sourceContactId":"contact-7"}}`),
				Attachments:       []byte(`[{"url":"https://example.com/a.png","mime":"image/png","size":42}]`),
				EnrichedAt:        &enrichedAt,
				EnrichedRationale: "critical checkout failure",
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.Get",
			dispatcher.Path(
				func() *attunev1.GetFeedbackRequest { return &attunev1.GetFeedbackRequest{} },
				dispatcher.ParamInt64("id", func(req *attunev1.GetFeedbackRequest, id int64) { req.Id = id }, "id must be an integer"),
			),
			h.Get,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback/123",
			"",
			dispatchtest.Param{Name: "id", Value: "123"},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, int64(123), repo.getID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "123", body["id"])
		require.Equal(t, "critical checkout failure", body["enrichedRationale"])
		account := body["accountContext"].(map[string]any)
		require.Equal(t, "acct:acme", account["accountKey"])
		require.Equal(t, "Acme Corp", account["accountDisplay"])
		require.Equal(t, "safari", body["sourceMeta"].(map[string]any)["browser"])
		identity := body["identityEvidence"].(map[string]any)
		require.Equal(t, "u-1", identity["sourceUser"])
		require.Equal(t, float64(4), identity["mergeCandidateCount"])
		require.Equal(t, true, identity["hasEmail"])
		require.Equal(t, true, identity["hasExternalId"])
		require.Equal(t, true, identity["hasSourceContactId"])
		assessment := identity["assessment"].(map[string]any)
		require.Equal(t, "FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG", assessment["strength"])
		require.Equal(t, "FEEDBACK_IDENTITY_RECOMMENDED_ACTION_REVIEW_MERGE", assessment["recommendedAction"])
		require.Equal(t, float64(3), assessment["stableKeyCount"])
		require.Contains(t, assessment["missingKinds"], "crm_id")
	})

	t.Run("signal trace", func(t *testing.T) {
		now := time.Date(2026, 8, 1, 3, 4, 5, 0, time.UTC)
		repo := &fakeFeedbackRepo{
			signalTrace: feedbackrepo.SignalTrace{
				FeedbackID:     123,
				SignalTraceID:  "trace-123",
				Source:         "api",
				TerminalStatus: "completed",
				Complete:       true,
				GeneratedAt:    now,
				Stages: []feedbackrepo.SignalTraceStage{{
					Key:         feedbackrepo.SignalTraceStageSource,
					Label:       "Source event",
					Status:      "completed",
					EventCount:  1,
					LastEventAt: ptrext.Of(now),
				}},
				Events: []feedbackrepo.SignalTraceEvent{{
					Stage:      feedbackrepo.SignalTraceStageSource,
					Kind:       "source_captured",
					Status:     "completed",
					TraceID:    "trace-123",
					Summary:    "Feedback source event captured",
					OccurredAt: now,
					Metadata:   map[string]any{"source": "api"},
				}},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetSignalTrace",
			dispatcher.Combine(
				func() *attunev1.GetFeedbackSignalTraceRequest { return &attunev1.GetFeedbackSignalTraceRequest{} },
				dispatcher.ParamInt64("id", func(req *attunev1.GetFeedbackSignalTraceRequest, id int64) { req.FeedbackId = id }, "id must be an integer"),
				BindFeedbackSignalTraceRequest,
			),
			h.GetSignalTrace,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackSignalTraceRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback/123/signal-trace?limit=7",
			"",
			dispatchtest.Param{Name: "id", Value: "123"},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.signalTraceTenant)
		require.Equal(t, int64(123), repo.signalTraceID)
		require.Equal(t, 7, repo.signalTraceLimit)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "123", body["feedbackId"])
		require.Equal(t, "trace-123", body["signalTraceId"])
		require.Equal(t, true, body["complete"])
		stages := body["stages"].([]any)
		require.Equal(t, "source_event", stages[0].(map[string]any)["key"])
		events := body["events"].([]any)
		require.Equal(t, "source_captured", events[0].(map[string]any)["kind"])
		require.Equal(t, "api", events[0].(map[string]any)["metadata"].(map[string]any)["source"])
	})

	t.Run("identity review exposes merge candidates and weak evidence", func(t *testing.T) {
		now := time.Date(2026, 7, 7, 1, 2, 3, 0, time.UTC)
		repo := &fakeFeedbackRepo{
			identityRows: []feedbackrepo.IdentityReviewRow{
				{ID: 201, Source: "web", UserID: "web-1", Content: "checkout fails for Ada", SourceMeta: []byte(`{"email":"ada@example.com","externalId":"ext-1"}`), CreatedAt: now},
				{ID: 202, Source: "support", UserID: "ticket-2", Content: "Ada reported the same checkout failure", SourceMeta: []byte(`{"email":"ada@example.com","supportId":"zd-9"}`), CreatedAt: now.Add(-time.Hour)},
				{ID: 203, Source: "api", UserID: "anonymous-3", Content: "cannot identify this user yet", SourceMeta: []byte(`{}`), CreatedAt: now.Add(-2 * time.Hour)},
			},
		}
		h := &FeedbackHandler{repo: repo}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetIdentityReview",
			dispatcher.Empty(func() *attunev1.GetFeedbackIdentityReviewRequest {
				return &attunev1.GetFeedbackIdentityReviewRequest{}
			}),
			h.GetIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/identity-review", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "tenant-1", repo.identityTenant)
		require.Equal(t, identityReviewScanLimit, repo.identityLimit)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		summary := body["summary"].(map[string]any)
		require.Equal(t, float64(3), summary["scannedFeedbackCount"])
		require.Equal(t, float64(1), summary["mergeCandidateCount"])
		require.Equal(t, float64(1), summary["needsEvidenceCount"])
		candidate := body["mergeCandidates"].([]any)[0].(map[string]any)
		require.Equal(t, "email", candidate["identityKind"])
		require.Equal(t, "ada@example.com", candidate["identityValue"])
		require.Equal(t, "FEEDBACK_IDENTITY_RECOMMENDED_ACTION_REVIEW_MERGE", candidate["recommendedAction"])
		needsEvidence := body["needsEvidence"].([]any)[0].(map[string]any)
		assessment := needsEvidence["assessment"].(map[string]any)
		require.Equal(t, "FEEDBACK_IDENTITY_RECOMMENDED_ACTION_CAPTURE_MORE_KEYS", assessment["recommendedAction"])
	})

	t.Run("identity review includes recent durable merges and subject roster", func(t *testing.T) {
		subjectID := uuid.New()
		eventID := uuid.New()
		now := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
		subject := signalgraphrepo.Subject{
			ID:                   subjectID,
			DisplayName:          "ada@example.com",
			PrimaryIdentityKind:  "email",
			PrimaryIdentityValue: "ada@example.com",
			Status:               "active",
			IdentityCount:        1,
			EvidenceCount:        2,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		repo := &fakeFeedbackRepo{}
		graph := &fakeIdentityGraph{
			recent: []signalgraphrepo.RecentMerge{{
				EventID:       eventID,
				IdentityKind:  "email",
				IdentityValue: "ada@example.com",
				FeedbackIDs:   []int64{201, 202},
				EvidenceCount: 2,
				CreatedBy:     "user-1",
				CreatedAt:     now,
				Subject:       subject,
			}},
			roster: signalgraphrepo.SubjectRoster{
				ActiveSubjectCount:  1,
				ActiveIdentityCount: 1,
				EvidenceCount:       2,
				Subjects:            []signalgraphrepo.Subject{subject},
			},
		}
		h := &FeedbackHandler{repo: repo, identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetIdentityReview",
			dispatcher.Empty(func() *attunev1.GetFeedbackIdentityReviewRequest {
				return &attunev1.GetFeedbackIdentityReviewRequest{}
			}),
			h.GetIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/identity-review", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		recent := body["recentMerges"].([]any)[0].(map[string]any)
		require.Equal(t, eventID.String(), recent["eventId"])
		require.Equal(t, "email", recent["identityKind"])
		require.Equal(t, "ada@example.com", recent["identityValue"])
		recentSubject := recent["subject"].(map[string]any)
		require.Equal(t, subjectID.String(), recentSubject["id"])
		roster := body["subjectRoster"].(map[string]any)
		require.Equal(t, float64(1), roster["activeSubjectCount"])
		require.Equal(t, float64(1), roster["activeIdentityCount"])
		require.Equal(t, float64(2), roster["evidenceCount"])
		rosterSubject := roster["subjects"].([]any)[0].(map[string]any)
		require.Equal(t, subjectID.String(), rosterSubject["id"])
	})

	t.Run("identity subject detail exposes identities and timeline", func(t *testing.T) {
		now := time.Date(2026, 8, 1, 2, 3, 4, 0, time.UTC)
		fixture := newIdentitySubjectDetailFixture(now)
		graph := fixture.graph
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetIdentitySubject",
			dispatcher.Path(
				func() *attunev1.GetFeedbackIdentitySubjectRequest {
					return &attunev1.GetFeedbackIdentitySubjectRequest{}
				},
				dispatcher.Param("subject_id", func(req *attunev1.GetFeedbackIdentitySubjectRequest, id string) {
					req.SubjectId = id
				}),
			),
			h.GetIdentitySubject,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackIdentitySubjectRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback/identity-review/subjects/"+fixture.subjectID.String(),
			"",
			dispatchtest.Param{Name: "subject_id", Value: fixture.subjectID.String()},
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "tenant-1", graph.detailTenant)
		require.Equal(t, fixture.subjectID.String(), graph.detailSubjectID)
		require.Equal(t, identityReviewEventLimit, graph.detailLimit)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		subject := body["subject"].(map[string]any)
		require.Equal(t, fixture.subjectID.String(), subject["id"])
		identities := body["identities"].([]any)
		activeIdentity := identities[0].(map[string]any)
		require.Equal(t, fixture.identityID.String(), activeIdentity["id"])
		require.Equal(t, false, activeIdentity["revoked"])
		revokedIdentity := identities[1].(map[string]any)
		require.Equal(t, fixture.revokedIdentityID.String(), revokedIdentity["id"])
		require.Equal(t, true, revokedIdentity["revoked"])
		events := body["events"].([]any)
		event := events[0].(map[string]any)
		require.Equal(t, fixture.eventID.String(), event["id"])
		require.Equal(t, "review_merge", event["action"])
		require.Equal(t, []any{"201", "202"}, event["feedbackIds"])
		evidence := event["evidence"].([]any)[0].(map[string]any)
		require.Equal(t, "201", evidence["feedbackId"])
		require.Equal(t, "web", evidence["source"])
		require.Equal(t, "checkout fails for Ada", evidence["excerpt"])
		keys := evidence["keys"].([]any)
		require.True(t, identityEvidenceHasKind(keys, "email"))
	})

	t.Run("identity subject detail maps not found", func(t *testing.T) {
		subjectID := uuid.New()
		graph := &fakeIdentityGraph{detailErr: signalgraphsvc.ErrSubjectNotFound}
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetIdentitySubject",
			dispatcher.Path(
				func() *attunev1.GetFeedbackIdentitySubjectRequest {
					return &attunev1.GetFeedbackIdentitySubjectRequest{}
				},
				dispatcher.Param("subject_id", func(req *attunev1.GetFeedbackIdentitySubjectRequest, id string) {
					req.SubjectId = id
				}),
			),
			h.GetIdentitySubject,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackIdentitySubjectRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback/identity-review/subjects/"+subjectID.String(),
			"",
			dispatchtest.Param{Name: "subject_id", Value: subjectID.String()},
		))

		require.Equal(t, http.StatusNotFound, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "NOT_FOUND", body["code"])
	})

	t.Run("identity review merge persists signal subject", func(t *testing.T) {
		subjectID := uuid.New()
		now := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
		graph := &fakeIdentityGraph{
			result: signalgraphsvc.MergeIdentityReviewResult{
				Subject: signalgraphrepo.Subject{
					ID:                   subjectID,
					DisplayName:          "Ada Lovelace",
					PrimaryIdentityKind:  "email",
					PrimaryIdentityValue: "ada@example.com",
					Status:               "active",
					IdentityCount:        1,
					EvidenceCount:        2,
					CreatedAt:            now,
					UpdatedAt:            now,
				},
				EvidenceCount:  2,
				CreatedSubject: true,
			},
		}
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.MergeIdentityReview",
			dispatcher.JSON(func() *attunev1.MergeFeedbackIdentityReviewRequest {
				return &attunev1.MergeFeedbackIdentityReviewRequest{}
			}),
			h.MergeIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.MergeFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/feedback/identity-review/merge",
			`{"identityKind":"email","identityValue":"ada@example.com","feedbackIds":["201","202"],"note":"reviewed"}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "tenant-1", graph.in.TenantID)
		require.Equal(t, "email", graph.in.IdentityKind)
		require.Equal(t, "ada@example.com", graph.in.IdentityValue)
		require.Equal(t, []int64{201, 202}, graph.in.FeedbackIDs)
		require.Equal(t, "reviewed", graph.in.Note)
		require.Equal(t, "user-1", graph.in.Actor.ID)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "signal_subject.merge", body["action"])
		require.Equal(t, true, body["createdSubject"])
		subject := body["subject"].(map[string]any)
		require.Equal(t, subjectID.String(), subject["id"])
		require.Equal(t, "Ada Lovelace", subject["displayName"])
		require.Equal(t, float64(1), subject["identityCount"])
		require.Equal(t, float64(2), subject["evidenceCount"])
	})

	t.Run("identity review merge maps validation errors", func(t *testing.T) {
		graph := &fakeIdentityGraph{err: signalgraphsvc.ErrValidation}
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.MergeIdentityReview",
			dispatcher.JSON(func() *attunev1.MergeFeedbackIdentityReviewRequest {
				return &attunev1.MergeFeedbackIdentityReviewRequest{}
			}),
			h.MergeIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.MergeFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/feedback/identity-review/merge",
			`{"identityKind":"source_user","identityValue":"u-1","feedbackIds":["201","202"]}`,
		))

		require.Equal(t, http.StatusBadRequest, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "VALIDATION", body["code"])
	})

	t.Run("identity review split revokes durable merge", func(t *testing.T) {
		subjectID := uuid.New()
		now := time.Date(2026, 8, 1, 1, 2, 3, 0, time.UTC)
		graph := &fakeIdentityGraph{
			splitResult: signalgraphsvc.SplitIdentityReviewResult{
				Subject: signalgraphrepo.Subject{
					ID:            subjectID,
					DisplayName:   "ada@example.com",
					Status:        "active",
					IdentityCount: 0,
					EvidenceCount: 0,
					CreatedAt:     now,
					UpdatedAt:     now,
				},
				EvidenceCount: 2,
			},
		}
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.SplitIdentityReview",
			dispatcher.JSON(func() *attunev1.SplitFeedbackIdentityReviewRequest {
				return &attunev1.SplitFeedbackIdentityReviewRequest{}
			}),
			h.SplitIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SplitFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/feedback/identity-review/split",
			`{"subjectId":"`+subjectID.String()+`","identityKind":"email","identityValue":"ada@example.com","note":"wrong person"}`,
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "tenant-1", graph.splitIn.TenantID)
		require.Equal(t, subjectID.String(), graph.splitIn.SubjectID)
		require.Equal(t, "email", graph.splitIn.IdentityKind)
		require.Equal(t, "ada@example.com", graph.splitIn.IdentityValue)
		require.Equal(t, "wrong person", graph.splitIn.Note)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "signal_subject.split", body["action"])
		require.Equal(t, float64(2), body["evidenceCount"])
	})

	t.Run("identity review split maps not found errors", func(t *testing.T) {
		graph := &fakeIdentityGraph{splitErr: signalgraphsvc.ErrIdentityNotFound}
		h := &FeedbackHandler{identityGraph: graph}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.SplitIdentityReview",
			dispatcher.JSON(func() *attunev1.SplitFeedbackIdentityReviewRequest {
				return &attunev1.SplitFeedbackIdentityReviewRequest{}
			}),
			h.SplitIdentityReview,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SplitFeedbackIdentityReviewRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodPost,
			"/fb/v1/console/feedback/identity-review/split",
			`{"subjectId":"`+uuid.New().String()+`","identityKind":"email","identityValue":"ada@example.com"}`,
		))

		require.Equal(t, http.StatusNotFound, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "NOT_FOUND", body["code"])
	})

	t.Run("list with enrichment_status filter", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			listRows: []feedbackrepo.ConsoleListRow{{
				ID:               456,
				Content:          "error occurred",
				Source:           "sdk",
				EnrichmentStatus: "failed",
				CreatedAt:        time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return &attunev1.ListFeedbackRequest{} }, BindListRequest),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback?enrichment_status=failed",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, repo.listOpts.EnrichmentStatus)
		require.Equal(t, "failed", *repo.listOpts.EnrichmentStatus)
	})

	t.Run("list with terminal_failed_only filter", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			listRows: []feedbackrepo.ConsoleListRow{{
				ID:                 789,
				Content:            "terminal failure",
				Source:             "api",
				EnrichmentStatus:   "failed",
				EnrichmentAttempts: 5,
				CreatedAt:          time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC),
			}},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return &attunev1.ListFeedbackRequest{} }, BindListRequest),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback?terminal_failed_only=true",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, repo.listOpts.TerminalFailedOnly)
		require.True(t, *repo.listOpts.TerminalFailedOnly)
	})

	t.Run("list with combined terminal failure filters", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			listRows: []feedbackrepo.ConsoleListRow{},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.List",
			dispatcher.Query(func() *attunev1.ListFeedbackRequest { return &attunev1.ListFeedbackRequest{} }, BindListRequest),
			h.List,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.ListFeedbackRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(
			http.MethodGet,
			"/fb/v1/console/feedback?enrichment_status=failed&terminal_failed_only=true&limit=50",
			"",
		))

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, repo.listOpts.EnrichmentStatus)
		require.Equal(t, "failed", *repo.listOpts.EnrichmentStatus)
		require.NotNil(t, repo.listOpts.TerminalFailedOnly)
		require.True(t, *repo.listOpts.TerminalFailedOnly)
		require.Equal(t, 50, repo.listOpts.Limit)
	})

	t.Run("terminal workbench", func(t *testing.T) {
		oldest := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		repo := &fakeFeedbackRepo{
			workbench: &feedbackrepo.TerminalFailureWorkbench{
				PeriodStart:           time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
				PeriodEnd:             time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
				TotalTerminalFailures: 4,
				OldestCreatedAt:       &oldest,
				ReasonClassClusters: []feedbackrepo.TerminalFailureCluster{
					{
						Key:               "llm_err",
						Label:             "LLM error",
						Count:             3,
						OldestCreatedAt:   oldest,
						NewestCreatedAt:   oldest.Add(2 * time.Hour),
						SampleFeedbackIDs: []int64{123, 124, 125},
						RemediationHint:   "Check the routed LLM channel and provider health.",
					},
				},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetTerminalFailureWorkbench",
			dispatcher.Empty(func() *attunev1.GetTerminalFailureWorkbenchRequest {
				return &attunev1.GetTerminalFailureWorkbenchRequest{}
			}),
			h.GetTerminalFailureWorkbench,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetTerminalFailureWorkbenchRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/terminal-failures", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.workbenchTenant)
		require.False(t, repo.workbenchFrom.IsZero())
		require.False(t, repo.workbenchTo.IsZero())
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "4", body["totalTerminalFailures"])
		clusters := body["reasonClassClusters"].([]any)
		require.Len(t, clusters, 1)
		cluster := clusters[0].(map[string]any)
		require.Equal(t, "llm_err", cluster["key"])
		require.Equal(t, "LLM error", cluster["label"])
	})

	t.Run("triage command center exposes accountable lanes", func(t *testing.T) {
		oldest := time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
		deadline := oldest.Add(24 * time.Hour)
		repo := &fakeFeedbackRepo{
			triageCenter: feedbackrepo.FeedbackTriageCommandCenter{
				GeneratedAt:          time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
				OpenCount:            9,
				ActiveCount:          4,
				ClosedCount:          2,
				UrgentOpenCount:      3,
				TerminalFailureCount: 1,
				IdentityDebtCount:    5,
				OverdueCount:         2,
				DueSoonCount:         1,
				Lanes: []feedbackrepo.FeedbackTriageLane{{
					Key:               "urgent_open",
					Label:             "Urgent open feedback",
					OwnerLane:         "support_triage",
					Severity:          "critical",
					SLAHours:          24,
					Count:             3,
					OverdueCount:      2,
					DueSoonCount:      1,
					OldestCreatedAt:   &oldest,
					NextDeadlineAt:    &deadline,
					RecommendedAction: "Open the oldest urgent samples.",
					FilterQuery:       "urgent=true",
					SampleFeedbackIDs: []int64{301, 302},
				}},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetTriageCommandCenter",
			dispatcher.Empty(func() *attunev1.GetFeedbackTriageCommandCenterRequest {
				return &attunev1.GetFeedbackTriageCommandCenterRequest{}
			}),
			h.GetTriageCommandCenter,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackTriageCommandCenterRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/triage-command-center", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.triageTenant)
		require.False(t, repo.triageNow.IsZero())
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "3", body["urgentOpenCount"])
		lanes := body["lanes"].([]any)
		lane := lanes[0].(map[string]any)
		require.Equal(t, "urgent_open", lane["key"])
		require.Equal(t, "support_triage", lane["ownerLane"])
		require.Equal(t, float64(24), lane["slaHours"])
		require.Equal(t, []any{"301", "302"}, lane["sampleFeedbackIds"])
	})

	t.Run("assignment escalations expose durable SLA work", func(t *testing.T) {
		generatedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		dueAt := generatedAt.Add(-2 * time.Hour)
		repo := &fakeFeedbackRepo{
			assignmentEscalations: feedbackrepo.AssignmentEscalationQueue{
				GeneratedAt:       generatedAt,
				OverdueCount:      2,
				DueSoonCount:      1,
				MissingOwnerCount: 1,
				MissingSLACount:   1,
				Items: []feedbackrepo.AssignmentEscalation{{
					FeedbackID: 42,
					Title:      "Enterprise login regression",
					Source:     "portal",
					Type:       "bug",
					IsUrgent:   true,
					CreatedAt:  generatedAt.Add(-24 * time.Hour),
					Assignment: feedbackrepo.Assignment{
						FeedbackID: 42,
						SLADueAt:   ptrext.Of(dueAt),
						Note:       "Escalate before renewal call.",
					},
					Reasons:       []string{"overdue", "missing_owner"},
					HoursUntilDue: ptrext.Of(-2),
					Priority:      "critical",
					Account: feedbackrepo.AccountContext{
						AccountKey:     "acct:acme",
						AccountDisplay: "Acme Corp",
						Source:         "source_meta",
					},
				}},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.GetFeedbackAssignmentEscalations",
			dispatcher.Query(
				func() *attunev1.GetFeedbackAssignmentEscalationsRequest {
					return ptrext.Of(attunev1.GetFeedbackAssignmentEscalationsRequest{})
				},
				func(r *http.Request, req *attunev1.GetFeedbackAssignmentEscalationsRequest) error {
					lim := r.URL.Query().Get("limit")
					if lim == "" {
						return nil
					}
					req.Limit = ptrext.Of(int32(7))
					return nil
				},
			),
			h.GetFeedbackAssignmentEscalations,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackAssignmentEscalationsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/assignment/escalations?limit=7", ""))

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, dispatchtest.TenantID, repo.assignmentEscalationsTenant)
		require.False(t, repo.assignmentEscalationsNow.IsZero())
		require.Equal(t, 7, repo.assignmentEscalationsLimit)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "2", body["overdueCount"])
		require.Equal(t, "1", body["dueSoonCount"])
		require.Equal(t, "1", body["missingOwnerCount"])
		require.Equal(t, "1", body["missingSlaCount"])
		items := body["items"].([]any)
		require.Len(t, items, 1)
		item := items[0].(map[string]any)
		require.Equal(t, "42", item["feedbackId"])
		require.Equal(t, "critical", item["priority"])
		require.Equal(t, []any{"overdue", "missing_owner"}, item["escalationReasons"])
		require.Equal(t, float64(-2), item["hoursUntilDue"])
		account := item["accountContext"].(map[string]any)
		require.Equal(t, "acct:acme", account["accountKey"])
		require.Equal(t, "Acme Corp", account["accountDisplay"])
		assignment := item["assignment"].(map[string]any)
		require.Equal(t, "overdue", assignment["slaStatus"])
	})

	t.Run("stats", func(t *testing.T) {
		repo := &fakeFeedbackRepo{
			usageRows: []feedbackrepo.UsageBucket{
				{Bucket: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Value: 3},
				{Bucket: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Value: 4},
			},
			urgent: 2,
			topValues: map[string][]feedbackrepo.ValueCount{
				"severity": {{Value: "critical", Count: 5}},
				"labels":   {{Value: "billing", Count: 4}},
			},
		}
		h := &FeedbackHandler{repo: repo, tenants: tenants}
		handler := dispatcher.Bind(
			"console.FeedbackHandler.Stats",
			dispatcher.Empty(func() *attunev1.GetFeedbackStatsRequest { return &attunev1.GetFeedbackStatsRequest{} }),
			h.Stats,
			dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetFeedbackStatsRequest) (*session.AuthCtx, error) {
				return dispatchtest.Auth(r.Context()), nil
			}),
		)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodGet, "/fb/v1/console/feedback/stats", ""))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		require.Equal(t, "7", body["total"])
		require.Equal(t, "2", body["urgentCount"])
		require.Len(t, body["dims"].([]any), 2)
	})
}
