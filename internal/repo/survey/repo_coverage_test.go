// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestSurveyRepoHelpersCoverBoundsAndNullableValues(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	if boundedLimit(0) != 50 || boundedLimit(500) != maxListLimit || boundedLimit(9) != 9 {
		t.Fatal("boundedLimit did not clamp default, max, and explicit values")
	}
	if DestinationHash("  ada@example.test  ") != DestinationHash("ada@example.test") {
		t.Fatal("DestinationHash did not trim canonical destination text")
	}
	if nullableUUID(nil) != nil || nullableUUID(ptrext.Of(id)) != id {
		t.Fatal("nullableUUID did not preserve nil and present ids")
	}
	if uuidFromPg(pgtype.UUID{}) != nil || ptrext.Indirect(uuidFromPg(pgtype.UUID{Bytes: id, Valid: true})) != id {
		t.Fatal("uuidFromPg did not preserve nil and valid ids")
	}
	if nextRetryAtArg(time.Time{}) != nil || nextRetryAtArg(now) != now {
		t.Fatal("nextRetryAtArg did not preserve zero and present times")
	}
	if nullableBytes(nil) != nil || string(nullableBytes([]byte("secret")).([]byte)) != "secret" {
		t.Fatal("nullableBytes did not preserve nil and present payloads")
	}
}

func TestSurveyRepoHelpersCoverJSONAndErrorMapping(t *testing.T) {
	t.Parallel()

	raw, err := jsonObject(map[string]any{"tier": "enterprise"})
	if err != nil || !strings.Contains(string(raw), "enterprise") {
		t.Fatalf("jsonObject() = %q, %v", string(raw), err)
	}
	got, err := decodeObject(raw)
	if err != nil || got["tier"] != "enterprise" {
		t.Fatalf("decodeObject() = %#v, %v", got, err)
	}
	if _, err := jsonObject(map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("jsonObject() accepted an unsupported value")
	}
	if _, err := decodeObject([]byte("{")); err == nil {
		t.Fatal("decodeObject() accepted invalid JSON")
	}
	if !errors.Is(mapNotFound(pgx.ErrNoRows), ErrNotFound) || !errors.Is(mapNotFound(context.Canceled), context.Canceled) {
		t.Fatal("mapNotFound did not preserve not-found and other errors")
	}
	if !errors.Is(mapWriteError(ptrext.Of(pgconn.PgError{Code: "23505"})), ErrConflict) {
		t.Fatal("mapWriteError did not map unique violations")
	}
	if !errors.Is(mapWriteError(ptrext.Of(pgconn.PgError{Code: "23503"})), ErrInvalidInput) {
		t.Fatal("mapWriteError did not map foreign-key violations")
	}
}

func TestSurveyRepoScanHelpersDecodeRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	requestID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	contactID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	ownerID := uuid.MustParse("66666666-6666-6666-6666-666666666666")

	campaign, err := scanCampaign(fakeSurveyRow{values: campaignRowValues(campaignID, now)})
	if err != nil || campaign.Content["question"] != "How was support?" {
		t.Fatalf("scanCampaign() = %#v, %v", campaign, err)
	}
	invitation, err := scanInvitation(fakeSurveyRow{values: invitationRowValues(invitationID, campaignID, ptrext.Of(requestID), ptrext.Of(contactID), now)})
	if err != nil || invitation.RequestID == nil || invitation.ContactID == nil {
		t.Fatalf("scanInvitation() = %#v, %v", invitation, err)
	}
	response, err := scanResponseWithLowScoreReviewAndAccount(fakeSurveyRow{
		values: responseReviewAccountRowValues(responseID, campaignID, invitationID, ptrext.Of(requestID), ptrext.Of(contactID), ptrext.Of(ownerID), now),
	})
	if err != nil || response.Review == nil || response.Account.AccountKey != "acct:acme" {
		t.Fatalf("scanResponseWithLowScoreReviewAndAccount() = %#v, %v", response, err)
	}
}

func TestSurveyRepoCampaignAndInvitationPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	campaignValues := campaignRowValues(campaignID, now)
	invitationValues := invitationRowValues(invitationID, campaignID, nil, nil, now)
	pool := ptrext.Of(fakeSurveyPool{
		rows: []fakeSurveyRow{
			{values: campaignValues},
			{values: campaignValues},
			{values: campaignValues},
			{values: campaignValues},
			{values: invitationValues},
			{values: []any{true}},
			{values: invitationValues},
			{values: invitationValues},
			{values: invitationValues},
		},
		queries: []*fakeSurveyRows{
			{rows: [][]any{campaignValues}},
			{rows: [][]any{invitationValues}},
		},
		execs: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 3")},
	})
	repo := Repo{pool: pool}

	if campaigns, err := repo.ListCampaigns(ctx, CampaignFilter{TenantID: " tenant-a ", Status: StatusActive}); err != nil || len(campaigns) != 1 {
		t.Fatalf("ListCampaigns() len=%d err=%v", len(campaigns), err)
	}
	if _, err := repo.GetCampaign(ctx, "tenant-a", campaignID); err != nil {
		t.Fatalf("GetCampaign() error = %v", err)
	}
	if _, err := repo.CreateCampaign(ctx, campaignFixture(campaignID, now)); err != nil {
		t.Fatalf("CreateCampaign() error = %v", err)
	}
	if _, err := repo.UpdateCampaign(ctx, campaignFixture(campaignID, now)); err != nil {
		t.Fatalf("UpdateCampaign() error = %v", err)
	}
	if _, err := repo.ArchiveCampaign(ctx, "tenant-a", campaignID, "admin-1", now); err != nil {
		t.Fatalf("ArchiveCampaign() error = %v", err)
	}
	requireInvitationPaths(ctx, t, ptrext.Of(repo), campaignID, invitationID, now)
	if pool.execIdx != 1 {
		t.Fatalf("expected stale expiration exec, got %d", pool.execIdx)
	}
}

func TestSurveyRepoResponseAndAnalyticsPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ownerID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	pool := surveyAnalyticsPool(responseID, campaignID, invitationID, ownerID, now)
	repo := Repo{pool: pool}

	response, err := repo.ListResponses(ctx, ResponseFilter{TenantID: "tenant-a", LowScoreOnly: ptrext.Of(true), Limit: 2})
	if err != nil || len(response) != 1 || response[0].Review == nil {
		t.Fatalf("ListResponses() = %#v, %v", response, err)
	}
	analytics, err := repo.Analytics(ctx, AnalyticsFilter{TenantID: "tenant-a", CampaignID: ptrext.Of(campaignID), From: ptrext.Of(now.Add(-time.Hour)), To: ptrext.Of(now)})
	if err != nil || analytics.ResponseRate != 0.4 || analytics.PositiveScoreRate != 0.5 {
		t.Fatalf("Analytics() = %#v, %v", analytics, err)
	}
	trend, err := repo.AnalyticsTrend(ctx, AnalyticsFilter{TenantID: "tenant-a", From: ptrext.Of(now.Add(-24 * time.Hour)), To: ptrext.Of(now)})
	if err != nil || len(trend) != 1 || trend[0].Date != "2026-08-02" {
		t.Fatalf("AnalyticsTrend() = %#v, %v", trend, err)
	}
	segments, err := repo.AnalyticsSegments(ctx, AnalyticsSegmentFilter{TenantID: "tenant-a", Dimension: SegmentCampaign, From: ptrext.Of(now.Add(-24 * time.Hour)), To: ptrext.Of(now), Limit: 5})
	if err != nil || len(segments) != 1 || segments[0].CampaignID == nil {
		t.Fatalf("AnalyticsSegments() = %#v, %v", segments, err)
	}
}

