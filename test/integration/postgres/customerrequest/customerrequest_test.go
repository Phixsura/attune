//go:build integration

// SPDX-License-Identifier: Apache-2.0

package customerrequest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	idempotencyrepo "github.com/Phixsura/attune/internal/repo/idempotency"
	"github.com/Phixsura/attune/internal/repo/tenant"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	crsvc "github.com/Phixsura/attune/internal/service/customerrequest"
	"github.com/Phixsura/attune/internal/testdb"
)

type env struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	repo     *crrepo.Repo
	tenantID string
}

func setup(t *testing.T) env {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "customer-request-io", "Customer Request IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return env{ctx: ctx, pool: pool, repo: crrepo.New(pool), tenantID: tenantID}
}

func TestPGCustomerRequestTenantIsolation(t *testing.T) {
	e := setup(t)
	otherTenant := e.createTenant(t, "customer-request-other")
	request := e.createRequest(t, e.tenantID, "Tenant scoped request")
	foreignFeedbackID := e.seedFeedback(t, otherTenant, "foreign-user", "foreign-subject")

	tx := e.begin(t)
	err := e.repo.LinkFeedbackTx(e.ctx, tx, crrepo.LinkFeedbackInput{
		TenantID:   e.tenantID,
		RequestID:  request.ID,
		FeedbackID: foreignFeedbackID,
		Importance: crrepo.ImportanceNormal,
		ActorID:    "tester",
	})
	rollback(t, e.ctx, tx)
	if !errors.Is(err, crrepo.ErrFeedbackNotFound) {
		t.Fatalf("cross-tenant LinkFeedbackTx error = %v, want ErrFeedbackNotFound", err)
	}

	tx = e.begin(t)
	customer, err := e.repo.LinkCustomerTx(e.ctx, tx, crrepo.CustomerLinkInput{
		TenantID:       e.tenantID,
		RequestID:      request.ID,
		SubjectKey:     "user:42",
		SubjectDisplay: "Ada Lovelace",
		AccountKey:     "account:acme",
		AccountDisplay: "Acme Inc",
		ActorID:        "tester",
	})
	if err != nil {
		t.Fatalf("LinkCustomerTx: %v", err)
	}
	vote, err := e.repo.AddVoteTx(e.ctx, tx, crrepo.VoteInput{
		TenantID:   e.tenantID,
		RequestID:  request.ID,
		SubjectKey: "user:42",
		AccountKey: "account:acme",
		Weight:     3,
		ActorID:    "tester",
	})
	if err != nil {
		t.Fatalf("AddVoteTx: %v", err)
	}
	commit(t, e.ctx, tx)

	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail tenant: %v", err)
	}
	if detail.Summary.CustomerCount != 1 || detail.Summary.AccountCount != 1 || detail.Summary.VoteCount != 1 {
		t.Fatalf("tenant detail counts = customers:%d accounts:%d votes:%d, want 1/1/1",
			detail.Summary.CustomerCount, detail.Summary.AccountCount, detail.Summary.VoteCount)
	}
	if len(detail.CustomerLinks) != 1 || detail.CustomerLinks[0].ID != customer.ID {
		t.Fatalf("customer links = %+v, want only %s", detail.CustomerLinks, customer.ID)
	}
	if len(detail.Votes) != 1 || detail.Votes[0].ID != vote.ID {
		t.Fatalf("votes = %+v, want only %s", detail.Votes, vote.ID)
	}

	_, err = e.repo.GetDetail(e.ctx, otherTenant, request.ID, 50)
	if !errors.Is(err, crrepo.ErrNotFound) {
		t.Fatalf("cross-tenant GetDetail error = %v, want ErrNotFound", err)
	}
	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   otherTenant,
		Visibility: crrepo.VisibilityAll,
	})
	if err != nil {
		t.Fatalf("List other tenant: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("other tenant list len = %d, want 0", len(list.Items))
	}
}

func TestPGCustomerRequestDecisionIntelligence(t *testing.T) {
	e := setup(t)
	fixture := seedDecisionIntelligence(t, e)
	detail := assertDecisionIntelligenceDetail(t, e, fixture)
	assertDecisionScoreSort(t, e, fixture.highValue.ID, detail.Summary.DecisionScore)
}

func TestPGCustomerRequestScoringSettingsCustomizeDecisionScore(t *testing.T) {
	e := setup(t)
	highRevenue := e.createRequest(t, e.tenantID, "High revenue request")
	broadFeedback := e.createRequest(t, e.tenantID, "Broad feedback request")
	feedbackIDs := []int64{
		e.seedFeedback(t, e.tenantID, "broad-user-1", "broad-subject-1"),
		e.seedFeedback(t, e.tenantID, "broad-user-2", "broad-subject-2"),
		e.seedFeedback(t, e.tenantID, "broad-user-3", "broad-subject-3"),
	}

	tx := e.begin(t)
	_, err := e.repo.LinkCustomerTx(e.ctx, tx, crrepo.CustomerLinkInput{
		TenantID:       e.tenantID,
		RequestID:      highRevenue.ID,
		AccountKey:     "account:large",
		AccountDisplay: "Large Account",
		ActorID:        "operator",
		AccountProfile: crrepo.AccountProfileInput{
			AccountKey:      "account:large",
			AccountDisplay:  "Large Account",
			RevenueCents:    50_000_000,
			RevenueCurrency: "USD",
			Source:          "manual",
			ActorID:         "operator",
		},
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("LinkCustomerTx high revenue: %v", err)
	}
	for _, feedbackID := range feedbackIDs {
		linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, broadFeedback.ID, feedbackID)
	}
	commit(t, e.ctx, tx)

	defaults, err := e.repo.GetScoringSettings(e.ctx, e.tenantID)
	if err != nil {
		t.Fatalf("GetScoringSettings defaults: %v", err)
	}
	if defaults.FeedbackWeight != 2 || defaults.RevenueCentsPerPoint != 100000 {
		t.Fatalf("default scoring settings = %+v, want current formula defaults", defaults)
	}

	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	feedbackWeight := 100
	feedbackCap := 1000
	updated, err := service.UpdateScoringSettings(e.ctx, crsvc.ScoringSettingsInput{
		TenantID:       e.tenantID,
		FeedbackWeight: ptrext.Of(feedbackWeight),
		FeedbackCap:    ptrext.Of(feedbackCap),
		Actor:          auditlogsvc.Actor{Type: "delegated_admin", ID: "planner-1"},
	})
	if err != nil {
		t.Fatalf("UpdateScoringSettings: %v", err)
	}
	if updated.FeedbackWeight != feedbackWeight || updated.FeedbackCap != feedbackCap {
		t.Fatalf("updated scoring settings = %+v, want feedback-heavy formula", updated)
	}

	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   e.tenantID,
		Visibility: crrepo.VisibilityAll,
		Sort:       crrepo.SortDecisionScore,
		Direction:  crrepo.DirectionDesc,
	})
	if err != nil {
		t.Fatalf("List by custom decision score: %v", err)
	}
	if len(list.Items) < 2 || list.Items[0].ID != broadFeedback.ID {
		t.Fatalf("custom decision-score order = %+v, want broad feedback first", list.Items)
	}
	if list.Items[0].DecisionScore <= list.Items[1].DecisionScore {
		t.Fatalf("custom score did not rank broad request higher: %+v", list.Items[:2])
	}
	assertScoringSettingsAuditRows(t, auditRepo, e.tenantID)
}

func TestPGCustomerRequestDeliveryHealthRollup(t *testing.T) {
	e := setup(t)
	failed := e.createRequest(t, e.tenantID, "Failed sync request")
	stale := e.createRequest(t, e.tenantID, "Stale sync request")
	pending := e.createRequest(t, e.tenantID, "Pending sync request")
	manual := e.createRequest(t, e.tenantID, "Manual sync request")
	synced := e.createRequest(t, e.tenantID, "Synced sync request")
	noLinks := e.createRequest(t, e.tenantID, "No issue links request")

	tx := e.begin(t)
	seedIssueState(t, e, tx, failed.ID, "failed", crrepo.IssueSyncStateFailed)
	seedIssueState(t, e, tx, stale.ID, "stale", crrepo.IssueSyncStateStale)
	seedIssueState(t, e, tx, pending.ID, "pending", crrepo.IssueSyncStatePending)
	seedIssueState(t, e, tx, manual.ID, "manual", crrepo.IssueSyncStateManual)
	seedIssueState(t, e, tx, synced.ID, "synced", crrepo.IssueSyncStateSynced)
	commit(t, e.ctx, tx)

	assertDeliveryHealth(t, e, failed.ID, crrepo.DeliveryHealthFailed, 0, 0, 1, 0, 0)
	assertDeliveryHealth(t, e, stale.ID, crrepo.DeliveryHealthStale, 0, 1, 0, 0, 0)
	assertDeliveryHealth(t, e, pending.ID, crrepo.DeliveryHealthPending, 0, 0, 0, 1, 0)
	assertDeliveryHealth(t, e, manual.ID, crrepo.DeliveryHealthManual, 0, 0, 0, 0, 1)
	assertDeliveryHealth(t, e, synced.ID, crrepo.DeliveryHealthSynced, 1, 0, 0, 0, 0)
	assertDeliveryHealth(t, e, noLinks.ID, crrepo.DeliveryHealthNoLinks, 0, 0, 0, 0, 0)
	assertDeliveryHealthSort(t, e, failed.ID, stale.ID, pending.ID)
}

