//go:build integration

package feedback_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/feedback"
	feedbackauditrepo "github.com/Phixsura/attune/internal/repo/feedbackaudit"
	systemsettingsrepo "github.com/Phixsura/attune/internal/repo/systemsettings"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	feedbackassignmentsvc "github.com/Phixsura/attune/internal/service/feedbackassignment"
	"github.com/Phixsura/attune/internal/testdb"
)

// seedTenantAndRow inserts a tenant + a pending user_feedback row,
// returning the row id. The tenant is the demo seed shape, so the
// migration's column DEFAULT plants the standard semantic dimensions.
func seedTenantAndRow(t *testing.T, pool *pgxpool.Pool, content string) (tenantID string, rowID int64) {
	t.Helper()
	ctx := context.Background()
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('demo','Demo Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', $2)
		RETURNING id`, tenantID, content).Scan(&rowID)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}
	return tenantID, rowID
}

func TestPG_MigrationSeedsDefaultDims(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, _ := seedTenantAndRow(t, pool, "test content")

	var dimsRaw []byte
	err := pool.QueryRow(context.Background(),
		"SELECT enrich_dimensions FROM tenants WHERE id = $1", tenantID).Scan(&dimsRaw)
	if err != nil {
		t.Fatal(err)
	}
	var dims domain.DimensionSet
	if err := json.Unmarshal(dimsRaw, &dims); err != nil {
		t.Fatal(err)
	}
	if len(dims) != 4 {
		t.Fatalf("expected 4 default dims, got %d", len(dims))
	}
	if err := dims.Validate(); err != nil {
		t.Fatalf("seeded dimensions must validate: %v", err)
	}
	names := []string{}
	for _, d := range dims {
		names = append(names, d.Name)
	}
	want := map[string]bool{"type": true, "severity": true, "sentiment": true, "labels": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected default dim: %s", n)
		}
	}
	pack := domain.CustomerFeedbackPackV1()
	if pack.Name != feedback.DefaultDomainPack {
		t.Fatalf("domain pack constant drift: %s != %s", pack.Name, feedback.DefaultDomainPack)
	}
	sentiment, ok := dims.ByName("sentiment")
	if !ok {
		t.Fatal("sentiment dim missing")
	}
	packSentiment, ok := pack.Dimensions.ByName("sentiment")
	if !ok {
		t.Fatal("pack sentiment dim missing")
	}
	if sentiment.DisplayName["default"] != packSentiment.DisplayName["default"] ||
		sentiment.Renderer.Kind != packSentiment.Renderer.Kind ||
		len(sentiment.Taxonomy) != len(packSentiment.Taxonomy) {
		t.Fatalf("seeded sentiment drifted from pack: got=%+v want=%+v", sentiment, packSentiment)
	}
}

func TestPG_TryClaim_FlipsStatusOnce(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "x")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	ok, err := repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	// Second claim on a freshly-claimed row must lose.
	ok2, err := repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if ok2 {
		t.Error("second claim within 5min should lose contention")
	}
}

func TestPG_AssignFeedbackPersistsOwnerSLAAndConsoleProjection(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "assignable feedback")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	var ownerID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, email, role, role_source, accepted_at
		)
		VALUES ($1, 'tenant_user', 'pm-user', 'pm@example.com', 'member', 'manual', NOW())
		RETURNING id::text`,
		tenantID,
	).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert tenant member: %v", err)
	}
	if err := repo.ValidateAssignmentOwner(ctx, tenantID, ownerID); err != nil {
		t.Fatalf("validate assignment owner: %v", err)
	}

	dueAt := time.Date(2026, 6, 26, 9, 30, 0, 0, time.UTC)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin assignment tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	assigned, err := repo.AssignFeedbackTx(ctx, tx, tenantID, id, feedback.AssignmentInput{
		OwnerMemberIDSet: true,
		OwnerMemberID:    ptrext.Of(ownerID),
		SLADueAtSet:      true,
		SLADueAt:         ptrext.Of(dueAt),
		Note:             "Own release readiness.",
		ActorID:          "operator-1",
	})
	if err != nil {
		t.Fatalf("assign feedback: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit assignment tx: %v", err)
	}
	if got := ptrext.IndirectOr(assigned.OwnerMemberID, ""); got != ownerID {
		t.Fatalf("assigned owner = %q, want %q", got, ownerID)
	}

	detail, err := repo.GetForConsole(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("get console feedback: %v", err)
	}
	if got := ptrext.IndirectOr(detail.Assignment.OwnerMemberID, ""); got != ownerID {
		t.Fatalf("projected owner = %q, want %q", got, ownerID)
	}
	if detail.Assignment.OwnerEmail != "pm@example.com" {
		t.Fatalf("projected owner email = %q", detail.Assignment.OwnerEmail)
	}
	if detail.Assignment.AssignedBy != "operator-1" {
		t.Fatalf("projected assigned_by = %q", detail.Assignment.AssignedBy)
	}
	if detail.Assignment.Note != "Own release readiness." {
		t.Fatalf("projected note = %q", detail.Assignment.Note)
	}
	if detail.Assignment.SLADueAt == nil || !detail.Assignment.SLADueAt.Equal(dueAt) {
		t.Fatalf("projected due_at = %v, want %v", detail.Assignment.SLADueAt, dueAt)
	}
}