func TestSurveyRepoCreateAndUpdateResponsePaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	responseValues := responseRowValues(responseID, campaignID, invitationID, nil, nil, now)
	reviewValues := reviewRowValues(responseID, campaignID, nil, now)
	tx := ptrext.Of(fakeSurveyTx{rows: []fakeSurveyRow{{values: responseValues}, {values: reviewValues}}})
	repo := Repo{pool: ptrext.Of(fakeSurveyPool{tx: tx, rows: []fakeSurveyRow{
		{values: responseValues}, {err: pgx.ErrNoRows}, {values: reviewValues},
	}})}

	created, err := repo.CreateResponse(ctx, responseFixture(responseID, campaignID, invitationID, now), ptrext.Of(LowScoreReviewSeed{DueAt: ptrext.Of(now.Add(24 * time.Hour)), UpdatedBy: "admin-1"}))
	if err != nil || created.Review == nil || tx.commitCount != 1 {
		t.Fatalf("CreateResponse() = %#v, commit=%d err=%v", created, tx.commitCount, err)
	}
	if got, err := repo.GetResponseByInvitation(ctx, "tenant-a", invitationID); err != nil || got.ID != responseID {
		t.Fatalf("GetResponseByInvitation() = %#v, %v", got, err)
	}
	if got, err := repo.UpdateLowScoreReview(ctx, reviewFixture(responseID, campaignID, nil, now)); err != nil || got.ResponseID != responseID {
		t.Fatalf("UpdateLowScoreReview() = %#v, %v", got, err)
	}
}

func TestSurveyRepoAutomationLookupPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	requestID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	contactID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	senderID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	pool := ptrext.Of(fakeSurveyPool{
		rows: []fakeSurveyRow{
			{values: triggerContextRowValues(requestID, contactID, now)},
			{values: requestRecipientRowValues(contactID, now)},
			{values: []any{3}},
			{values: []any{2}},
			{values: emailSenderRowValues(senderID)},
			{values: emailSenderRowValues(senderID)},
		},
		queries: []*fakeSurveyRows{
			{rows: [][]any{campaignRowValues(campaignID, now)}},
			{rows: [][]any{requestRecipientRowValues(contactID, now)}},
		},
	})
	repo := Repo{pool: pool}

	campaigns, err := repo.ListActiveCampaignsByTrigger(ctx, " tenant-a ", TriggerRequestResolved)
	requireSurveyCondition(t, "ListActiveCampaignsByTrigger", err, len(campaigns) == 1, campaigns)
	trigger, err := repo.FeedbackTriggerContext(ctx, " tenant-a ", 42)
	requireSurveyCondition(t, "FeedbackTriggerContext", err, trigger.RequestID != nil && trigger.ContactID != nil, trigger)
	recipients, err := repo.RequestRecipients(ctx, "tenant-a", requestID)
	requireSurveyCondition(t, "RequestRecipients", err, len(recipients) == 1, recipients)
	contact, err := repo.EmailContact(ctx, "tenant-a", contactID)
	requireSurveyCondition(t, "EmailContact", err, contact.ContactID == contactID, contact)
	count, err := repo.CountCampaignInvitationsSince(ctx, "tenant-a", campaignID, now.Add(-time.Hour))
	requireSurveyCondition(t, "CountCampaignInvitationsSince", err, count == 3, count)
	count, err = repo.CountContactInvitationsSince(ctx, "tenant-a", contactID, now.Add(-time.Hour))
	requireSurveyCondition(t, "CountContactInvitationsSince", err, count == 2, count)
	sender, err := repo.ActiveEmailSender(ctx, "tenant-a")
	requireSurveyCondition(t, "ActiveEmailSender", err, sender.ID == senderID, sender)
	sender, err = repo.EmailSender(ctx, "tenant-a", senderID)
	requireSurveyCondition(t, "EmailSender", err, sender.ID == senderID, sender)
}