func TestPGCustomerRequestLinkGitHubIssueByConnectionAndNumber(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Managed existing GitHub issue")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	detail, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:     e.tenantID,
		RequestID:    request.ID,
		ConnectionID: ptrext.Of(connectionID),
		MappingID:    ptrext.Of(mappingID),
		IssueNumber:  "00212",
		Actor:        auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("LinkIssue by connection and number: %v", err)
	}
	if len(detail.Request.IssueLinks) != 1 {
		t.Fatalf("issue links len = %d, want one", len(detail.Request.IssueLinks))
	}
	link := detail.Request.IssueLinks[0]
	if link.ExternalKey != "Phixsura/attune#212" ||
		link.ExternalURL != "https://github.com/Phixsura/attune/issues/212" ||
		link.SyncState != crrepo.IssueSyncStatePending {
		t.Fatalf("issue link = %+v; want managed pending GitHub issue link", link)
	}
	assertManagedGitHubExternalObjectLink(t, e, request.ID, mappingID, "212", link.ExternalURL)
	assertManagedGitHubIssuePullRun(t, e, request.ID, connectionID, mappingID, "212")
}

func TestPGCustomerRequestDeliveryGraphUsesExternalObjectPayload(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Delivery graph payload request")
	_, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	linked, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/228",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("LinkIssue managed URL: %v", err)
	}
	if len(linked.Request.IssueLinks) != 1 {
		t.Fatalf("issue links len = %d, want one", len(linked.Request.IssueLinks))
	}

	payloadUpdatedAt := time.Date(2026, 7, 7, 1, 30, 0, 0, time.UTC)
	updateExternalObjectPayload(t, e, request.ID, mappingID, "228", payloadUpdatedAt)

	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail with external object payload: %v", err)
	}
	if detail.DeliveryGraph.Health != crrepo.DeliveryHealthFailed {
		t.Fatalf("delivery graph health = %q, want failed", detail.DeliveryGraph.Health)
	}
	if len(detail.DeliveryGraph.Artifacts) != 2 {
		t.Fatalf("delivery graph artifacts len = %d, want 2", len(detail.DeliveryGraph.Artifacts))
	}
	artifact := detail.DeliveryGraph.Artifacts[1]
	if artifact.Source != "external_object_link" || artifact.Title != "Provider payload title" {
		t.Fatalf("delivery artifact = %+v, want external payload source/title", artifact)
	}
	if artifact.Status != "closed" || artifact.StatusCategory != "completed" {
		t.Fatalf("delivery artifact status = %q/%q, want closed/completed", artifact.Status, artifact.StatusCategory)
	}
	if artifact.Assignee != "octo, hubot" || artifact.SyncError != "secondary rate limit" {
		t.Fatalf("delivery artifact assignee/error = %q/%q", artifact.Assignee, artifact.SyncError)
	}
	if artifact.LastSeenAt == nil || !artifact.LastSeenAt.Equal(payloadUpdatedAt) {
		t.Fatalf("delivery artifact last seen = %+v, want %s", artifact.LastSeenAt, payloadUpdatedAt)
	}
}

func TestPGCustomerRequestDeliveryGraphUsesProjectedArtifacts(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Delivery graph projected artifact request")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	if _, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/229",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	}); err != nil {
		t.Fatalf("LinkIssue managed URL: %v", err)
	}
	prSeenAt := time.Date(2026, 7, 7, 2, 0, 0, 0, time.UTC)
	insertProjectedDeliveryArtifact(t, e, request.ID, connectionID, mappingID, prSeenAt)

	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail with projected delivery artifact: %v", err)
	}
	if detail.DeliveryGraph.Health != crrepo.DeliveryHealthFailed {
		t.Fatalf("delivery graph health = %q, want failed", detail.DeliveryGraph.Health)
	}
	if detail.DeliveryGraph.HealthExplanation != "2 linked artifacts: 1 failed, 1 pending." {
		t.Fatalf("delivery graph explanation = %q", detail.DeliveryGraph.HealthExplanation)
	}
	if len(detail.DeliveryGraph.Artifacts) != 3 || len(detail.DeliveryGraph.Relationships) != 2 {
		t.Fatalf("delivery graph = %+v, want root plus issue plus PR with two relationships", detail.DeliveryGraph)
	}
	artifact := detail.DeliveryGraph.Artifacts[2]
	if artifact.ArtifactType != "pull_request" || artifact.Title != "Implement delivery graph projection" {
		t.Fatalf("projected artifact = %+v, want PR projection", artifact)
	}
	if artifact.Health != crrepo.DeliveryHealthFailed || artifact.SyncError != "merge conflict" {
		t.Fatalf("projected artifact health/error = %q/%q", artifact.Health, artifact.SyncError)
	}
	if artifact.LastSeenAt == nil || !artifact.LastSeenAt.Equal(prSeenAt) {
		t.Fatalf("projected artifact last seen = %+v, want %s", artifact.LastSeenAt, prSeenAt)
	}
	if detail.DeliveryGraph.Relationships[1].RelationshipType != "implements" {
		t.Fatalf("projected relationship = %+v, want implements", detail.DeliveryGraph.Relationships[1])
	}
}

func TestPGCustomerRequestLinkGitHubIssueURLQueuesManagedPullRun(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Managed GitHub issue by URL")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	detail, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/213",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("LinkIssue by GitHub URL: %v", err)
	}
	if len(detail.Request.IssueLinks) != 1 {
		t.Fatalf("issue links len = %d, want one", len(detail.Request.IssueLinks))
	}
	link := detail.Request.IssueLinks[0]
	if link.ExternalKey != "Phixsura/attune#213" ||
		link.ExternalURL != "https://github.com/Phixsura/attune/issues/213" ||
		link.SyncState != crrepo.IssueSyncStatePending {
		t.Fatalf("issue link = %+v; want managed pending GitHub URL link", link)
	}
	assertManagedGitHubExternalObjectLink(t, e, request.ID, mappingID, "213", link.ExternalURL)
	assertManagedGitHubIssuePullRun(t, e, request.ID, connectionID, mappingID, "213")
}

func TestPGCustomerRequestUnlinkManagedGitHubIssueTombstonesLocalLink(t *testing.T) {
	e := setup(t)
	first := e.createRequest(t, e.tenantID, "Managed GitHub issue to unlink")
	second := e.createRequest(t, e.tenantID, "Managed GitHub issue rebind target")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))
	actor := auditlogsvc.Actor{Type: "admin", ID: "operator-1"}

	detail, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   first.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/219",
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("LinkIssue initial managed URL: %v", err)
	}
	if len(detail.Request.IssueLinks) != 1 {
		t.Fatalf("initial issue links len = %d, want one", len(detail.Request.IssueLinks))
	}

	if _, err := service.UnlinkIssue(e.ctx, e.tenantID, first.ID, detail.Request.IssueLinks[0].ID, actor); err != nil {
		t.Fatalf("UnlinkIssue managed URL: %v", err)
	}
	assertIssueLinkCount(t, e, first.ID, 0)
	assertLocalGitHubExternalObjectLinkTombstone(t, e, first.ID, mappingID, "219")

	rebuilt, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   second.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/219",
		Actor:       actor,
	})
	if err != nil {
		t.Fatalf("LinkIssue rebind managed URL: %v", err)
	}
	if len(rebuilt.Request.IssueLinks) != 1 {
		t.Fatalf("rebound issue links len = %d, want one", len(rebuilt.Request.IssueLinks))
	}
	assertIssueLinkCount(t, e, first.ID, 0)
	assertManagedGitHubExternalObjectLink(t, e, second.ID, mappingID, "219", "https://github.com/Phixsura/attune/issues/219")
	assertManagedGitHubIssuePullRun(t, e, second.ID, connectionID, mappingID, "219")
}