func TestPG_AssignmentEscalationQueuePrioritizesDurableSLAWork(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, overdueID := seedTenantAndRow(t, pool, "overdue enterprise blocker")
	_, missingOwnerID := seedTenantAndRow(t, pool, "unowned renewal blocker")
	_, missingSLAID := seedTenantAndRow(t, pool, "owned work missing commitment")
	_, dueSoonID := seedTenantAndRow(t, pool, "deadline approaching")
	_, healthyID := seedTenantAndRow(t, pool, "healthy assigned feedback")
	_, closedID := seedTenantAndRow(t, pool, "closed item should not escalate")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)
	ownerID := seedAssignmentMember(t, ctx, pool, tenantID, "escalation-owner", "escalation-owner@example.com")
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	prepareAssignmentEscalationFeedback(t, ctx, pool, overdueID, assignmentEscalationSeed{
		Title:         "Overdue enterprise blocker",
		Source:        "portal",
		FeedbackType:  "bug",
		CreatedAt:     now.Add(-30 * time.Hour),
		IsUrgent:      true,
		OwnerMemberID: ptrext.Of(ownerID),
		SLADueAt:      ptrext.Of(now.Add(-2 * time.Hour)),
	})
	prepareAssignmentEscalationFeedback(t, ctx, pool, missingOwnerID, assignmentEscalationSeed{
		Title:        "Unowned renewal blocker",
		Source:       "github",
		FeedbackType: "bug",
		CreatedAt:    now.Add(-20 * time.Hour),
		IsUrgent:     false,
		SLADueAt:     ptrext.Of(now.Add(24 * time.Hour)),
	})
	prepareAssignmentEscalationFeedback(t, ctx, pool, missingSLAID, assignmentEscalationSeed{
		Title:         "Owned work missing commitment",
		Source:        "api",
		FeedbackType:  "question",
		CreatedAt:     now.Add(-18 * time.Hour),
		OwnerMemberID: ptrext.Of(ownerID),
	})
	prepareAssignmentEscalationFeedback(t, ctx, pool, dueSoonID, assignmentEscalationSeed{
		Title:         "Deadline approaching",
		Source:        "lark",
		FeedbackType:  "feature",
		CreatedAt:     now.Add(-12 * time.Hour),
		IsUrgent:      true,
		OwnerMemberID: ptrext.Of(ownerID),
		SLADueAt:      ptrext.Of(now.Add(3 * time.Hour)),
	})
	prepareAssignmentEscalationFeedback(t, ctx, pool, healthyID, assignmentEscalationSeed{
		Title:         "Healthy assigned feedback",
		Source:        "api",
		FeedbackType:  "feature",
		CreatedAt:     now.Add(-10 * time.Hour),
		OwnerMemberID: ptrext.Of(ownerID),
		SLADueAt:      ptrext.Of(now.Add(48 * time.Hour)),
	})
	closedStateID := seedClosedWorkflowState(t, ctx, pool, tenantID)
	prepareAssignmentEscalationFeedback(t, ctx, pool, closedID, assignmentEscalationSeed{
		Title:        "Closed item should not escalate",
		Source:       "api",
		FeedbackType: "bug",
		CreatedAt:    now.Add(-72 * time.Hour),
		IsUrgent:     true,
		StateID:      ptrext.Of(closedStateID),
	})

	queue, err := repo.FeedbackAssignmentEscalations(ctx, tenantID, now, 10)
	if err != nil {
		t.Fatalf("feedback assignment escalations: %v", err)
	}
	assertAssignmentEscalationQueue(t, queue, []int64{overdueID, missingOwnerID, missingSLAID, dueSoonID})

	limited, err := repo.FeedbackAssignmentEscalations(ctx, tenantID, now, 2)
	if err != nil {
		t.Fatalf("limited feedback assignment escalations: %v", err)
	}
	assertAssignmentEscalationQueue(t, limited, []int64{overdueID, missingOwnerID})
}

func TestPG_BatchAssignFeedbackPersistsOwnerSLAAndAudit(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, firstID := seedTenantAndRow(t, pool, "first batch assignable feedback")
	_, secondID := seedTenantAndRow(t, pool, "second batch assignable feedback")
	repo := feedback.NewFeedback(pool)
	auditRepo := feedbackauditrepo.New(pool)
	service := feedbackassignmentsvc.New(repo, auditRepo, pool)
	ctx := context.Background()
	ownerID := seedAssignmentMember(t, ctx, pool, tenantID, "batch-pm-user", "batch-pm@example.com")

	dueAt := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	missingID := secondID + 1_000_000
	result, err := service.AssignBatch(ctx, feedbackassignmentsvc.BatchInput{
		TenantID:         tenantID,
		FeedbackIDs:      []int64{firstID, secondID, firstID, missingID},
		OwnerMemberIDSet: true,
		OwnerMemberID:    ptrext.Of(ownerID),
		SLADueAtSet:      true,
		SLADueAt:         ptrext.Of(dueAt),
		Note:             "Batch owner handoff.",
		ActorID:          "operator-2",
	})
	if err != nil {
		t.Fatalf("batch assign feedback: %v", err)
	}
	assertBatchAssignmentResult(t, result, missingID)

	detail, err := repo.GetForConsole(ctx, tenantID, firstID)
	if err != nil {
		t.Fatalf("get console feedback: %v", err)
	}
	assertBatchAssignmentProjection(t, detail, ownerID, dueAt)

	entries, _, err := auditRepo.List(ctx, tenantID, firstID, "", 10)
	if err != nil {
		t.Fatalf("list assignment audit: %v", err)
	}
	assertBatchAssignmentAudit(t, entries)
}

func TestPG_ApplyAssignmentRecommendationsPersistsSLAAndAudit(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, urgentID := seedTenantAndRow(t, pool, "urgent policy feedback")
	_, coveredID := seedTenantAndRow(t, pool, "covered policy feedback")
	repo := feedback.NewFeedback(pool)
	auditRepo := feedbackauditrepo.New(pool)
	service := feedbackassignmentsvc.New(repo, auditRepo, pool)
	ctx := context.Background()

	createdAt := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	prepareRecommendedFeedback(t, ctx, pool, urgentID, createdAt, true, nil, "urgent@example.com")
	strictDue := createdAt.Add(70 * time.Hour)
	prepareRecommendedFeedback(t, ctx, pool, coveredID, createdAt, false, ptrext.Of(strictDue), "covered@example.com")

	missingID := coveredID + 1_000_000
	result, err := service.ApplyRecommendations(ctx, feedbackassignmentsvc.ApplyRecommendationInput{
		TenantID:    tenantID,
		FeedbackIDs: []int64{urgentID, coveredID, missingID},
		ActorID:     "operator-3",
		Now:         createdAt.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("apply assignment recommendations: %v", err)
	}
	assertRecommendationApplyResult(t, result, missingID)

	detail, err := repo.GetForConsole(ctx, tenantID, urgentID)
	if err != nil {
		t.Fatalf("get urgent feedback detail: %v", err)
	}
	assertRecommendedAssignmentProjection(t, detail, createdAt.Add(24*time.Hour))

	entries, _, err := auditRepo.List(ctx, tenantID, urgentID, "", 10)
	if err != nil {
		t.Fatalf("list recommended assignment audit: %v", err)
	}
	assertRecommendedAssignmentAudit(t, entries)
}

func TestPG_AssignmentPolicyPersistsDefaultOwnerAndAudit(t *testing.T) {
	fixture := newPGAssignmentPolicyFixture(t)
	updatePGAssignmentPolicy(t, fixture, "policy-admin", "route urgent feedback to enterprise triage", 8)
	policy, err := fixture.service.GetPolicy(fixture.ctx, fixture.tenantID)
	if err != nil {
		t.Fatalf("get assignment policy: %v", err)
	}
	assertAssignmentPolicyRule(t, policy, fixture.ownerID)
	assertAssignmentPolicyRevisions(t, fixture.service, fixture.ctx, fixture.tenantID, []int{1})
	assertPGAssignmentPolicyDryRun(t, fixture)
	assertPGConfiguredRecommendationAndApply(t, fixture)
	updatePGAssignmentPolicy(t, fixture, "policy-admin-2", "tighten urgent SLA", 4)
	assertAssignmentPolicyRevisions(t, fixture.service, fixture.ctx, fixture.tenantID, []int{2, 1})
	assertPGAssignmentPolicyRestore(t, fixture)
	assertPGAssignmentPolicyAudit(t, fixture)
}

type pgAssignmentPolicyFixture struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	tenantID  string
	urgentID  int64
	repo      *feedback.FeedbackRepo
	auditRepo *auditlogrepo.Repo
	service   *feedbackassignmentsvc.Service
	ownerID   string
	createdAt time.Time
}