func TestSurveyRepoInvitationDeliveryMutationPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 15, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	invitationValues := invitationRowValues(invitationID, campaignID, nil, nil, now)
	repo := Repo{pool: ptrext.Of(fakeSurveyPool{
		rows: []fakeSurveyRow{{values: invitationValues}, {values: invitationValues}, {values: invitationValues}, {values: invitationValues}},
		queries: []*fakeSurveyRows{
			{rows: [][]any{invitationValues}},
		},
	})}

	if claimed, err := repo.ClaimPendingEmailInvitations(ctx, 1, "worker-1"); err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimPendingEmailInvitations() = %#v, %v", claimed, err)
	}
	if got, err := repo.MarkInvitationDelivered(ctx, "tenant-a", invitationID, "worker-1", "resend", "msg-1", 202); err != nil || got.ID != invitationID {
		t.Fatalf("MarkInvitationDelivered() = %#v, %v", got, err)
	}
	if got, err := repo.MarkInvitationFailed(ctx, "tenant-a", invitationID, "worker-1", "rate limited", "rate_limited", 429, time.Minute, false); err != nil || got.ID != invitationID {
		t.Fatalf("MarkInvitationFailed() = %#v, %v", got, err)
	}
	if got, err := repo.RetryInvitationDelivery(ctx, "tenant-a", invitationID); err != nil || got.ID != invitationID {
		t.Fatalf("RetryInvitationDelivery() = %#v, %v", got, err)
	}
	if got, err := repo.SuppressInvitation(ctx, "tenant-a", invitationID, "contact opted out"); err != nil || got.ID != invitationID {
		t.Fatalf("SuppressInvitation() = %#v, %v", got, err)
	}
}

func TestSurveyRepoProviderEventPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 30, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	invitationID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	contactID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	tx := ptrext.Of(fakeSurveyTx{rows: []fakeSurveyRow{
		{values: invitationRowValues(invitationID, campaignID, nil, ptrext.Of(contactID), now)},
		{values: invitationRowValues(invitationID, campaignID, nil, ptrext.Of(contactID), now)},
	}})
	repo := Repo{pool: ptrext.Of(fakeSurveyPool{tx: tx})}

	got, err := repo.RecordProviderEvent(ctx, ProviderEventInput{
		TenantID: " tenant-a ", InvitationID: ptrext.Of(invitationID), Provider: " resend ",
		ProviderEventType: ProviderEventBounced, ProviderMessageID: "msg-1",
		ProviderEventKey: "event-1", Payload: map[string]any{"reason": "mailbox_full"}, OccurredAt: now,
	})
	if err != nil || got.ID != invitationID || tx.commitCount != 1 {
		t.Fatalf("RecordProviderEvent() = %#v, commit=%d err=%v", got, tx.commitCount, err)
	}
}

func TestSurveyRepoRecoveryNotificationPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 11, 45, 0, 0, time.UTC)
	campaignID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	responseID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	requestID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	ownerID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	notificationID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	repo := Repo{pool: ptrext.Of(fakeSurveyPool{
		rows: []fakeSurveyRow{
			{values: recoveryContextRowValues(responseID, campaignID, requestID, ownerID, now)},
			{values: recoveryOwnerRowValues(ownerID)},
			{values: recoveryNotificationRowValues(notificationID, responseID, ownerID, RecoveryNotificationPending, now)},
			{values: recoveryNotificationRowValues(notificationID, responseID, ownerID, RecoveryNotificationDelivered, now)},
			{values: recoveryNotificationRowValues(notificationID, responseID, ownerID, RecoveryNotificationFailed, now)},
			{values: recoveryNotificationRowValues(notificationID, responseID, ownerID, RecoveryNotificationSuppressed, now)},
		},
		queries: []*fakeSurveyRows{
			{rows: [][]any{recoveryNotificationRowValues(notificationID, responseID, ownerID, RecoveryNotificationPending, now)}},
		},
	})}

	item, err := repo.RecoveryNotificationContext(ctx, "tenant-a", responseID)
	requireSurveyCondition(t, "RecoveryNotificationContext", err, item.Owner.ID == ownerID, item)
	owner, err := repo.GetRecoveryOwner(ctx, "tenant-a", ownerID)
	requireSurveyCondition(t, "GetRecoveryOwner", err, owner.ID == ownerID, owner)
	notification, created, err := repo.EnsureRecoveryNotification(ctx, RecoveryNotificationInput{TenantID: "tenant-a", ResponseID: responseID, OwnerMemberID: ownerID, Reason: "owner_missing", DestinationHash: "hash-1", Payload: map[string]any{"score": 2}})
	requireSurveyCondition(t, "EnsureRecoveryNotification", err, created && notification.ID == notificationID, notification)
	claimed, err := repo.ClaimPendingRecoveryNotifications(ctx, 1, "worker-1")
	requireSurveyCondition(t, "ClaimPendingRecoveryNotifications", err, len(claimed) == 1, claimed)
	got, err := repo.MarkRecoveryNotificationDelivered(ctx, "tenant-a", notificationID, "worker-1", "resend", "msg-1", 202)
	requireSurveyCondition(t, "MarkRecoveryNotificationDelivered", err, got.Status == RecoveryNotificationDelivered, got)
	got, err = repo.MarkRecoveryNotificationFailed(ctx, "tenant-a", notificationID, "worker-1", "rate limited", "rate_limited", 429, time.Minute, false)
	requireSurveyCondition(t, "MarkRecoveryNotificationFailed", err, got.Status == RecoveryNotificationFailed, got)
	got, err = repo.MarkRecoveryNotificationSuppressed(ctx, "tenant-a", notificationID, "worker-1", "owner has no email")
	requireSurveyCondition(t, "MarkRecoveryNotificationSuppressed", err, got.Status == RecoveryNotificationSuppressed, got)
}