func TestPGCustomerRequestLinkGitHubIssueURLWithPushOnlyMappingStaysManual(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Manual GitHub link stays manual")
	connectionID, mappingID := e.seedGitHubExternalIssueMappingWithDirection(
		t,
		"https://github.com/Phixsura/attune",
		"push",
	)
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	detail, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/214",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("LinkIssue with push-only mapping: %v", err)
	}
	if len(detail.Request.IssueLinks) != 1 {
		t.Fatalf("issue links len = %d, want one", len(detail.Request.IssueLinks))
	}
	link := detail.Request.IssueLinks[0]
	if link.SyncState != crrepo.IssueSyncStateManual {
		t.Fatalf("issue link sync state = %s, want manual", link.SyncState)
	}
	assertIssueLinkNotManaged(t, e, link.ID)
	assertNoManagedGitHubExternalObjectLink(t, e, request.ID, mappingID)
	assertNoExternalSyncRuns(t, e, connectionID, mappingID)
}

func TestPGCustomerRequestLinkGitHubIssueURLConflictsWhenExternalIssueAlreadyManaged(t *testing.T) {
	e := setup(t)
	first := e.createRequest(t, e.tenantID, "First managed GitHub link")
	second := e.createRequest(t, e.tenantID, "Second managed GitHub link")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	firstDetail, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   first.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/215",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("LinkIssue first managed URL: %v", err)
	}
	if len(firstDetail.Request.IssueLinks) != 1 {
		t.Fatalf("first issue links len = %d, want one", len(firstDetail.Request.IssueLinks))
	}

	_, err = service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   second.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/215",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrConflict) {
		t.Fatalf("LinkIssue duplicate managed URL error = %v, want ErrConflict", err)
	}
	assertManagedGitHubExternalObjectLink(t, e, first.ID, mappingID, "215", "https://github.com/Phixsura/attune/issues/215")
	assertManagedGitHubIssuePullRun(t, e, first.ID, connectionID, mappingID, "215")
	assertIssueLinkCount(t, e, second.ID, 0)
}

func TestPGCustomerRequestLinkGitHubIssueURLConflictsWhenRequestAlreadyManagedToDifferentIssue(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Already managed GitHub link")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	if _, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/217",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	}); err != nil {
		t.Fatalf("LinkIssue first managed URL: %v", err)
	}

	_, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/218",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrConflict) {
		t.Fatalf("LinkIssue second managed URL error = %v, want ErrConflict", err)
	}
	assertIssueLinkCount(t, e, request.ID, 1)
	assertManagedGitHubExternalObjectLink(t, e, request.ID, mappingID, "217", "https://github.com/Phixsura/attune/issues/217")
	assertExternalSyncRunCount(t, e, connectionID, mappingID, 1)
}