func newPGAssignmentPolicyFixture(t *testing.T) pgAssignmentPolicyFixture {
	t.Helper()
	pool := testdb.NewPool(t)
	tenantID, urgentID := seedTenantAndRow(t, pool, "urgent configured assignment policy feedback")
	repo := feedback.NewFeedback(pool)
	feedbackAuditRepo := feedbackauditrepo.New(pool)
	auditRepo := auditlogrepo.New(pool)
	service := feedbackassignmentsvc.New(repo, feedbackAuditRepo, pool)
	service.SetPolicyStore(systemsettingsrepo.NewRepo(pool))
	service.SetAuditLogger(auditlogsvc.New(auditRepo))
	ctx := context.Background()
	ownerID := seedAssignmentMember(t, ctx, pool, tenantID, "policy-pm-user", "policy-pm@example.com")
	createdAt := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	prepareRecommendedFeedback(t, ctx, pool, urgentID, createdAt, true, nil, "policy-urgent@example.com")
	return pgAssignmentPolicyFixture{
		ctx:       ctx,
		pool:      pool,
		tenantID:  tenantID,
		urgentID:  urgentID,
		repo:      repo,
		auditRepo: auditRepo,
		service:   service,
		ownerID:   ownerID,
		createdAt: createdAt,
	}
}

func updatePGAssignmentPolicy(
	t *testing.T,
	fixture pgAssignmentPolicyFixture,
	actorID string,
	note string,
	slaHours int,
) {
	t.Helper()
	_, err := fixture.service.UpdatePolicy(fixture.ctx, feedbackassignmentsvc.UpdatePolicyInput{
		TenantID: fixture.tenantID,
		Actor:    auditlogsvc.Actor{Type: "user", ID: actorID},
		Note:     note,
		Rules: []feedbackassignmentsvc.PolicyRule{{
			RuleKey:              "urgent_open",
			OwnerLane:            "enterprise_triage",
			SLAHours:             slaHours,
			DefaultOwnerMemberID: ptrext.Of(fixture.ownerID),
			Enabled:              true,
		}},
	})
	if err != nil {
		t.Fatalf("update assignment policy: %v", err)
	}
}

func assertPGAssignmentPolicyDryRun(t *testing.T, fixture pgAssignmentPolicyFixture) {
	t.Helper()
	dryRun, err := fixture.service.DryRunPolicy(fixture.ctx, feedbackassignmentsvc.DryRunPolicyInput{
		TenantID:    fixture.tenantID,
		FeedbackIDs: []int64{fixture.urgentID},
		Now:         fixture.createdAt.Add(time.Hour),
		Rules: []feedbackassignmentsvc.PolicyRule{{
			RuleKey:              "urgent_open",
			OwnerLane:            "enterprise_triage",
			SLAHours:             4,
			DefaultOwnerMemberID: ptrext.Of(fixture.ownerID),
			Enabled:              true,
		}},
	})
	if err != nil {
		t.Fatalf("dry-run assignment policy: %v", err)
	}
	assertAssignmentPolicyDryRun(t, dryRun)
}