func requireInvitationPaths(ctx context.Context, t *testing.T, repo *Repo, campaignID uuid.UUID, invitationID uuid.UUID, now time.Time) {
	t.Helper()
	if invitations, err := repo.ListInvitations(ctx, InvitationFilter{TenantID: "tenant-a", CampaignID: ptrext.Of(campaignID), Limit: 1}); err != nil || len(invitations) != 1 {
		t.Fatalf("ListInvitations() len=%d err=%v", len(invitations), err)
	}
	if _, err := repo.CreateInvitation(ctx, invitationFixture(invitationID, campaignID, now)); err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	if exists, err := repo.InvitationExistsByDedupeKey(ctx, "tenant-a", campaignID, "dedupe-1"); err != nil || !exists {
		t.Fatalf("InvitationExistsByDedupeKey() = %t, %v", exists, err)
	}
	if _, err := repo.GetInvitation(ctx, "tenant-a", invitationID); err != nil {
		t.Fatalf("GetInvitation() error = %v", err)
	}
	if _, err := repo.GetInvitationByTokenHash(ctx, "hash-1"); err != nil {
		t.Fatalf("GetInvitationByTokenHash() error = %v", err)
	}
	if _, err := repo.ExpireInvitation(ctx, "tenant-a", invitationID, "expired"); err != nil {
		t.Fatalf("ExpireInvitation() error = %v", err)
	}
	if count, err := repo.ExpireStaleInvitations(ctx, 3, now, "expired"); err != nil || count != 3 {
		t.Fatalf("ExpireStaleInvitations() = %d, %v", count, err)
	}
}

func requireSurveyCondition(t *testing.T, name string, err error, ok bool, got any) {
	t.Helper()
	if err != nil || !ok {
		t.Fatalf("%s() = %#v, %v", name, got, err)
	}
}

func surveyAnalyticsPool(responseID uuid.UUID, campaignID uuid.UUID, invitationID uuid.UUID, ownerID uuid.UUID, now time.Time) *fakeSurveyPool {
	return ptrext.Of(fakeSurveyPool{
		rows: []fakeSurveyRow{
			{values: []any{10, 8, 1, 2, 3, 1, 1, 1, 1}},
			{values: []any{4, 2, 2, sql.NullFloat64{Float64: 3.5, Valid: true}, sql.NullFloat64{Float64: 120, Valid: true}}},
			{values: []any{3, 2, 1, 1, 1, ptrext.Of(now), 1, 1, 1, 1, 1}},
		},
		queries: []*fakeSurveyRows{
			{rows: [][]any{responseReviewAccountRowValues(responseID, campaignID, invitationID, nil, nil, ptrext.Of(ownerID), now)}},
			{rows: [][]any{{1, 2}, {5, 2}}},
			{rows: [][]any{{"contact_cooldown", 1}}},
			{rows: [][]any{{ownerID.String(), 2, 1, 1, 1, 1, ptrext.Of(now), 76}}},
			{rows: [][]any{{"2026-08-02", 10, 8, 1, 4, 2, 2, 3.5, 0.4, 2, 3, 1}}},
			{rows: [][]any{{"campaign-1", "Post-resolution CSAT", sql.NullString{String: campaignID.String(), Valid: true}, 10, 8, 1, 4, 2, 2, 1, 3.5, 0.4, 0.5, 0.5, 0.1, 120.0, 9.0}}},
		},
	})
}