func TestPGCustomerRequestLinkGitHubIssueURLConflictsWhenLocalTombstoneTargetsRequestWithDifferentActiveIssue(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Active managed link with old tombstone")
	connectionID, mappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))
	actor := auditlogsvc.Actor{Type: "admin", ID: "operator-1"}

	if _, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/220",
		Actor:       actor,
	}); err != nil {
		t.Fatalf("LinkIssue active managed URL: %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, sync_state,
		  local_deleted_at, tombstone_reason)
		VALUES ($1, $2, $3, 'customer_request', $4, 'issue', '221',
		        'https://github.com/Phixsura/attune/issues/221', 'deleted',
		        NOW(), 'local_unlinked')`,
		uuid.New(), e.tenantID, mappingID, request.ID.String()); err != nil {
		t.Fatalf("insert local tombstone: %v", err)
	}

	_, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/221",
		Actor:       actor,
	})
	if !errors.Is(err, crrepo.ErrConflict) {
		t.Fatalf("LinkIssue local tombstone with active link error = %v, want ErrConflict", err)
	}
	assertIssueLinkCount(t, e, request.ID, 1)
	assertManagedGitHubExternalObjectLink(t, e, request.ID, mappingID, "220", "https://github.com/Phixsura/attune/issues/220")
	assertExternalSyncRunCount(t, e, connectionID, mappingID, 1)
}

func TestPGCustomerRequestLinkGitHubIssueURLConflictsWhenRepositoryMappingIsAmbiguous(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Ambiguous managed GitHub link")
	firstConnectionID, firstMappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	secondConnectionID, secondMappingID := e.seedGitHubExternalIssueMapping(t, "https://github.com/Phixsura/attune")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	service.SetIssueCreateRunStore(externalsyncrepo.New(e.pool))

	_, err := service.LinkIssue(e.ctx, crsvc.LinkIssueInput{
		TenantID:    e.tenantID,
		RequestID:   request.ID,
		Provider:    "github",
		ExternalURL: "https://github.com/Phixsura/attune/issues/216",
		Actor:       auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrConflict) {
		t.Fatalf("LinkIssue ambiguous managed URL error = %v, want ErrConflict", err)
	}
	assertIssueLinkCount(t, e, request.ID, 0)
	assertNoManagedGitHubExternalObjectLink(t, e, request.ID, firstMappingID)
	assertNoManagedGitHubExternalObjectLink(t, e, request.ID, secondMappingID)
	assertNoExternalSyncRuns(t, e, firstConnectionID, firstMappingID)
	assertNoExternalSyncRuns(t, e, secondConnectionID, secondMappingID)
}

func TestPGCustomerRequestNotesWriteAuditAndTouchRequest(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Noted request")
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, nil, auditlogsvc.New(auditRepo))
	actor := auditlogsvc.Actor{
		Type:  "admin",
		ID:    "operator-1",
		Email: "operator@example.com",
	}

	time.Sleep(10 * time.Millisecond)
	added, err := service.AddNote(e.ctx, crsvc.NoteInput{
		TenantID:  e.tenantID,
		RequestID: request.ID,
		Body:      "  Prioritize after ACME review.  ",
		Actor:     actor,
	})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if len(added.Request.Notes) != 1 {
		t.Fatalf("notes len after add = %d, want 1", len(added.Request.Notes))
	}
	note := added.Request.Notes[0]
	if note.Body != "Prioritize after ACME review." || note.CreatedBy != "operator-1" {
		t.Fatalf("added note = %+v, want trimmed body and actor", note)
	}
	if added.Request.Summary.UpdatedBy != "operator-1" || !added.Request.Summary.UpdatedAt.After(request.UpdatedAt) {
		t.Fatalf("add note touch = by:%q at:%s, want operator-1 after %s",
			added.Request.Summary.UpdatedBy, added.Request.Summary.UpdatedAt, request.UpdatedAt)
	}

	deleted, err := service.DeleteNote(e.ctx, e.tenantID, request.ID, note.ID, actor)
	if err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if len(deleted.Request.Notes) != 0 {
		t.Fatalf("notes len after delete = %d, want 0", len(deleted.Request.Notes))
	}
	assertNoteAuditRows(t, auditRepo, e.tenantID, request.ID.String())
}

type decisionIntelligenceFixture struct {
	highValue crrepo.Summary
	lowValue  crrepo.Summary
	issue     *crrepo.IssueLink
}

func seedDecisionIntelligence(t *testing.T, e env) decisionIntelligenceFixture {
	t.Helper()
	highValue := e.createRequest(t, e.tenantID, "Revenue-backed request")
	lowValue := e.createRequest(t, e.tenantID, "Low evidence request")

	externalUpdatedAt := time.Date(2026, 7, 7, 9, 30, 0, 0, time.UTC)
	tx := e.begin(t)
	_, err := e.repo.LinkCustomerTx(e.ctx, tx, crrepo.CustomerLinkInput{
		TenantID:       e.tenantID,
		RequestID:      highValue.ID,
		SubjectKey:     "user:ada",
		SubjectDisplay: "Ada Lovelace",
		AccountKey:     "account:acme",
		AccountDisplay: "Acme Inc",
		ActorID:        "operator",
		AccountProfile: crrepo.AccountProfileInput{
			AccountKey:      "account:acme",
			AccountDisplay:  "Acme Inc",
			RevenueCents:    2_400_000,
			RevenueCurrency: "USD",
			Tier:            "enterprise",
			SizeSegment:     "mid_market",
			LifecycleStatus: "active",
			CRMProvider:     "salesforce",
			CRMExternalID:   "001",
			Source:          "manual",
			ActorID:         "operator",
		},
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("LinkCustomerTx: %v", err)
	}
	_, err = e.repo.AddVoteTx(e.ctx, tx, crrepo.VoteInput{
		TenantID:   e.tenantID,
		RequestID:  highValue.ID,
		SubjectKey: "user:ada",
		AccountKey: "account:acme",
		Weight:     5,
		ActorID:    "operator",
		AccountProfile: crrepo.AccountProfileInput{
			AccountKey:      "account:acme",
			AccountDisplay:  "Acme Inc",
			RevenueCents:    2_400_000,
			RevenueCurrency: "USD",
			Source:          "manual",
			ActorID:         "operator",
		},
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("AddVoteTx: %v", err)
	}
	issue, err := e.repo.LinkIssueTx(e.ctx, tx, crrepo.IssueLinkInput{
		TenantID:    e.tenantID,
		RequestID:   highValue.ID,
		Provider:    "github",
		ExternalKey: "Phixsura/attune#212",
		ExternalURL: "https://github.com/Phixsura/attune/issues/212",
		Title:       "Customer Requests",
		Status:      "open",
		ActorID:     "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("LinkIssueTx: %v", err)
	}
	issue, err = e.repo.RecordIssueSyncTx(e.ctx, tx, crrepo.IssueSyncInput{
		TenantID:               e.tenantID,
		RequestID:              highValue.ID,
		IssueLinkID:            issue.ID,
		SyncState:              crrepo.IssueSyncStateSynced,
		Status:                 "closed",
		ExternalStatusCategory: "done",
		ExternalAssignee:       "product-eng",
		ExternalUpdatedAt:      &externalUpdatedAt,
		ActorID:                "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("RecordIssueSyncTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return decisionIntelligenceFixture{
		highValue: highValue,
		lowValue:  lowValue,
		issue:     issue,
	}
}

func assertDecisionIntelligenceDetail(
	t *testing.T,
	e env,
	fixture decisionIntelligenceFixture,
) *crrepo.Detail {
	t.Helper()
	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, fixture.highValue.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail high value: %v", err)
	}
	if detail.Summary.RevenueImpactCents != 2_400_000 {
		t.Fatalf("RevenueImpactCents = %d, want 2400000", detail.Summary.RevenueImpactCents)
	}
	if detail.Summary.DecisionScore <= fixture.lowValue.DecisionScore {
		t.Fatalf("DecisionScore = %d, want above low value %d", detail.Summary.DecisionScore, fixture.lowValue.DecisionScore)
	}
	if len(detail.AccountProfiles) != 1 || detail.AccountProfiles[0].Tier != "enterprise" {
		t.Fatalf("AccountProfiles = %+v, want enterprise Acme profile", detail.AccountProfiles)
	}
	if len(detail.IssueLinks) != 1 || detail.IssueLinks[0].SyncState != crrepo.IssueSyncStateSynced {
		t.Fatalf("IssueLinks = %+v, want synced issue link", detail.IssueLinks)
	}
	if detail.IssueLinks[0].Status != "closed" || detail.IssueLinks[0].ExternalStatusCategory != "done" {
		t.Fatalf("issue sync status = %q/%q, want closed/done", detail.IssueLinks[0].Status, detail.IssueLinks[0].ExternalStatusCategory)
	}
	if detail.Summary.DeliveryHealth != crrepo.DeliveryHealthSynced || detail.Summary.SyncedIssueCount != 1 {
		t.Fatalf("delivery health = %s synced=%d, want synced/1",
			detail.Summary.DeliveryHealth, detail.Summary.SyncedIssueCount)
	}
	if fixture.issue.LastSyncedAt == nil {
		t.Fatalf("RecordIssueSyncTx returned nil LastSyncedAt")
	}
	return detail
}

func assertDecisionScoreSort(t *testing.T, e env, highValueID uuid.UUID, highScore int) {
	t.Helper()
	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   e.tenantID,
		Visibility: crrepo.VisibilityAll,
		Sort:       crrepo.SortDecisionScore,
		Direction:  crrepo.DirectionDesc,
	})
	if err != nil {
		t.Fatalf("List by decision score: %v", err)
	}
	if len(list.Items) < 2 || list.Items[0].ID != highValueID {
		t.Fatalf("top decision score item = %+v, want %s first after score %d",
			list.Items, highValueID, highScore)
	}
}

func seedIssueState(
	t *testing.T,
	e env,
	tx pgx.Tx,
	requestID uuid.UUID,
	key string,
	state crrepo.IssueSyncState,
) {
	t.Helper()
	issue, err := e.repo.LinkIssueTx(e.ctx, tx, crrepo.IssueLinkInput{
		TenantID:    e.tenantID,
		RequestID:   requestID,
		Provider:    "other",
		ExternalKey: key,
		ExternalURL: "https://example.com/" + key,
		Title:       key,
		ActorID:     "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("LinkIssueTx %s: %v", key, err)
	}
	if state == crrepo.IssueSyncStateManual {
		return
	}
	_, err = e.repo.RecordIssueSyncTx(e.ctx, tx, crrepo.IssueSyncInput{
		TenantID:    e.tenantID,
		RequestID:   requestID,
		IssueLinkID: issue.ID,
		SyncState:   state,
		Status:      string(state),
		ActorID:     "operator",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("RecordIssueSyncTx %s: %v", key, err)
	}
}

func assertDeliveryHealth(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	health crrepo.DeliveryHealth,
	synced, stale, failed, pending, manual int,
) {
	t.Helper()
	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, requestID, 50)
	if err != nil {
		t.Fatalf("GetDetail delivery health: %v", err)
	}
	summary := detail.Summary
	if summary.DeliveryHealth != health ||
		summary.SyncedIssueCount != synced ||
		summary.StaleIssueCount != stale ||
		summary.FailedIssueCount != failed ||
		summary.PendingIssueCount != pending ||
		summary.ManualIssueCount != manual {
		t.Fatalf("delivery health summary = %s synced:%d stale:%d failed:%d pending:%d manual:%d",
			summary.DeliveryHealth,
			summary.SyncedIssueCount,
			summary.StaleIssueCount,
			summary.FailedIssueCount,
			summary.PendingIssueCount,
			summary.ManualIssueCount)
	}
}

func assertDeliveryHealthSort(t *testing.T, e env, failedID, staleID, pendingID uuid.UUID) {
	t.Helper()
	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   e.tenantID,
		Visibility: crrepo.VisibilityAll,
		Sort:       crrepo.SortDeliveryHealth,
		Direction:  crrepo.DirectionDesc,
	})
	if err != nil {
		t.Fatalf("List by delivery health: %v", err)
	}
	if len(list.Items) < 3 {
		t.Fatalf("delivery health list len = %d, want at least 3", len(list.Items))
	}
	if list.Items[0].ID != failedID || list.Items[1].ID != staleID || list.Items[2].ID != pendingID {
		t.Fatalf("delivery health order = %s, %s, %s; want failed/stale/pending",
			list.Items[0].ID, list.Items[1].ID, list.Items[2].ID)
	}
}

func assertNoteAuditRows(t *testing.T, auditRepo *auditlogrepo.Repo, tenantID, requestID string) {
	t.Helper()
	rows, err := auditRepo.List(context.Background(), auditlogrepo.ListFilter{
		TenantID:   tenantID,
		Actions:    []string{"customer_request.add_note", "customer_request.delete_note"},
		TargetType: "customer_request",
		TargetID:   requestID,
		Unbounded:  true,
	})
	if err != nil {
		t.Fatalf("List note audit rows: %v", err)
	}
	if len(rows.Items) != 2 {
		t.Fatalf("note audit rows len = %d, want 2", len(rows.Items))
	}
	for _, row := range rows.Items {
		if bytes.Contains(row.AfterJSON, []byte("Prioritize after ACME review")) {
			t.Fatalf("note audit row leaked body: %s", string(row.AfterJSON))
		}
		var after map[string]any
		if err := json.Unmarshal(row.AfterJSON, &after); err != nil {
			t.Fatalf("unmarshal note audit after json: %v", err)
		}
		if after["note_id"] == "" || after["body_length"] == nil {
			t.Fatalf("note audit after = %v, want note_id and body_length", after)
		}
	}
}

func assertScoringSettingsAuditRows(t *testing.T, auditRepo *auditlogrepo.Repo, tenantID string) {
	t.Helper()
	rows, err := auditRepo.List(context.Background(), auditlogrepo.ListFilter{
		TenantID:   tenantID,
		Actions:    []string{"customer_request.update_scoring_settings"},
		TargetType: "customer_request_scoring_settings",
		TargetID:   tenantID,
		Unbounded:  true,
	})
	if err != nil {
		t.Fatalf("List scoring settings audit rows: %v", err)
	}
	if len(rows.Items) != 1 {
		t.Fatalf("scoring settings audit rows len = %d, want 1", len(rows.Items))
	}
	var after map[string]any
	if err := json.Unmarshal(rows.Items[0].AfterJSON, &after); err != nil {
		t.Fatalf("unmarshal scoring settings audit after json: %v", err)
	}
	if after["feedback_weight"] != float64(100) || after["feedback_cap"] != float64(1000) {
		t.Fatalf("scoring settings audit after = %+v, want feedback formula", after)
	}
}

func TestPGCustomerRequestOwnerMustBelongToTenant(t *testing.T) {
	e := setup(t)
	otherTenant := e.createTenant(t, "customer-request-owner-other")
	validOwnerID := e.createMember(t, e.tenantID, "owner-user", "owner@example.com")
	foreignOwnerID := e.createMember(t, otherTenant, "foreign-owner", "foreign-owner@example.com")
	service := crsvc.New(e.repo, idempotencyrepo.New(e.pool), nil)

	created, err := service.Create(e.ctx, crsvc.CreateInput{
		TenantID:       e.tenantID,
		Title:          "Owned request",
		Description:    "Request with a tenant-local owner",
		Status:         crrepo.StatusOpen,
		Priority:       crrepo.PriorityMedium,
		OwnerMemberID:  &validOwnerID,
		IdempotencyKey: "owner_ok_1",
		Actor:          auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if err != nil {
		t.Fatalf("Create with tenant owner: %v", err)
	}
	if created.Request.Summary.OwnerMemberID == nil || *created.Request.Summary.OwnerMemberID != validOwnerID {
		t.Fatalf("OwnerMemberID = %v, want %s", created.Request.Summary.OwnerMemberID, validOwnerID)
	}

	_, err = service.Create(e.ctx, crsvc.CreateInput{
		TenantID:       e.tenantID,
		Title:          "Foreign owned request",
		Description:    "Should not be created",
		Status:         crrepo.StatusOpen,
		Priority:       crrepo.PriorityNone,
		OwnerMemberID:  &foreignOwnerID,
		IdempotencyKey: "owner_bad_create",
		Actor:          auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrOwnerNotFound) {
		t.Fatalf("Create foreign owner error = %v, want ErrOwnerNotFound", err)
	}

	feedbackID := e.seedFeedback(t, e.tenantID, "owner-promote-user", "owner-promote-subject")
	_, err = service.PromoteFeedback(e.ctx, crsvc.PromoteInput{
		TenantID:       e.tenantID,
		FeedbackIDs:    []int64{feedbackID},
		Title:          "Foreign owner promotion",
		Description:    "Should not be promoted",
		Status:         crrepo.StatusOpen,
		Priority:       crrepo.PriorityNone,
		OwnerMemberID:  &foreignOwnerID,
		IdempotencyKey: "owner_bad_promote",
		Actor:          auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrOwnerNotFound) {
		t.Fatalf("PromoteFeedback foreign owner error = %v, want ErrOwnerNotFound", err)
	}

	_, err = service.Update(e.ctx, crsvc.UpdateInput{
		TenantID:         e.tenantID,
		ID:               created.Request.Summary.ID,
		OwnerMemberIDSet: true,
		OwnerMemberID:    &foreignOwnerID,
		Actor:            auditlogsvc.Actor{Type: "admin", ID: "operator-1"},
	})
	if !errors.Is(err, crrepo.ErrOwnerNotFound) {
		t.Fatalf("Update foreign owner error = %v, want ErrOwnerNotFound", err)
	}

	assertRequestCount(t, e, 1)
	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, created.Request.Summary.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail owned request: %v", err)
	}
	if detail.Summary.OwnerMemberID == nil || *detail.Summary.OwnerMemberID != validOwnerID {
		t.Fatalf("post-failure owner = %v, want %s", detail.Summary.OwnerMemberID, validOwnerID)
	}
}

func TestPGCustomerRequestBacklinkWritesTouchRequest(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Touch request")
	feedbackID := e.seedFeedback(t, e.tenantID, "touch-user", "touch-subject")

	time.Sleep(10 * time.Millisecond)
	tx := e.begin(t)
	err := e.repo.LinkFeedbackTx(e.ctx, tx, crrepo.LinkFeedbackInput{
		TenantID:   e.tenantID,
		RequestID:  request.ID,
		FeedbackID: feedbackID,
		Importance: crrepo.ImportanceNormal,
		ActorID:    "support-agent",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("LinkFeedbackTx: %v", err)
	}
	commit(t, e.ctx, tx)

	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.Summary.UpdatedBy != "support-agent" {
		t.Fatalf("UpdatedBy = %q, want support-agent", detail.Summary.UpdatedBy)
	}
	if !detail.Summary.UpdatedAt.After(request.UpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want after %s", detail.Summary.UpdatedAt, request.UpdatedAt)
	}
}

func TestPGCustomerRequestListByFeedbackID(t *testing.T) {
	e := setup(t)
	first := e.createRequest(t, e.tenantID, "First linked request")
	second := e.createRequest(t, e.tenantID, "Second linked request")
	firstFeedbackID := e.seedFeedback(t, e.tenantID, "first-user", "first-subject")
	secondFeedbackID := e.seedFeedback(t, e.tenantID, "second-user", "second-subject")

	tx := e.begin(t)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, first.ID, firstFeedbackID)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, second.ID, secondFeedbackID)
	commit(t, e.ctx, tx)

	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   e.tenantID,
		Visibility: crrepo.VisibilityAll,
		FeedbackID: firstFeedbackID,
	})
	if err != nil {
		t.Fatalf("List by feedback id: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("List len = %d, want 1", len(list.Items))
	}
	if list.Items[0].ID != first.ID {
		t.Fatalf("List item = %s, want %s", list.Items[0].ID, first.ID)
	}
}

func TestPGCustomerRequestSoftDeletedFeedbackIsHiddenButUnlinkable(t *testing.T) {
	e := setup(t)
	request := e.createRequest(t, e.tenantID, "Soft deleted feedback request")
	visibleFeedbackID := e.seedFeedback(t, e.tenantID, "visible-user", "visible-subject")
	hiddenFeedbackID := e.seedFeedback(t, e.tenantID, "hidden-user", "hidden-subject")
	rejectedFeedbackID := e.seedFeedback(t, e.tenantID, "rejected-user", "rejected-subject")

	tx := e.begin(t)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, request.ID, visibleFeedbackID)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, request.ID, hiddenFeedbackID)
	commit(t, e.ctx, tx)

	e.softDeleteFeedback(t, e.tenantID, hiddenFeedbackID)
	e.softDeleteFeedback(t, e.tenantID, rejectedFeedbackID)

	tx = e.begin(t)
	err := e.repo.LinkFeedbackTx(e.ctx, tx, crrepo.LinkFeedbackInput{
		TenantID:   e.tenantID,
		RequestID:  request.ID,
		FeedbackID: rejectedFeedbackID,
		Importance: crrepo.ImportanceNormal,
		ActorID:    "tester",
	})
	rollback(t, e.ctx, tx)
	if !errors.Is(err, crrepo.ErrFeedbackNotFound) {
		t.Fatalf("LinkFeedbackTx soft-deleted error = %v, want ErrFeedbackNotFound", err)
	}

	detail, err := e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail hidden feedback: %v", err)
	}
	if detail.Summary.SupportingFeedbackCount != 1 || detail.Summary.HiddenFeedbackCount != 1 {
		t.Fatalf("feedback counts visible=%d hidden=%d, want 1/1",
			detail.Summary.SupportingFeedbackCount, detail.Summary.HiddenFeedbackCount)
	}
	if detail.Summary.CustomerCount != 1 {
		t.Fatalf("CustomerCount = %d, want visible feedback only", detail.Summary.CustomerCount)
	}
	if len(detail.Feedback) != 1 || detail.Feedback[0].FeedbackID != visibleFeedbackID {
		t.Fatalf("visible evidence = %+v, want only %d", detail.Feedback, visibleFeedbackID)
	}

	tx = e.begin(t)
	if err := e.repo.UnlinkFeedbackTx(e.ctx, tx, e.tenantID, request.ID, hiddenFeedbackID, "tester"); err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("UnlinkFeedbackTx hidden feedback: %v", err)
	}
	commit(t, e.ctx, tx)

	detail, err = e.repo.GetDetail(e.ctx, e.tenantID, request.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail after hidden unlink: %v", err)
	}
	if detail.Summary.SupportingFeedbackCount != 1 || detail.Summary.HiddenFeedbackCount != 0 {
		t.Fatalf("post-unlink counts visible=%d hidden=%d, want 1/0",
			detail.Summary.SupportingFeedbackCount, detail.Summary.HiddenFeedbackCount)
	}
}

func TestPGCustomerRequestPromoteFeedbackWritesAuditAndIdempotency(t *testing.T) {
	e := setup(t)
	feedbackID := e.seedFeedback(t, e.tenantID, "promote-user", "promote-subject")
	idempotencyKey := "promote_audit_1"
	idempotencyRepo := idempotencyrepo.New(e.pool)
	auditRepo := auditlogrepo.New(e.pool)
	service := crsvc.New(e.repo, idempotencyRepo, auditlogsvc.New(auditRepo))
	input := crsvc.PromoteInput{
		TenantID:       e.tenantID,
		FeedbackIDs:    []int64{feedbackID},
		Title:          "Promoted request",
		Description:    "Evidence promoted from feedback",
		Status:         crrepo.StatusOpen,
		Priority:       crrepo.PriorityHigh,
		IdempotencyKey: idempotencyKey,
		Actor: auditlogsvc.Actor{
			Type:  "admin",
			ID:    "operator-1",
			Email: "operator@example.com",
		},
	}

	detail, err := service.PromoteFeedback(e.ctx, input)
	if err != nil {
		t.Fatalf("PromoteFeedback: %v", err)
	}
	if detail.Request.Summary.Title != "Promoted request" {
		t.Fatalf("promoted title = %q, want Promoted request", detail.Request.Summary.Title)
	}
	if detail.Request.Summary.SupportingFeedbackCount != 1 || len(detail.Request.Feedback) != 1 {
		t.Fatalf("feedback evidence count summary=%d detail=%d, want 1/1",
			detail.Request.Summary.SupportingFeedbackCount, len(detail.Request.Feedback))
	}
	if detail.Request.Feedback[0].FeedbackID != feedbackID {
		t.Fatalf("feedback evidence id = %d, want %d", detail.Request.Feedback[0].FeedbackID, feedbackID)
	}
	assertPromoteAuditEntry(t, detail, auditRepo, e.tenantID, feedbackID)

	key, err := idempotencyRepo.Get(e.ctx, e.tenantID, idempotencyKey)
	if err != nil {
		t.Fatalf("Get idempotency key: %v", err)
	}
	if key.Status != idempotencyrepo.StatusCompleted || key.ResponseCode != 200 || len(key.ResponseBody) == 0 {
		t.Fatalf("idempotency key = status:%s code:%d body:%d, want completed/200/body",
			key.Status, key.ResponseCode, len(key.ResponseBody))
	}
	replayed, err := service.PromoteFeedback(e.ctx, input)
	if err != nil {
		t.Fatalf("PromoteFeedback replay: %v", err)
	}
	if replayed.Request.Summary.ID != detail.Request.Summary.ID {
		t.Fatalf("replay request id = %s, want original %s", replayed.Request.Summary.ID, detail.Request.Summary.ID)
	}
	assertRequestCount(t, e, 1)
	assertPromoteAuditRowCount(t, auditRepo, e.tenantID, detail.Request.Summary.ID.String(), 1)

	conflict := input
	conflict.Title = "Different promoted request"
	_, err = service.PromoteFeedback(e.ctx, conflict)
	if !errors.Is(err, crsvc.ErrIdempotencyConflict) {
		t.Fatalf("PromoteFeedback conflict error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestPGCustomerRequestMergePreservesBacklinks(t *testing.T) {
	e := setup(t)
	target := e.createRequest(t, e.tenantID, "Target request")
	source := e.createRequest(t, e.tenantID, "Source duplicate")
	result := e.seedAndMergeRequests(t, source.ID, target.ID)
	assertMergeResult(t, result)

	targetDetail, err := e.repo.GetDetail(e.ctx, e.tenantID, target.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail target: %v", err)
	}
	assertTargetBacklinks(t, targetDetail, source.ID)

	sourceDetail, err := e.repo.GetDetail(e.ctx, e.tenantID, source.ID, 50)
	if err != nil {
		t.Fatalf("GetDetail source: %v", err)
	}
	assertSourceMerged(t, sourceDetail, target.ID)
}

func assertPromoteAuditEntry(
	t *testing.T,
	detail *crsvc.Detail,
	auditRepo *auditlogrepo.Repo,
	tenantID string,
	feedbackID int64,
) {
	t.Helper()
	if len(detail.AuditEntries) != 1 {
		t.Fatalf("detail audit entries len = %d, want 1", len(detail.AuditEntries))
	}
	if detail.AuditEntries[0].Action != "customer_request.promote_feedback" {
		t.Fatalf("detail audit action = %q, want promote", detail.AuditEntries[0].Action)
	}

	rows, err := auditRepo.List(context.Background(), auditlogrepo.ListFilter{
		TenantID:   tenantID,
		Actions:    []string{"customer_request.promote_feedback"},
		TargetType: "customer_request",
		TargetID:   detail.Request.Summary.ID.String(),
		Unbounded:  true,
	})
	if err != nil {
		t.Fatalf("List audit rows: %v", err)
	}
	if len(rows.Items) != 1 {
		t.Fatalf("audit rows len = %d, want 1", len(rows.Items))
	}
	row := rows.Items[0]
	if row.ActorType != "admin" || row.ActorID != "operator-1" {
		t.Fatalf("audit actor = %s/%s, want admin/operator-1", row.ActorType, row.ActorID)
	}
	var after map[string]any
	if err := json.Unmarshal(row.AfterJSON, &after); err != nil {
		t.Fatalf("unmarshal audit after json: %v", err)
	}
	if count, _ := after["feedback_count"].(float64); int(count) != 1 {
		t.Fatalf("audit feedback_count = %v, want 1", after["feedback_count"])
	}
	ids, _ := after["feedback_ids"].([]any)
	if len(ids) != 1 || int64(ids[0].(float64)) != feedbackID {
		t.Fatalf("audit feedback_ids = %v, want [%d]", after["feedback_ids"], feedbackID)
	}
}

func assertRequestCount(t *testing.T, e env, want int) {
	t.Helper()
	list, err := e.repo.List(e.ctx, crrepo.ListFilter{
		TenantID:   e.tenantID,
		Visibility: crrepo.VisibilityAll,
	})
	if err != nil {
		t.Fatalf("List customer requests: %v", err)
	}
	if len(list.Items) != want {
		t.Fatalf("customer request count = %d, want %d", len(list.Items), want)
	}
}

func assertPromoteAuditRowCount(t *testing.T, auditRepo *auditlogrepo.Repo, tenantID, requestID string, want int) {
	t.Helper()
	rows, err := auditRepo.List(context.Background(), auditlogrepo.ListFilter{
		TenantID:   tenantID,
		Actions:    []string{"customer_request.promote_feedback"},
		TargetType: "customer_request",
		TargetID:   requestID,
		Unbounded:  true,
	})
	if err != nil {
		t.Fatalf("List promote audit rows: %v", err)
	}
	if len(rows.Items) != want {
		t.Fatalf("promote audit row count = %d, want %d", len(rows.Items), want)
	}
}

func (e env) seedAndMergeRequests(t *testing.T, sourceID, targetID uuid.UUID) crrepo.MergeResult {
	t.Helper()
	movedFeedbackID := e.seedFeedback(t, e.tenantID, "moved-user", "moved-subject")
	duplicateFeedbackID := e.seedFeedback(t, e.tenantID, "duplicate-user", "duplicate-subject")
	tx := e.begin(t)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, sourceID, movedFeedbackID)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, sourceID, duplicateFeedbackID)
	linkFeedback(t, e.ctx, e.repo, tx, e.tenantID, targetID, duplicateFeedbackID)
	linkCustomer(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "user:moved", "account:moved")
	linkCustomer(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "user:duplicate", "account:duplicate")
	linkCustomer(t, e.ctx, e.repo, tx, e.tenantID, targetID, "user:duplicate", "account:duplicate")
	addVote(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "vote:moved", "vote-account:moved")
	addVote(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "vote:duplicate", "vote-account:duplicate")
	addVote(t, e.ctx, e.repo, tx, e.tenantID, targetID, "vote:duplicate", "vote-account:duplicate")
	addNote(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "Source discussion context")
	linkIssue(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "Phixsura/attune#212")
	linkIssue(t, e.ctx, e.repo, tx, e.tenantID, sourceID, "Phixsura/attune#213")
	linkIssue(t, e.ctx, e.repo, tx, e.tenantID, targetID, "Phixsura/attune#213")
	result, err := e.repo.MergeTx(e.ctx, tx, e.tenantID, sourceID, targetID, "tester")
	if err != nil {
		t.Fatalf("MergeTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return result
}

func assertMergeResult(t *testing.T, result crrepo.MergeResult) {
	t.Helper()
	assertMovedAndSkipped(t, "feedback", result.MovedFeedbackCount, result.SkippedDuplicateFeedbackCount)
	assertMovedAndSkipped(t, "customer", result.MovedCustomerCount, result.SkippedDuplicateCustomerCount)
	assertMovedAndSkipped(t, "vote", result.MovedVoteCount, result.SkippedDuplicateVoteCount)
	assertMovedAndSkipped(t, "issue", result.MovedIssueCount, result.SkippedDuplicateIssueCount)
	if result.MovedNoteCount != 1 {
		t.Fatalf("note moved count = %d, want 1", result.MovedNoteCount)
	}
}

func assertMovedAndSkipped(t *testing.T, label string, moved, skipped int) {
	t.Helper()
	if moved != 1 {
		t.Fatalf("%s moved count = %d, want 1", label, moved)
	}
	if skipped != 1 {
		t.Fatalf("%s skipped duplicate count = %d, want 1", label, skipped)
	}
}

func assertTargetBacklinks(t *testing.T, detail *crrepo.Detail, sourceID uuid.UUID) {
	t.Helper()
	assertLen(t, "feedback", len(detail.Feedback), 2)
	assertLen(t, "customers", len(detail.CustomerLinks), 2)
	assertLen(t, "votes", len(detail.Votes), 2)
	assertLen(t, "notes", len(detail.Notes), 1)
	assertLen(t, "issues", len(detail.IssueLinks), 2)
	if detail.Summary.DuplicateRequestCount != 1 {
		t.Fatalf("DuplicateRequestCount = %d, want 1", detail.Summary.DuplicateRequestCount)
	}
	assertLen(t, "duplicates", len(detail.Duplicates), 1)
	if detail.Duplicates[0].ID != sourceID {
		t.Fatalf("duplicate id = %s, want source %s", detail.Duplicates[0].ID, sourceID)
	}
	if detail.Notes[0].Body != "Source discussion context" {
		t.Fatalf("copied note body = %q, want source context", detail.Notes[0].Body)
	}
}

func assertLen(t *testing.T, label string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s len = %d, want %d", label, got, want)
	}
}

func assertSourceMerged(t *testing.T, detail *crrepo.Detail, targetID uuid.UUID) {
	t.Helper()
	if detail.Summary.MergedIntoRequestID == nil {
		t.Fatal("MergedIntoRequestID = nil, want target")
	}
	if *detail.Summary.MergedIntoRequestID != targetID {
		t.Fatalf("MergedIntoRequestID = %s, want %s", *detail.Summary.MergedIntoRequestID, targetID)
	}
	if detail.Summary.ArchivedAt == nil {
		t.Fatal("ArchivedAt = nil, want archived source")
	}
}

func (e env) createTenant(t *testing.T, slug string) string {
	t.Helper()
	tenantID, err := tenant.NewTenant(e.pool).Create(e.ctx, slug, slug)
	if err != nil {
		t.Fatalf("create tenant %q: %v", slug, err)
	}
	return tenantID
}

func (e env) seedGitHubExternalIssueMapping(t *testing.T, repoURL string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	return e.seedGitHubExternalIssueMappingWithDirection(t, repoURL, "bidirectional")
}

func (e env) seedGitHubExternalIssueMappingWithDirection(t *testing.T, repoURL, direction string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	keyID := "kid-" + uuid.NewString()
	if _, err := e.pool.Exec(e.ctx, `
		INSERT INTO secret_key_registry (key_id, status)
		VALUES ($1, 'ENABLED')`, keyID); err != nil {
		t.Fatalf("insert external sync key: %v", err)
	}
	connectionID := uuid.New()
	if _, err := e.pool.Exec(e.ctx, `
		INSERT INTO external_connections
		 (id, tenant_id, provider, name, enabled, status, auth_type, provider_config,
		  scopes, credential_key_id, credential_ciphertext, created_by, updated_by)
		VALUES ($1, $2, 'github', $3, TRUE, 'active', 'token', $4::jsonb,
		        ARRAY['issues'], $5, $6, 'tester', 'tester')`,
		connectionID,
		e.tenantID,
		"GitHub "+connectionID.String()[:8],
		`{"repo_url": "`+repoURL+`"}`,
		keyID,
		[]byte("ciphertext"),
	); err != nil {
		t.Fatalf("insert external connection: %v", err)
	}
	mappingID := uuid.New()
	if _, err := e.pool.Exec(e.ctx, `
		INSERT INTO external_object_mappings
		 (id, tenant_id, connection_id, local_object_type, external_object_type,
		  direction, field_mapping, status_mapping, conflict_policy, tombstone_policy)
		VALUES ($1, $2, $3, 'customer_request', 'issue', $4,
		        '{}'::jsonb, '{}'::jsonb, 'manual', 'mark_stale')`,
		mappingID, e.tenantID, connectionID, direction); err != nil {
		t.Fatalf("insert external object mapping: %v", err)
	}
	return connectionID, mappingID
}

func assertManagedGitHubExternalObjectLink(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
	externalURL string,
) {
	t.Helper()
	var gotKey, gotURL, gotState string
	if err := e.pool.QueryRow(e.ctx, `
		SELECT external_key, external_url, sync_state
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_id = $3
		   AND external_deleted_at IS NULL
		   AND local_deleted_at IS NULL`,
		e.tenantID, mappingID, requestID.String()).Scan(&gotKey, &gotURL, &gotState); err != nil {
		t.Fatalf("read managed external object link: %v", err)
	}
	if gotKey != externalKey || gotURL != externalURL || gotState != "pending" {
		t.Fatalf("external object link = key:%q url:%q state:%q; want %q/%q/pending",
			gotKey, gotURL, gotState, externalKey, externalURL)
	}
}

func updateExternalObjectPayload(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
	updatedAt time.Time,
) {
	t.Helper()
	payload := `{
		"title":"Provider payload title",
		"state":"closed",
		"state_reason":"completed",
		"html_url":"https://github.com/Phixsura/attune/issues/228",
		"updated_at":"` + updatedAt.Format(time.RFC3339) + `",
		"assignees":[{"login":"octo"},{"login":"hubot"}]
	}`
	tag, err := e.pool.Exec(e.ctx, `
		UPDATE external_object_links
		   SET external_version = $5,
		       external_updated_at = $6,
		       normalized_payload = $7::jsonb,
		       sync_state = 'failed',
		       sync_error = 'secondary rate limit',
		       last_synced_at = NOW(),
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_id = $3
		   AND external_key = $4
		   AND external_deleted_at IS NULL
		   AND local_deleted_at IS NULL`,
		e.tenantID, mappingID, requestID.String(), externalKey, updatedAt.Format(time.RFC3339),
		updatedAt, payload)
	if err != nil {
		t.Fatalf("update external object payload: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("update external object payload rows = %d, want 1", tag.RowsAffected())
	}
}

func insertProjectedDeliveryArtifact(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	connectionID uuid.UUID,
	mappingID uuid.UUID,
	seenAt time.Time,
) {
	t.Helper()
	tag, err := e.pool.Exec(e.ctx, `
		INSERT INTO customer_request_delivery_artifacts (
			tenant_id, request_id, provider, connection_id, mapping_id,
			artifact_type, relationship, external_key, external_url, display_key,
			title, status, status_category, state_reason, assignee, sync_state,
			sync_error, source, payload, external_updated_at, last_seen_at
		)
		VALUES (
			$1, $2, 'github', $3, $4,
			'pull_request', 'implements', 'Phixsura/attune#313',
			'https://github.com/Phixsura/attune/pull/313', 'PR #313',
			'Implement delivery graph projection', 'blocked', 'blocked',
			'merge_conflict', 'octo', 'failed',
			'merge conflict', 'delivery_artifact', '{"checks":"failed"}'::jsonb,
			$5, $5
		)`,
		e.tenantID, requestID, connectionID, mappingID, seenAt)
	if err != nil {
		t.Fatalf("insert projected delivery artifact: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("insert projected delivery artifact rows = %d, want 1", tag.RowsAffected())
	}
}

func assertLocalGitHubExternalObjectLinkTombstone(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
) {
	t.Helper()
	var localObjectID, syncState, tombstoneReason string
	var localDeleted, externalDeleted bool
	if err := e.pool.QueryRow(e.ctx, `
		SELECT local_object_id, sync_state, tombstone_reason,
		       local_deleted_at IS NOT NULL,
		       external_deleted_at IS NOT NULL
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_key = $3`,
		e.tenantID, mappingID, externalKey).Scan(
		&localObjectID,
		&syncState,
		&tombstoneReason,
		&localDeleted,
		&externalDeleted,
	); err != nil {
		t.Fatalf("read local external object link tombstone: %v", err)
	}
	if localObjectID != requestID.String() || syncState != "deleted" ||
		tombstoneReason != "local_unlinked" || !localDeleted || externalDeleted {
		t.Fatalf(
			"external object link tombstone = local:%q state:%q reason:%q local_deleted:%t external_deleted:%t; want local tombstone",
			localObjectID,
			syncState,
			tombstoneReason,
			localDeleted,
			externalDeleted,
		)
	}
}

func assertIssueLinkNotManaged(t *testing.T, e env, issueLinkID uuid.UUID) {
	t.Helper()
	var managed bool
	if err := e.pool.QueryRow(e.ctx, `
		SELECT external_object_link_id IS NOT NULL
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1
		   AND id = $2`,
		e.tenantID, issueLinkID).Scan(&managed); err != nil {
		t.Fatalf("read issue link managed state: %v", err)
	}
	if managed {
		t.Fatal("issue link external_object_link_id is set, want unmanaged manual link")
	}
}

func assertManagedGitHubIssuePullRun(
	t *testing.T,
	e env,
	requestID uuid.UUID,
	connectionID uuid.UUID,
	mappingID uuid.UUID,
	externalKey string,
) {
	t.Helper()
	var direction, trigger, status, gotExternalKey, gotLocalObjectID, source, actor string
	if err := e.pool.QueryRow(e.ctx, `
		SELECT direction, trigger, status,
		       input_metadata->>'external_key',
		       input_metadata->>'local_object_id',
		       input_metadata->>'source',
		       actor_id
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND connection_id = $2
		   AND mapping_id = $3
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		e.tenantID, connectionID, mappingID).Scan(
		&direction,
		&trigger,
		&status,
		&gotExternalKey,
		&gotLocalObjectID,
		&source,
		&actor,
	); err != nil {
		t.Fatalf("read managed GitHub issue pull run: %v", err)
	}
	if direction != "pull" || trigger != "manual" || status != "queued" ||
		gotExternalKey != externalKey || gotLocalObjectID != requestID.String() ||
		source != "customer_request_issue_link" || actor != "operator-1" {
		t.Fatalf("pull run = direction:%q trigger:%q status:%q external:%q local:%q source:%q actor:%q",
			direction, trigger, status, gotExternalKey, gotLocalObjectID, source, actor)
	}
}