func assertPGConfiguredRecommendationAndApply(t *testing.T, fixture pgAssignmentPolicyFixture) {
	t.Helper()
	recs, err := fixture.service.RecommendBatch(fixture.ctx, feedbackassignmentsvc.RecommendationInput{
		TenantID:    fixture.tenantID,
		FeedbackIDs: []int64{fixture.urgentID},
		Now:         fixture.createdAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("recommend with assignment policy: %v", err)
	}
	assertConfiguredRecommendation(t, recs, fixture.ownerID)

	applied, err := fixture.service.ApplyRecommendations(fixture.ctx, feedbackassignmentsvc.ApplyRecommendationInput{
		TenantID:    fixture.tenantID,
		FeedbackIDs: []int64{fixture.urgentID},
		ActorID:     "policy-operator",
		Now:         fixture.createdAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("apply configured recommendation: %v", err)
	}
	if applied.Succeeded != 1 {
		t.Fatalf("configured apply result = %#v, want one success", applied)
	}
	detail, err := fixture.repo.GetForConsole(fixture.ctx, fixture.tenantID, fixture.urgentID)
	if err != nil {
		t.Fatalf("get configured urgent detail: %v", err)
	}
	assertConfiguredAssignmentProjection(t, detail, fixture.ownerID, fixture.createdAt.Add(8*time.Hour))
}

func assertPGAssignmentPolicyRestore(t *testing.T, fixture pgAssignmentPolicyFixture) {
	t.Helper()
	restored, err := fixture.service.RestorePolicy(fixture.ctx, feedbackassignmentsvc.RestorePolicyInput{
		TenantID: fixture.tenantID,
		Version:  1,
		Actor:    auditlogsvc.Actor{Type: "user", ID: "policy-admin-3"},
	})
	if err != nil {
		t.Fatalf("restore assignment policy: %v", err)
	}
	if restored.Version != 3 {
		t.Fatalf("restored policy version = %d, want 3", restored.Version)
	}
	assertAssignmentPolicyRule(t, restored, fixture.ownerID)
	assertAssignmentPolicyRevisions(t, fixture.service, fixture.ctx, fixture.tenantID, []int{3, 2, 1})
}

func assertPGAssignmentPolicyAudit(t *testing.T, fixture pgAssignmentPolicyFixture) {
	t.Helper()
	auditRows, err := fixture.auditRepo.List(fixture.ctx, auditlogrepo.ListFilter{
		TenantID:   fixture.tenantID,
		Actions:    []string{"feedback_assignment.policy_update", "feedback_assignment.policy_restore"},
		TargetType: "feedback_assignment_policy",
		TargetID:   fixture.tenantID,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("list assignment policy audit: %v", err)
	}
	assertAssignmentPolicyAuditRows(t, auditRows.Items)
}

func assertConfiguredRecommendation(
	t *testing.T,
	recs feedbackassignmentsvc.RecommendationResult,
	ownerID string,
) {
	t.Helper()
	if len(recs.Recommendations) != 1 {
		t.Fatalf("recommendations = %#v, want one configured urgent recommendation", recs)
	}
	rec := recs.Recommendations[0]
	if rec.OwnerLane != "enterprise_triage" || rec.SLAHours != 8 || ptrext.Indirect(rec.RecommendedOwnerMemberID) != ownerID {
		t.Fatalf("configured recommendation = %#v", rec)
	}
}

func assertConfiguredAssignmentProjection(
	t *testing.T,
	detail *feedback.ConsoleDetailRow,
	ownerID string,
	wantDue time.Time,
) {
	t.Helper()
	if got := ptrext.IndirectOr(detail.Assignment.OwnerMemberID, ""); got != ownerID {
		t.Fatalf("configured assignment owner = %q, want %q", got, ownerID)
	}
	if detail.Assignment.SLADueAt == nil || !detail.Assignment.SLADueAt.Equal(wantDue) {
		t.Fatalf("configured assignment SLA = %v, want %v", detail.Assignment.SLADueAt, wantDue)
	}
}

func assertAssignmentPolicyAuditRows(t *testing.T, entries []auditlogrepo.Entry) {
	t.Helper()
	actions := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		actions[entry.Action] = struct{}{}
	}
	if _, ok := actions["feedback_assignment.policy_update"]; !ok {
		t.Fatalf("assignment policy audit rows = %#v, missing update audit", entries)
	}
	if _, ok := actions["feedback_assignment.policy_restore"]; !ok {
		t.Fatalf("assignment policy audit rows = %#v, missing restore audit", entries)
	}
}

func assertAssignmentPolicyRevisions(
	t *testing.T,
	service *feedbackassignmentsvc.Service,
	ctx context.Context,
	tenantID string,
	wantVersions []int,
) {
	t.Helper()
	revisions, err := service.ListPolicyRevisions(ctx, tenantID)
	if err != nil {
		t.Fatalf("list assignment policy revisions: %v", err)
	}
	if len(revisions) < len(wantVersions) {
		t.Fatalf("assignment policy revisions = %#v, want at least %v", revisions, wantVersions)
	}
	for i, want := range wantVersions {
		if revisions[i].Version != want {
			t.Fatalf("assignment policy revisions = %#v, want version %d at index %d", revisions, want, i)
		}
	}
}

func assertAssignmentPolicyDryRun(t *testing.T, dryRun feedbackassignmentsvc.DryRunPolicyResult) {
	t.Helper()
	if dryRun.TotalMatched != 1 || dryRun.Changed != 1 || len(dryRun.Impacts) != 1 {
		t.Fatalf("assignment policy dry-run = %#v, want one changed impact", dryRun)
	}
	impact := dryRun.Impacts[0]
	if impact.CurrentSLAHours != 8 || impact.DraftSLAHours != 4 || !impact.Changed {
		t.Fatalf("assignment policy dry-run impact = %#v, want 8h -> 4h", impact)
	}
}

func assertAssignmentPolicyRule(t *testing.T, policy feedbackassignmentsvc.Policy, ownerID string) {
	t.Helper()
	for _, rule := range policy.Rules {
		if rule.RuleKey != "urgent_open" {
			continue
		}
		if rule.OwnerLane != "enterprise_triage" || rule.SLAHours != 8 || ptrext.Indirect(rule.DefaultOwnerMemberID) != ownerID {
			t.Fatalf("urgent policy rule = %#v, want configured lane/SLA/owner", rule)
		}
		return
	}
	t.Fatalf("assignment policy rules = %#v, missing urgent_open", policy.Rules)
}

type assignmentEscalationSeed struct {
	Title         string
	Source        string
	FeedbackType  string
	CreatedAt     time.Time
	IsUrgent      bool
	OwnerMemberID *string
	SLADueAt      *time.Time
	StateID       *string
}

func prepareAssignmentEscalationFeedback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID int64,
	seed assignmentEscalationSeed,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		   SET enriched_title = $2,
		       source = $3,
		       type = $4,
		       created_at = $5,
		       is_urgent = $6,
		       owner_member_id = $7::uuid,
		       feedback_sla_due_at = $8,
		       workflow_state_id = $9::uuid
		 WHERE id = $1`,
		feedbackID,
		seed.Title,
		seed.Source,
		seed.FeedbackType,
		seed.CreatedAt,
		seed.IsUrgent,
		seed.OwnerMemberID,
		seed.SLADueAt,
		seed.StateID,
	)
	if err != nil {
		t.Fatalf("prepare assignment escalation row: %v", err)
	}
}

func seedClosedWorkflowState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
) string {
	t.Helper()
	var stateID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenant_workflow_states (tenant_id, name, color, category, position, is_default)
		VALUES ($1, 'Escalation closed', '#64748b', 'closed', 90, false)
		RETURNING id::text`,
		tenantID,
	).Scan(&stateID)
	if err != nil {
		t.Fatalf("insert closed workflow state: %v", err)
	}
	return stateID
}

func assertAssignmentEscalationQueue(
	t *testing.T,
	queue feedback.AssignmentEscalationQueue,
	wantIDs []int64,
) {
	t.Helper()
	if queue.OverdueCount != 1 || queue.DueSoonCount != 1 ||
		queue.MissingOwnerCount != 1 || queue.MissingSLACount != 1 {
		t.Fatalf("assignment escalation counts = %#v, want one per durable breach type", queue)
	}
	if len(queue.Items) != len(wantIDs) {
		t.Fatalf("assignment escalation items = %#v, want ids %v", queue.Items, wantIDs)
	}
	for i, wantID := range wantIDs {
		if queue.Items[i].FeedbackID != wantID {
			t.Fatalf("assignment escalation item[%d] = %#v, want feedback id %d", i, queue.Items[i], wantID)
		}
	}
	if len(queue.Items) == 0 {
		return
	}
	first := queue.Items[0]
	if first.Priority != "critical" || !containsEscalationReason(first.Reasons, "overdue") ||
		ptrext.Indirect(first.HoursUntilDue) != -2 {
		t.Fatalf("first assignment escalation = %#v, want overdue critical item", first)
	}
}

func containsEscalationReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func prepareRecommendedFeedback(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID int64,
	createdAt time.Time,
	urgent bool,
	dueAt *time.Time,
	email string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		   SET created_at = $2,
		       is_urgent = $3,
		       feedback_sla_due_at = $4,
		       source_meta = jsonb_build_object('email', $5::text)
		 WHERE id = $1`,
		feedbackID,
		createdAt,
		urgent,
		dueAt,
		email,
	)
	if err != nil {
		t.Fatalf("prepare recommended feedback row: %v", err)
	}
}

func seedAssignmentMember(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	userID string,
	email string,
) string {
	t.Helper()
	var ownerID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, email, role, role_source, accepted_at
		)
		VALUES ($1, 'tenant_user', $2, $3, 'member', 'manual', NOW())
		RETURNING id::text`,
		tenantID,
		userID,
		email,
	).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert tenant member: %v", err)
	}
	return ownerID
}