func campaignFixture(id uuid.UUID, now time.Time) Campaign {
	return Campaign{
		ID: id, TenantID: " tenant-a ", Name: "Post-resolution CSAT", SurveyType: TypeCSAT,
		Status: StatusActive, TriggerEvent: TriggerRequestResolved, DistributionMode: DistributionContactEmail,
		DedupePolicy: DedupeOnePerResolution, TriggerFilter: map[string]any{"status": "closed"},
		Content: map[string]any{"question": "How was support?"}, Locale: "en", ContentVersion: 2,
		SamplingPercent: 100, MinDaysBetweenContact: 30, ExpiresAfterDays: 14, MaxDailyInvitations: 100,
		LowScoreThreshold: 3, RequireRecentCustomerActivity: true, RecentActivityDays: 90,
		SuppressAutoResolved: true, CreatedBy: "admin-1", UpdatedBy: "admin-1", CreatedAt: now, UpdatedAt: now,
	}
}

func invitationFixture(id uuid.UUID, campaignID uuid.UUID, now time.Time) Invitation {
	return Invitation{
		ID: id, TenantID: " tenant-a ", CampaignID: campaignID, CampaignContentVersion: 2,
		CampaignSnapshot: map[string]any{"name": "CSAT"}, DedupeKey: "dedupe-1", SourceType: "request",
		SourceID: "CR-1", DistributionMode: DistributionContactEmail, TokenHash: "hash-1",
		DeliveryStatus: DeliveryPending, ResponseStatus: ResponseNotStarted, SuppressionStatus: SuppressionNotSuppressed,
		RecipientSnapshot: map[string]any{"email": "ada@example.test"}, DeliverySecret: []byte("secret"),
		Provider: "postmark", ProviderMessageID: "msg-1", NextRetryAt: now, ExpiresAt: ptrext.Of(now.Add(24 * time.Hour)),
		CreatedBy: "admin-1", CreatedAt: now, UpdatedAt: now,
	}
}

func responseFixture(id uuid.UUID, campaignID uuid.UUID, invitationID uuid.UUID, now time.Time) Response {
	return Response{
		ID: id, TenantID: " tenant-a ", CampaignID: campaignID, InvitationID: invitationID, SourceType: "request",
		SourceID: "CR-1", Score: 2, Comment: "Still painful", Locale: "en", Metadata: map[string]any{"account_key": "acct:acme"},
		UserAgentHash: "ua", IPHash: "ip", SubmittedAt: now, CreatedAt: now,
	}
}

func reviewFixture(responseID uuid.UUID, campaignID uuid.UUID, ownerID *uuid.UUID, now time.Time) LowScoreReview {
	return LowScoreReview{
		ResponseID: responseID, TenantID: "tenant-a", CampaignID: campaignID, Status: ReviewOpen, Severity: SeverityHigh,
		OwnerMemberID: ownerID, CustomerContacted: true, DueAt: ptrext.Of(now.Add(24 * time.Hour)),
		UpdatedBy: "admin-1", CreatedAt: now, UpdatedAt: now,
	}
}

func campaignRowValues(id uuid.UUID, now time.Time) []any {
	c := campaignFixture(id, now)
	return []any{
		c.ID, c.TenantID, c.Name, c.SurveyType, c.Status, c.TriggerEvent, c.DistributionMode,
		c.DedupePolicy, []byte(`{"status":"closed"}`), []byte(`{"question":"How was support?"}`), c.Locale,
		c.ContentVersion, c.SamplingPercent, c.MinDaysBetweenContact, c.ExpiresAfterDays, c.MaxDailyInvitations,
		c.LowScoreThreshold, c.RequireRecentCustomerActivity, c.RecentActivityDays, c.SuppressAutoResolved,
		c.CreatedBy, c.UpdatedBy, c.ArchivedAt, c.CreatedAt, c.UpdatedAt,
	}
}

