//go:build integration

// SPDX-License-Identifier: Apache-2.0

package customerrequest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
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
	assertLen(t, "issues", len(detail.IssueLinks), 2)
	if detail.Summary.DuplicateRequestCount != 1 {
		t.Fatalf("DuplicateRequestCount = %d, want 1", detail.Summary.DuplicateRequestCount)
	}
	assertLen(t, "duplicates", len(detail.Duplicates), 1)
	if detail.Duplicates[0].ID != sourceID {
		t.Fatalf("duplicate id = %s, want source %s", detail.Duplicates[0].ID, sourceID)
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