func assertRecommendationApplyResult(
	t *testing.T,
	result feedbackassignmentsvc.ApplyRecommendationResult,
	missingID int64,
) {
	t.Helper()
	if result.TotalMatched != 3 || result.Succeeded != 1 || result.Skipped != 1 || len(result.Failed) != 1 {
		t.Fatalf("recommendation result = %#v, want 3 matched, 1 succeeded, 1 skipped, 1 failed", result)
	}
	if result.Failed[0].FeedbackID != missingID || result.Failed[0].Code != "NOT_FOUND" {
		t.Fatalf("recommendation failure = %#v, want missing feedback id", result.Failed[0])
	}
}

func assertRecommendedAssignmentProjection(
	t *testing.T,
	detail *feedback.ConsoleDetailRow,
	dueAt time.Time,
) {
	t.Helper()
	if detail.Assignment.SLADueAt == nil || !detail.Assignment.SLADueAt.Equal(dueAt) {
		t.Fatalf("recommended due_at = %v, want %v", detail.Assignment.SLADueAt, dueAt)
	}
	if !strings.Contains(detail.Assignment.Note, "Assignment policy: Urgent open feedback") {
		t.Fatalf("recommended note = %q", detail.Assignment.Note)
	}
}

func assertRecommendedAssignmentAudit(t *testing.T, entries []feedbackauditrepo.Entry) {
	t.Helper()
	gotFields := map[string]bool{}
	for _, entry := range entries {
		gotFields[entry.FieldName] = true
		if entry.EntityType != "feedback_assignment" || entry.ChangedBy != "operator-3" {
			t.Fatalf("recommended audit entry = %#v, want assignment audit by operator-3", entry)
		}
	}
	for _, field := range []string{"feedback_sla_due_at", "owner_assignment_note"} {
		if !gotFields[field] {
			t.Fatalf("recommended audit fields = %v, missing %s", gotFields, field)
		}
	}
}

func assertBatchAssignmentResult(
	t *testing.T,
	result feedbackassignmentsvc.BatchResult,
	missingID int64,
) {
	t.Helper()
	if result.TotalMatched != 3 || result.Succeeded != 2 || len(result.Failed) != 1 {
		t.Fatalf("batch result = %#v, want 3 matched, 2 succeeded, 1 failed", result)
	}
	if result.Failed[0].FeedbackID != missingID || result.Failed[0].Code != "NOT_FOUND" {
		t.Fatalf("batch failure = %#v, want missing feedback id", result.Failed[0])
	}
}

func assertBatchAssignmentProjection(
	t *testing.T,
	detail *feedback.ConsoleDetailRow,
	ownerID string,
	dueAt time.Time,
) {
	t.Helper()
	if got := ptrext.IndirectOr(detail.Assignment.OwnerMemberID, ""); got != ownerID {
		t.Fatalf("projected owner = %q, want %q", got, ownerID)
	}
	if detail.Assignment.OwnerEmail != "batch-pm@example.com" {
		t.Fatalf("projected owner email = %q", detail.Assignment.OwnerEmail)
	}
	if detail.Assignment.AssignedBy != "operator-2" {
		t.Fatalf("projected assigned_by = %q", detail.Assignment.AssignedBy)
	}
	if detail.Assignment.SLADueAt == nil || !detail.Assignment.SLADueAt.Equal(dueAt) {
		t.Fatalf("projected due_at = %v, want %v", detail.Assignment.SLADueAt, dueAt)
	}
}

func assertBatchAssignmentAudit(t *testing.T, entries []feedbackauditrepo.Entry) {
	t.Helper()
	gotFields := map[string]bool{}
	for _, entry := range entries {
		gotFields[entry.FieldName] = true
		if entry.EntityType != "feedback_assignment" || entry.ChangedBy != "operator-2" {
			t.Fatalf("audit entry = %#v, want assignment audit by operator-2", entry)
		}
	}
	for _, field := range []string{"owner_member_id", "feedback_sla_due_at", "owner_assignment_note"} {
		if !gotFields[field] {
			t.Fatalf("audit fields = %v, missing %s", gotFields, field)
		}
	}
}

func TestPG_MarkFailedSchedulesRetryAndStopsAfterMaxAttempts(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "retry me")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	ok, err := repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("claim before failure: ok=%v err=%v", ok, err)
	}
	repo.MarkFailed(ctx, id, "provider unavailable") // terminal-flag boundary covered by TestPG_MarkFailedReturnsTerminalAndTenant

	var (
		status    string
		attempts  int
		nextRetry time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT enrichment_status, enrichment_attempts, enrichment_next_retry_at
		  FROM user_feedback
		 WHERE id = $1`, id).Scan(&status, &attempts, &nextRetry); err != nil {
		t.Fatalf("read failed retry state: %v", err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("retry state = %s/%d; want failed/1", status, attempts)
	}
	if !nextRetry.After(time.Now()) {
		t.Fatalf("next retry = %s; want future timestamp", nextRetry)
	}

	ok, err = repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatalf("claim before backoff elapsed: %v", err)
	}
	if ok {
		t.Fatal("row should not be claimable before next retry")
	}
	assertPendingListExcludes(t, repo, id)

	if _, err := pool.Exec(ctx,
		`UPDATE user_feedback SET enrichment_next_retry_at = NOW() - INTERVAL '1 second' WHERE id = $1`, id); err != nil {
		t.Fatalf("force retry due: %v", err)
	}
	ok, err = repo.TryClaim(ctx, id)
	if err != nil || !ok {
		t.Fatalf("claim after backoff elapsed: ok=%v err=%v", ok, err)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE user_feedback
		   SET enrichment_status = 'failed',
		       enrichment_attempts = 5,
		       enrichment_next_retry_at = NULL,
		       enrichment_claimed_at = NULL
		 WHERE id = $1`, id); err != nil {
		t.Fatalf("force max attempts: %v", err)
	}
	ok, err = repo.TryClaim(ctx, id)
	if err != nil {
		t.Fatalf("claim after max attempts: %v", err)
	}
	if ok {
		t.Fatal("row should not be claimable after max attempts")
	}
	assertPendingListExcludes(t, repo, id)
}