func invitationRowValues(id uuid.UUID, campaignID uuid.UUID, requestID *uuid.UUID, contactID *uuid.UUID, now time.Time) []any {
	i := invitationFixture(id, campaignID, now)
	return []any{
		i.ID, i.TenantID, i.CampaignID, i.CampaignContentVersion, []byte(`{"name":"CSAT"}`), i.DedupeKey,
		i.SourceType, i.SourceID, pgUUID(requestID), pgUUID(contactID), i.DistributionMode, i.TokenHash,
		i.DeliveryStatus, i.ResponseStatus, i.SuppressionStatus, i.SuppressionReason, []byte(`{"email":"ada@example.test"}`),
		i.DeliverySecret, i.Provider, i.ProviderMessageID, i.Attempts, i.FailureKind, i.HTTPStatus, i.LastError,
		i.ClaimedAt, i.ClaimedBy, i.NextRetryAt, i.DeliveredAt, i.OpenedAt, i.RespondedAt, i.ExpiresAt,
		i.CreatedBy, i.CreatedAt, i.UpdatedAt,
	}
}

func responseRowValues(id uuid.UUID, campaignID uuid.UUID, invitationID uuid.UUID, requestID *uuid.UUID, contactID *uuid.UUID, now time.Time) []any {
	return []any{
		id, "tenant-a", campaignID, invitationID, pgUUID(requestID), pgUUID(contactID), "request", "CR-1", 2,
		"Still painful", "en", []byte(`{"account_key":"acct:acme"}`), "ua", "ip", now, now,
	}
}

func responseReviewAccountRowValues(id uuid.UUID, campaignID uuid.UUID, invitationID uuid.UUID, requestID *uuid.UUID, contactID *uuid.UUID, ownerID *uuid.UUID, now time.Time) []any {
	values := responseRowValues(id, campaignID, invitationID, requestID, contactID, now)
	values = append(values, reviewRowValues(id, campaignID, ownerID, now)...)
	values = append(values, RecoveryNotificationPending, "owner_missing", ptrext.Of(now), "")
	return append(values, "acct:acme", "Acme Corp", "response_metadata")
}

func reviewRowValues(responseID uuid.UUID, campaignID uuid.UUID, ownerID *uuid.UUID, now time.Time) []any {
	return []any{
		responseID, "tenant-a", campaignID, ReviewOpen, SeverityHigh, pgUUID(ownerID), "billing", "credited",
		true, ptrext.Of(now.Add(24 * time.Hour)), ptrext.Of(now), "admin-1", now, now,
	}
}

func triggerContextRowValues(requestID uuid.UUID, contactID uuid.UUID, now time.Time) []any {
	return []any{
		"tenant-a", int64(42), "intercom", "subject-1", "hash-1", "Ada", now,
		pgUUID(ptrext.Of(requestID)), "Login fails", "resolved",
		pgUUID(ptrext.Of(contactID)), "Ada Lovelace", []byte("ada@example.test"),
	}
}

func requestRecipientRowValues(contactID uuid.UUID, now time.Time) []any {
	return []any{
		contactID, "Ada Lovelace", "Acme", []byte("ada@example.test"),
		"opted_in", "subject-1", "hash-1", "Ada", now,
	}
}

func emailSenderRowValues(id uuid.UUID) []any {
	return []any{
		id, "tenant-a", "Attune", []byte("hello@attune.test"),
		[]byte("reply@attune.test"), "resend", []byte(`{"region":"us"}`),
	}
}

func recoveryContextRowValues(responseID uuid.UUID, campaignID uuid.UUID, requestID uuid.UUID, ownerID uuid.UUID, now time.Time) []any {
	return []any{
		"tenant-a", responseID, campaignID, "Post-resolution CSAT", TypeCSAT,
		pgUUID(ptrext.Of(requestID)), "request", "CR-1", 2, "Still painful", now,
		ownerID, "tenant-a", "owner@example.test", "owner@example.test",
		ReviewOpen, SeverityHigh, ptrext.Of(now.Add(24 * time.Hour)),
	}
}