func assertNoManagedGitHubExternalObjectLink(t *testing.T, e env, requestID uuid.UUID, mappingID uuid.UUID) {
	t.Helper()
	var count int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT COUNT(*)
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_id = $3`,
		e.tenantID, mappingID, requestID.String()).Scan(&count); err != nil {
		t.Fatalf("count managed external object links: %v", err)
	}
	if count != 0 {
		t.Fatalf("managed external object links count = %d, want 0", count)
	}
}

func assertNoExternalSyncRuns(t *testing.T, e env, connectionID uuid.UUID, mappingID uuid.UUID) {
	t.Helper()
	assertExternalSyncRunCount(t, e, connectionID, mappingID, 0)
}

func assertExternalSyncRunCount(t *testing.T, e env, connectionID uuid.UUID, mappingID uuid.UUID, want int) {
	t.Helper()
	var count int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT COUNT(*)
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND connection_id = $2
		   AND mapping_id = $3`,
		e.tenantID, connectionID, mappingID).Scan(&count); err != nil {
		t.Fatalf("count external sync runs: %v", err)
	}
	if count != want {
		t.Fatalf("external sync runs count = %d, want %d", count, want)
	}
}

func assertIssueLinkCount(t *testing.T, e env, requestID uuid.UUID, want int) {
	t.Helper()
	var count int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT COUNT(*)
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1
		   AND request_id = $2`,
		e.tenantID, requestID).Scan(&count); err != nil {
		t.Fatalf("count issue links: %v", err)
	}
	if count != want {
		t.Fatalf("issue link count = %d, want %d", count, want)
	}
}

func (e env) createMember(t *testing.T, tenantID, userID, email string) uuid.UUID {
	t.Helper()
	var rawID string
	err := e.pool.QueryRow(
		e.ctx, `
		INSERT INTO tenant_members (
			tenant_id, member_type, user_id, email, role, role_source, accepted_at
		)
		VALUES ($1, 'tenant_user', $2, $3, 'member', 'manual', NOW())
		RETURNING id::text`,
		tenantID, userID, email,
	).Scan(&rawID)
	if err != nil {
		t.Fatalf("create member %q: %v", userID, err)
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		t.Fatalf("parse member id %q: %v", rawID, err)
	}
	return id
}

func (e env) createRequest(t *testing.T, tenantID, title string) crrepo.Summary {
	t.Helper()
	tx := e.begin(t)
	created, err := e.repo.CreateTx(e.ctx, tx, crrepo.CreateInput{
		TenantID:    tenantID,
		Title:       title,
		Status:      crrepo.StatusOpen,
		Priority:    crrepo.PriorityNone,
		Description: "request description",
		ActorID:     "tester",
	})
	if err != nil {
		rollback(t, e.ctx, tx)
		t.Fatalf("CreateTx: %v", err)
	}
	commit(t, e.ctx, tx)
	return *created
}

func (e env) seedFeedback(t *testing.T, tenantID, userID, subjectKey string) int64 {
	t.Helper()
	var id int64
	err := e.pool.QueryRow(
		e.ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, subject_key, subject_hash, subject_display, source, content)
		VALUES ($1, $2, $3, $3, $2, 'web', 'customer feedback')
		RETURNING id`,
		tenantID, userID, subjectKey,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed feedback: %v", err)
	}
	return id
}