// TestPG_MarkFailedReturnsTerminalAndTenant pins the #64 signal: MarkFailed's
// RETURNING (enrichment_attempts >= max), tenant_id must report terminal=false
// until the final attempt, then terminal=true with the row's tenant — this is
// what feeds attune_enrichment_terminal_failures_total, and an off-by-one in the
// SQL comparison would silently break the metric.
func TestPG_MarkFailedReturnsTerminalAndTenant(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "exhaust me")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	const maxAttempts = 5 // feedback.maxEnrichmentAttempts
	for i := 1; i <= maxAttempts; i++ {
		terminal, tenant := repo.MarkFailed(ctx, id, "boom")
		if tenant != tenantID {
			t.Fatalf("attempt %d: tenant = %q, want %q", i, tenant, tenantID)
		}
		wantTerminal := i >= maxAttempts
		if terminal != wantTerminal {
			t.Fatalf("attempt %d: terminal = %v, want %v", i, terminal, wantTerminal)
		}
	}

	// A no-rows update (row gone) returns (false, "") without a terminal count.
	if terminal, tenant := repo.MarkFailed(ctx, 999999999, "missing"); terminal || tenant != "" {
		t.Fatalf("missing row: terminal=%v tenant=%q, want false/empty", terminal, tenant)
	}
}

func TestPG_MarkDoneAndContainmentQuery(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "payment broke")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	enriched := domain.Enriched{
		Title:        "Payment failed",
		DisplayTitle: "支付失败",
		Attrs: map[string]any{
			"type":     "bug",
			"severity": "critical",
			"labels":   []string{"payment", "ux"},
		},
		IsUrgent:                 true,
		Rationale:                "core flow",
		DisplayRationale:         "核心流程受阻",
		ClassificationConfidence: ptrext.Of(0.42),
	}
	if err := repo.MarkDone(ctx, id, enriched, feedback.EnrichmentMetadata{
		Language:      "en",
		DisplayLocale: "zh",
	}); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "severity", Value: "critical", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListForConsole: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row matching severity=critical, got %d", len(rows))
	}
	assertConsoleDisplayRow(t, rows[0])
	detail, err := repo.GetForConsole(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("GetForConsole: %v", err)
	}
	if detail.EnrichedDisplayRationale != "核心流程受阻" {
		t.Errorf("display rationale: %q", detail.EnrichedDisplayRationale)
	}
	if got := ptrext.Indirect(detail.ClassificationConfidence); got != 0.42 {
		t.Errorf("detail confidence: %v", got)
	}

	// Negative containment: severity=minor should match zero rows.
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "severity", Value: "minor", Multi: false}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("severity=minor should match 0 rows, got %d", len(rows))
	}

	// Multi-kind containment: labels=payment must hit.
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		Attrs: []feedback.AttrFilter{{Dim: "labels", Value: "payment", Multi: true}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("labels=payment should match 1 row, got %d", len(rows))
	}
}

func TestPG_FeedbackAccountContextFiltersRawSignals(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, acmeID := seedTenantAndRow(t, pool, "acme renewal blocker")
	_, betaID := seedTenantAndRow(t, pool, "beta onboarding request")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	seedFeedbackAccountContext(
		t, ctx, pool, acmeID, "portal", "bug", "Acme renewal blocker",
		now.Add(-2*time.Hour),
		`{"account":{"key":"acct:acme","name":"Acme Corp"},"email":"ada@example.com"}`,
	)
	seedFeedbackAccountContext(
		t, ctx, pool, betaID, "api", "feature", "Beta onboarding request",
		now.Add(-time.Hour),
		`{"companyId":"acct:beta","companyName":"Beta LLC"}`,
	)

	acmeRow := assertFeedbackAccountListRow(t, ctx, repo, tenantID, "acct:acme", acmeID)
	require.Equal(t, "Acme Corp", acmeRow.AccountContext.AccountDisplay)
	require.Equal(t, "source_meta", acmeRow.AccountContext.Source)

	betaRow := assertFeedbackAccountListRow(t, ctx, repo, tenantID, "acct:beta", betaID)
	require.Equal(t, "Beta LLC", betaRow.AccountContext.AccountDisplay)

	detail, err := repo.GetForConsole(ctx, tenantID, acmeID)
	require.NoError(t, err)
	require.Equal(t, "acct:acme", detail.AccountContext.AccountKey)

	queue, err := repo.FeedbackAssignmentEscalations(ctx, tenantID, now, 25)
	require.NoError(t, err)
	found := assignmentEscalationByFeedbackID(queue.Items, acmeID)
	require.NotNil(t, found)
	require.Equal(t, "acct:acme", found.Account.AccountKey)
	require.Equal(t, "Acme Corp", found.Account.AccountDisplay)
}