func recoveryOwnerRowValues(ownerID uuid.UUID) []any {
	return []any{ownerID, "tenant-a", "owner@example.test", "owner@example.test"}
}

func recoveryNotificationRowValues(id uuid.UUID, responseID uuid.UUID, ownerID uuid.UUID, status string, now time.Time) []any {
	return []any{
		id, "tenant-a", responseID, pgUUID(ptrext.Of(ownerID)), RecoveryNotificationEmail, status,
		"owner_missing", "hash-1", []byte(`{"score":2}`), "resend", "msg-1", 1,
		"",
		sql.NullInt32{Int32: 202, Valid: true},
		"", ptrext.Of(now), "worker-1",
		now, ptrext.Of(now), now, now,
	}
}

func pgUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: ptrext.Indirect(id), Valid: true}
}

type fakeSurveyPool struct {
	tx       pgx.Tx
	rows     []fakeSurveyRow
	rowIdx   int
	queries  []*fakeSurveyRows
	queryIdx int
	execs    []pgconn.CommandTag
	execIdx  int
}

func (p *fakeSurveyPool) Begin(context.Context) (pgx.Tx, error) {
	if p.tx != nil {
		return p.tx, nil
	}
	return ptrext.Of(fakeSurveyTx{}), nil
}

func (p *fakeSurveyPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	idx := p.execIdx
	p.execIdx++
	if idx < len(p.execs) {
		return p.execs[idx], nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (p *fakeSurveyPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if p.queryIdx >= len(p.queries) {
		p.queryIdx++
		return ptrext.Of(fakeSurveyRows{}), nil
	}
	rows := p.queries[p.queryIdx]
	p.queryIdx++
	return rows, nil
}

func (p *fakeSurveyPool) QueryRow(context.Context, string, ...any) pgx.Row {
	if p.rowIdx >= len(p.rows) {
		return fakeSurveyRow{err: errors.New("unexpected query row")}
	}
	row := p.rows[p.rowIdx]
	p.rowIdx++
	return row
}

type fakeSurveyTx struct {
	rows        []fakeSurveyRow
	rowIdx      int
	commitCount int
}

func (tx *fakeSurveyTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeSurveyTx) Commit(context.Context) error          { tx.commitCount++; return nil }
func (tx *fakeSurveyTx) Rollback(context.Context) error        { return nil }
func (tx *fakeSurveyTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *fakeSurveyTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeSurveyTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (tx *fakeSurveyTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeSurveyTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeSurveyTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return ptrext.Of(fakeSurveyRows{}), nil
}

func (tx *fakeSurveyTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeSurveyRow{err: errors.New("unexpected tx query row")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}
func (tx *fakeSurveyTx) Conn() *pgx.Conn { return nil }

type fakeSurveyRow struct {
	values []any
	err    error
}

func (r fakeSurveyRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	return assignSurveyScanValues(dest, r.values)
}

type fakeSurveyRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeSurveyRows) Close()                                       {}
func (r *fakeSurveyRows) Err() error                                   { return r.err }
func (r *fakeSurveyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeSurveyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeSurveyRows) RawValues() [][]byte                          { return nil }
func (r *fakeSurveyRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeSurveyRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeSurveyRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeSurveyRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	if len(dest) != len(r.rows[r.idx-1]) {
		return errors.New("scan destination count mismatch")
	}
	return assignSurveyScanValues(dest, r.rows[r.idx-1])
}

func assignSurveyScanValues(dest []any, values []any) error {
	for i := range dest {
		if err := assignSurveyScanValue(dest[i], values[i]); err != nil {
			return err
		}
	}
	return nil
}

func assignSurveyScanValue(dest any, src any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := destValue.Elem()
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	source := reflect.ValueOf(src)
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return nil
	}
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return nil
	}
	return errors.New("scan source type mismatch")
}