func (e env) softDeleteFeedback(t *testing.T, tenantID string, feedbackID int64) {
	t.Helper()
	tag, err := e.pool.Exec(
		e.ctx, `
		UPDATE user_feedback
		SET deleted_at = NOW()
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, feedbackID,
	)
	if err != nil {
		t.Fatalf("soft delete feedback %d: %v", feedbackID, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("soft delete feedback rows = %d, want 1", tag.RowsAffected())
	}
}

func (e env) begin(t *testing.T) pgx.Tx {
	t.Helper()
	tx, err := e.pool.Begin(e.ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	return tx
}

func linkFeedback(t *testing.T, ctx context.Context, repo *crrepo.Repo, tx pgx.Tx, tenantID string, requestID uuid.UUID, feedbackID int64) {
	t.Helper()
	if err := repo.LinkFeedbackTx(ctx, tx, crrepo.LinkFeedbackInput{
		TenantID:   tenantID,
		RequestID:  requestID,
		FeedbackID: feedbackID,
		Importance: crrepo.ImportanceNormal,
		ActorID:    "tester",
	}); err != nil {
		t.Fatalf("LinkFeedbackTx: %v", err)
	}
}

func linkCustomer(t *testing.T, ctx context.Context, repo *crrepo.Repo, tx pgx.Tx, tenantID string, requestID uuid.UUID, subjectKey, accountKey string) {
	t.Helper()
	if _, err := repo.LinkCustomerTx(ctx, tx, crrepo.CustomerLinkInput{
		TenantID:       tenantID,
		RequestID:      requestID,
		SubjectKey:     subjectKey,
		SubjectDisplay: subjectKey,
		AccountKey:     accountKey,
		AccountDisplay: accountKey,
		ActorID:        "tester",
	}); err != nil {
		t.Fatalf("LinkCustomerTx: %v", err)
	}
}

func addVote(t *testing.T, ctx context.Context, repo *crrepo.Repo, tx pgx.Tx, tenantID string, requestID uuid.UUID, subjectKey, accountKey string) {
	t.Helper()
	if _, err := repo.AddVoteTx(ctx, tx, crrepo.VoteInput{
		TenantID:       tenantID,
		RequestID:      requestID,
		SubjectKey:     subjectKey,
		SubjectDisplay: subjectKey,
		AccountKey:     accountKey,
		AccountDisplay: accountKey,
		Weight:         1,
		ActorID:        "tester",
	}); err != nil {
		t.Fatalf("AddVoteTx: %v", err)
	}
}

func addNote(t *testing.T, ctx context.Context, repo *crrepo.Repo, tx pgx.Tx, tenantID string, requestID uuid.UUID, body string) {
	t.Helper()
	if _, err := repo.AddNoteTx(ctx, tx, crrepo.NoteInput{
		TenantID:  tenantID,
		RequestID: requestID,
		Body:      body,
		ActorID:   "tester",
	}); err != nil {
		t.Fatalf("AddNoteTx: %v", err)
	}
}

func linkIssue(t *testing.T, ctx context.Context, repo *crrepo.Repo, tx pgx.Tx, tenantID string, requestID uuid.UUID, externalKey string) {
	t.Helper()
	if _, err := repo.LinkIssueTx(ctx, tx, crrepo.IssueLinkInput{
		TenantID:    tenantID,
		RequestID:   requestID,
		Provider:    "github",
		ExternalKey: externalKey,
		ExternalURL: "https://github.com/" + externalKey,
		Title:       externalKey,
		ActorID:     "tester",
	}); err != nil {
		t.Fatalf("LinkIssueTx: %v", err)
	}
}

func commit(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func rollback(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("rollback tx: %v", err)
	}
}