func seedFeedbackAccountContext(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	feedbackID int64,
	source string,
	feedbackType string,
	title string,
	createdAt time.Time,
	sourceMeta string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		SET source = $2,
		    type = $3,
		    enriched_title = $4,
		    created_at = $5,
		    source_meta = $6::jsonb
		WHERE id = $1`,
		feedbackID, source, feedbackType, title, createdAt, sourceMeta,
	)
	require.NoError(t, err)
}

func assertFeedbackAccountListRow(
	t *testing.T,
	ctx context.Context,
	repo *feedback.FeedbackRepo,
	tenantID string,
	accountKey string,
	wantID int64,
) feedback.ConsoleListRow {
	t.Helper()
	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		AccountKey: ptrext.Of(accountKey),
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, wantID, rows[0].ID)
	require.Equal(t, accountKey, rows[0].AccountContext.AccountKey)
	return rows[0]
}

func assignmentEscalationByFeedbackID(
	items []feedback.AssignmentEscalation,
	feedbackID int64,
) *feedback.AssignmentEscalation {
	for i := range items {
		if items[i].FeedbackID == feedbackID {
			return &items[i]
		}
	}
	return nil
}

func assertPendingListExcludes(t *testing.T, repo *feedback.FeedbackRepo, blockedID int64) {
	t.Helper()
	rows, err := repo.ListPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	for _, id := range rows {
		if id == blockedID {
			t.Fatalf("ListPending returned blocked id %d: %v", blockedID, rows)
		}
	}
}

func assertConsoleDisplayRow(t *testing.T, row feedback.ConsoleListRow) {
	t.Helper()
	if !row.IsUrgent {
		t.Error("is_urgent should be true")
	}
	if row.EnrichedTitle != "Payment failed" {
		t.Errorf("title: %q", row.EnrichedTitle)
	}
	if row.EnrichedDisplayTitle != "支付失败" {
		t.Errorf("display title: %q", row.EnrichedDisplayTitle)
	}
	if row.Language != "en" {
		t.Errorf("language: %q", row.Language)
	}
	if row.EnrichedDisplayLocale != "zh" {
		t.Errorf("display locale: %q", row.EnrichedDisplayLocale)
	}
	if got := ptrext.Indirect(row.ClassificationConfidence); got != 0.42 {
		t.Errorf("confidence: %v", got)
	}
}

func TestPG_AttrsSizeCapRefused(t *testing.T) {
	pool := testdb.NewPool(t)
	_, id := seedTenantAndRow(t, pool, "big payload")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("a", feedback.MaxAttrsBytes+1)
	enriched := domain.Enriched{
		Title: "x",
		Attrs: map[string]any{"labels": []string{huge}},
	}
	if err := repo.MarkDone(ctx, id, enriched); err == nil {
		t.Fatal("oversized attrs must be refused")
	}
	// Row stays in enriching state (was claimed before MarkDone).
	var status string
	if err := pool.QueryRow(ctx,
		"SELECT enrichment_status FROM user_feedback WHERE id=$1", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "enriching" {
		t.Errorf("status after rejection: %s (should remain enriching)", status)
	}
}

func TestPG_TopValuesByDim_SingleAndMulti(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id1 := seedTenantAndRow(t, pool, "row 1")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	// Row 1: severity=critical, labels=[payment, ux]
	_, _ = repo.TryClaim(ctx, id1)
	_ = repo.MarkDone(ctx, id1, domain.Enriched{
		Title: "t1",
		Attrs: map[string]any{"severity": "critical", "labels": []string{"payment", "ux"}},
	})
	// Row 2: severity=minor, labels=[ux, dark-mode]
	var id2 int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', 'row 2')
		RETURNING id`, tenantID).Scan(&id2)
	_, _ = repo.TryClaim(ctx, id2)
	_ = repo.MarkDone(ctx, id2, domain.Enriched{
		Title: "t2",
		Attrs: map[string]any{"severity": "minor", "labels": []string{"ux", "dark-mode"}},
	})

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)

	sev, err := repo.TopValuesByDim(ctx, tenantID, "severity", false, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, vc := range sev {
		got[vc.Value] = vc.Count
	}
	if got["critical"] != 1 || got["minor"] != 1 {
		t.Errorf("severity counts: %v", got)
	}

	lbl, err := repo.TopValuesByDim(ctx, tenantID, "labels", true, from, to, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotL := map[string]int64{}
	for _, vc := range lbl {
		gotL[vc.Value] = vc.Count
	}
	if gotL["ux"] != 2 {
		t.Errorf("ux should appear in both rows, got %v", gotL)
	}
	if gotL["payment"] != 1 || gotL["dark-mode"] != 1 {
		t.Errorf("labels counts: %v", gotL)
	}
}

func TestPG_Insert_AutoAssignsDefaultWorkflowState(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('wf-auto','WF Auto Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	var defaultStateID string
	err = pool.QueryRow(ctx, `
		INSERT INTO tenant_workflow_states (tenant_id, name, color, category, position, is_default)
		VALUES ($1, 'New', '#3b82f6', 'open', 0, true)
		RETURNING id::text`, tenantID).Scan(&defaultStateID)
	if err != nil {
		t.Fatalf("insert default workflow state: %v", err)
	}

	repo := feedback.NewFeedback(pool)
	id, err := repo.Insert(ctx, tenantID, "u1", "u1", "u1", "hash-u1", domain.IngestInput{
		Content: "auto-assign test",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var wsID *string // ptrext:allow scan-target
	if err := pool.QueryRow(ctx,
		"SELECT workflow_state_id::text FROM user_feedback WHERE id = $1", id).Scan(&wsID); err != nil { // ptrext:allow scan-target
		t.Fatalf("read workflow_state_id: %v", err)
	}
	if wsID == nil {
		t.Fatal("workflow_state_id should be set to the default state, got NULL")
	}
	if *wsID != defaultStateID {
		t.Errorf("workflow_state_id = %q, want %q", *wsID, defaultStateID)
	}
}

func TestPG_Insert_NullWorkflowStateWhenNoDefault(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	var tenantID string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('wf-none','WF None Co')
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	repo := feedback.NewFeedback(pool)
	id, err := repo.Insert(ctx, tenantID, "u1", "u1", "u1", "hash-u1", domain.IngestInput{
		Content: "no default test",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var wsID *string // ptrext:allow scan-target
	if err := pool.QueryRow(ctx,
		"SELECT workflow_state_id::text FROM user_feedback WHERE id = $1", id).Scan(&wsID); err != nil { // ptrext:allow scan-target
		t.Fatalf("read workflow_state_id: %v", err)
	}
	if wsID != nil {
		t.Errorf("workflow_state_id should be NULL when no default exists, got %q", *wsID)
	}
}

func TestPG_UrgentCount(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id1 := seedTenantAndRow(t, pool, "u1")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()
	_, _ = repo.TryClaim(ctx, id1)
	_ = repo.MarkDone(ctx, id1, domain.Enriched{Title: "x", IsUrgent: true})

	var id2 int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u', 'api', 'c2') RETURNING id`, tenantID).Scan(&id2)
	_, _ = repo.TryClaim(ctx, id2)
	_ = repo.MarkDone(ctx, id2, domain.Enriched{Title: "y", IsUrgent: false})

	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(time.Hour)
	n, err := repo.UrgentCount(ctx, tenantID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("urgent count: got %d, want 1", n)
	}
}

// TestPG_ListForConsole_EnrichmentStatusFilter verifies filtering by enrichment_status.
func TestPG_ListForConsole_EnrichmentStatusFilter(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, pendingID := seedTenantAndRow(t, pool, "pending row")
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	// Create a done row
	var doneID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status)
		VALUES ($1, 'u1', 'api', 'done row', 'done')
		RETURNING id`, tenantID).Scan(&doneID)

	// Create a failed row (not terminal - has future retry)
	var failedID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at)
		VALUES ($1, 'u1', 'api', 'failed row', 'failed', 2, NOW() + INTERVAL '1 minute')
		RETURNING id`, tenantID).Scan(&failedID)

	// Filter by pending
	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("pending"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(pending): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != pendingID {
		t.Errorf("pending filter: got %d rows, want 1 with id=%d", len(rows), pendingID)
	}

	// Filter by done
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("done"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(done): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != doneID {
		t.Errorf("done filter: got %d rows, want 1 with id=%d", len(rows), doneID)
	}

	// Filter by failed
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus: ptrext.Of("failed"),
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(failed): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != failedID {
		t.Errorf("failed filter: got %d rows, want 1 with id=%d", len(rows), failedID)
	}
}

// TestPG_ListForConsole_TerminalFailedOnlyFilter verifies the terminal_failed_only filter.
func TestPG_ListForConsole_TerminalFailedOnlyFilter(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, _ := seedTenantAndRow(t, pool, "ignore pending")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Create a failed row with retry scheduled (NOT terminal)
	var retriableID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at)
		VALUES ($1, 'u1', 'api', 'retriable', 'failed', 3, NOW() + INTERVAL '1 minute')
		RETURNING id`, tenantID).Scan(&retriableID)

	// Create a terminal failed row (attempts >= 5, no next retry)
	var terminalID int64
	_ = pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status, enrichment_attempts, enrichment_next_retry_at, enrichment_error)
		VALUES ($1, 'u1', 'api', 'terminal', 'failed', 5, NULL, 'provider timeout')
		RETURNING id`, tenantID).Scan(&terminalID)

	// terminal_failed_only should return only the terminal row
	rows, err := repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		TerminalFailedOnly: ptrext.Of(true),
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(terminal_failed_only): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("terminal_failed_only: got %d rows, want 1", len(rows))
	}
	if rows[0].ID != terminalID {
		t.Errorf("terminal_failed_only: got id=%d, want %d", rows[0].ID, terminalID)
	}

	// Verify the terminal row has correct metadata
	if rows[0].EnrichmentAttempts != 5 {
		t.Errorf("attempts: got %d, want 5", rows[0].EnrichmentAttempts)
	}
	if rows[0].EnrichmentNextRetryAt != nil {
		t.Errorf("next_retry_at should be nil for terminal row, got %v", rows[0].EnrichmentNextRetryAt)
	}

	// Combining enrichment_status=failed with terminal_failed_only=true should work
	rows, err = repo.ListForConsole(ctx, tenantID, feedback.ConsoleListOpts{
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListForConsole(failed+terminal): %v", err)
	}
	if len(rows) != 1 || rows[0].ID != terminalID {
		t.Errorf("failed+terminal filter: got %d rows", len(rows))
	}
}

// TestPG_ConsoleDetailRow_EnrichmentMetadata verifies enrichment_attempts and
// enrichment_next_retry_at are exposed in the detail view.
func TestPG_ConsoleDetailRow_EnrichmentMetadata(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "detail test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set up a failed row with retry metadata
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'failed',
		    enrichment_attempts = 3,
		    enrichment_next_retry_at = NOW() + INTERVAL '5 minutes',
		    enrichment_error = 'API rate limited'
		WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("update row: %v", err)
	}

	detail, err := repo.GetForConsole(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("GetForConsole: %v", err)
	}

	if detail.EnrichmentStatus != "failed" {
		t.Errorf("status: got %q, want 'failed'", detail.EnrichmentStatus)
	}
	if detail.EnrichmentAttempts != 3 {
		t.Errorf("attempts: got %d, want 3", detail.EnrichmentAttempts)
	}
	if detail.EnrichmentNextRetryAt == nil {
		t.Fatal("next_retry_at should not be nil")
	}
	if !detail.EnrichmentNextRetryAt.After(time.Now()) {
		t.Errorf("next_retry_at should be in the future, got %v", detail.EnrichmentNextRetryAt)
	}
	if detail.EnrichmentError != "API rate limited" {
		t.Errorf("error: got %q, want 'API rate limited'", detail.EnrichmentError)
	}
}

// TestPG_RetryEnrichment verifies manual retry resets attempts and schedules immediate retry.
func TestPG_RetryEnrichment(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "retry test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set up a terminal failed row
	_, err := pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'failed',
		    enrichment_attempts = 5,
		    enrichment_next_retry_at = NULL,
		    enrichment_error = 'exhausted retries'
		WHERE id = $1`, id)
	if err != nil {
		t.Fatalf("setup terminal row: %v", err)
	}

	// Verify row is NOT in ListPending (terminal)
	assertPendingListExcludes(t, repo, id)

	// Retry the row
	result, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("RetryEnrichment: %v", err)
	}

	if result.ID != id {
		t.Errorf("result.ID: got %d, want %d", result.ID, id)
	}
	if result.Status != "failed" {
		t.Errorf("result.Status: got %q, want 'failed'", result.Status)
	}
	if result.Attempts != 0 {
		t.Errorf("result.Attempts: got %d, want 0", result.Attempts)
	}
	if result.NextRetryAt == nil || !result.NextRetryAt.Before(time.Now().Add(time.Second)) {
		t.Errorf("result.NextRetryAt should be ~NOW, got %v", result.NextRetryAt)
	}

	// Verify row IS now in ListPending
	pending, err := repo.ListPending(ctx, 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	found := false
	for _, pid := range pending {
		if pid == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("row %d should be in ListPending after retry, got %v", id, pending)
	}

	// Verify error was cleared
	var enrichErr string
	_ = pool.QueryRow(ctx, "SELECT COALESCE(enrichment_error, '') FROM user_feedback WHERE id = $1", id).Scan(&enrichErr)
	if enrichErr != "" {
		t.Errorf("enrichment_error should be cleared, got %q", enrichErr)
	}
}

// TestPG_RetryEnrichment_InvalidState verifies retry fails for non-failed rows.
func TestPG_RetryEnrichment_InvalidState(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "invalid state test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Row is in 'pending' state by default
	_, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for pending row")
	}
	if !strings.Contains(err.Error(), "invalid state") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}

	// Set to done state
	_, _ = pool.Exec(ctx, `UPDATE user_feedback SET enrichment_status = 'done' WHERE id = $1`, id)
	_, err = repo.RetryEnrichment(ctx, tenantID, id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for done row")
	}
}

// TestPG_RetryEnrichment_WrongTenant verifies retry fails for wrong tenant.
func TestPG_RetryEnrichment_WrongTenant(t *testing.T) {
	pool := testdb.NewPool(t)
	tenantID, id := seedTenantAndRow(t, pool, "wrong tenant test")
	ctx := context.Background()
	repo := feedback.NewFeedback(pool)

	// Set to failed state
	_, _ = pool.Exec(ctx, `UPDATE user_feedback SET enrichment_status = 'failed', enrichment_attempts = 5, enrichment_next_retry_at = NULL WHERE id = $1`, id)

	// Try with wrong tenant
	_, err := repo.RetryEnrichment(ctx, "wrong-tenant-id", id)
	if err == nil {
		t.Fatal("RetryEnrichment should fail for wrong tenant")
	}

	// Verify row was not modified
	var attempts int
	_ = pool.QueryRow(ctx, "SELECT enrichment_attempts FROM user_feedback WHERE id = $1", id).Scan(&attempts)
	if attempts != 5 {
		t.Errorf("row should not be modified, attempts=%d", attempts)
	}

	// Correct tenant should work
	result, err := repo.RetryEnrichment(ctx, tenantID, id)
	if err != nil {
		t.Fatalf("RetryEnrichment with correct tenant: %v", err)
	}
	if result.Attempts != 0 {
		t.Errorf("attempts after retry: %d", result.Attempts)
	}
}
