// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestResponseListRequiresReviewForRecoveryFilters(t *testing.T) {
	t.Parallel()

	if responseListRequiresReview(ResponseFilter{}) {
		t.Fatal("empty response filter requires low-score review join")
	}
	if !responseListRequiresReview(ResponseFilter{LowScoreOnly: ptrext.Of(true)}) {
		t.Fatal("low-score filter does not require low-score review join")
	}
	if !responseListRequiresReview(ResponseFilter{RecoverySLAStatus: RecoverySLAOverdue}) {
		t.Fatal("SLA filter does not require low-score review join")
	}
	if !responseListRequiresReview(ResponseFilter{RecoveryBlockerReason: RecoveryBlockerOwner}) {
		t.Fatal("blocker filter does not require low-score review join")
	}
	if !responseListRequiresReview(ResponseFilter{ReviewSeverity: SeverityCritical}) {
		t.Fatal("severity filter does not require low-score review join")
	}
	if !responseListRequiresReview(ResponseFilter{OwnerMemberID: ptrext.Of(uuid.New())}) {
		t.Fatal("owner filter does not require low-score review join")
	}
}

func TestRecoverySLAWhere(t *testing.T) {
	t.Parallel()

	assertContainsAll(t, recoverySLAWhere(RecoverySLAOverdue), activeReviewWhere, "lsr.due_at < NOW()")
	assertContainsAll(t, recoverySLAWhere(RecoverySLADueSoon), "INTERVAL '24 hours'", "lsr.due_at >= NOW()")
	assertContainsAll(t, recoverySLAWhere(RecoverySLAOnTrack), activeReviewWhere, "lsr.due_at IS NULL")
	assertContainsAll(t, recoverySLAWhere(RecoverySLAClosed), terminalReviewWhere)
	if got := recoverySLAWhere("unexpected"); got != "" {
		t.Fatalf("recoverySLAWhere(unexpected) = %q, want empty", got)
	}
}

func TestRecoveryBlockerWhereMatchesPlaybookPrecedence(t *testing.T) {
	t.Parallel()

	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerOverdue), activeReviewWhere, "lsr.due_at < NOW()")
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerOwner), activeReviewWhere, notOverdueReviewWhere, "lsr.owner_member_id IS NULL")
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerDue), activeReviewWhere, ownerAssignedWhere, "lsr.due_at IS NULL")
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerContact), activeReviewWhere, ownerAssignedWhere, dueAssignedWhere, "NOT lsr.customer_contacted")
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerRootCause), customerContactedWhere, rootCauseMissingWhere)
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerAction), rootCauseCapturedWhere, actionTakenMissingWhere)
	assertContainsAll(t, recoveryBlockerWhere(RecoveryBlockerNone), terminalReviewWhere, actionTakenCapturedWhere)
	if got := recoveryBlockerWhere("unexpected"); got != "" {
		t.Fatalf("recoveryBlockerWhere(unexpected) = %q, want empty", got)
	}
}

func TestLowScoreOwnerLoadQueryTargetsActiveAssignedWork(t *testing.T) {
	t.Parallel()

	query := lowScoreOwnerLoadQuery(whereClause([]string{
		"lsr.tenant_id = $1",
		activeReviewWhere,
		"lsr.owner_member_id IS NOT NULL",
		"lsr.campaign_id = $2",
	}))

	assertContainsAll(
		t,
		query,
		"GROUP BY lsr.owner_member_id",
		"ORDER BY 8 DESC",
		"LIMIT $3",
		activeReviewWhere,
		"lsr.owner_member_id IS NOT NULL",
		"lsr.severity = 'critical'",
		"NOT lsr.customer_contacted",
	)
}

func TestClaimLowScoreReviewsForRecoveryAutomationQuery(t *testing.T) {
	t.Parallel()

	query := claimLowScoreReviewsForRecoveryAutomationQuery()

	assertContainsAll(
		t,
		query,
		"FOR UPDATE SKIP LOCKED",
		"claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes'",
		"automation=survey_recovery_worker",
		"due_at IS NULL",
		"due_at < NOW()",
		"severity = 'critical'",
		"owner_member_id IS NULL AND created_at <= NOW() - INTERVAL '24 hours'",
	)
}

func TestClaimPendingRecoveryNotificationsQuery(t *testing.T) {
	t.Parallel()

	query := claimPendingRecoveryNotificationsQuery()

	assertContainsAll(
		t,
		query,
		"survey_recovery_notifications",
		"status IN ('pending', 'failed')",
		"next_retry_at <= NOW()",
		"claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes'",
		"FOR UPDATE SKIP LOCKED",
	)
}

func TestClaimPendingEmailInvitationsRequiresDeliverySecret(t *testing.T) {
	t.Parallel()

	query := claimPendingEmailInvitationsQuery()

	assertContainsAll(
		t,
		query,
		"delivery_status IN ('pending', 'delayed')",
		"delivery_secret IS NOT NULL",
		"FOR UPDATE SKIP LOCKED",
	)
}

func TestProviderEventHelpers(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{
		ProviderEventAccepted,
		ProviderEventDelivered,
		ProviderEventBounced,
		ProviderEventComplained,
		ProviderEventRejected,
		ProviderEventTemporarilyDelayed,
		ProviderEventOpened,
	} {
		if !validProviderEventType(eventType) {
			t.Fatalf("%q should be a valid provider event type", eventType)
		}
	}
	if validProviderEventType("unexpected") {
		t.Fatal("unexpected provider event type was accepted")
	}
	if got := providerEventFailureKind(ProviderEventComplained); got != "provider_complaint" {
		t.Fatalf("complaint failure kind = %q", got)
	}
	if got := providerEventContactSuppressionReason(ProviderEventBounced); got != "survey_provider_bounce" {
		t.Fatalf("bounce suppression reason = %q", got)
	}
	for _, terminalStatus := range []string{DeliveryBounced, DeliveryComplained, DeliveryRejected, DeliveryNotApplicable} {
		for _, eventType := range []string{
			ProviderEventOpened,
			ProviderEventTemporarilyDelayed,
			ProviderEventBounced,
			ProviderEventComplained,
			ProviderEventRejected,
		} {
			if providerEventAppliesToInvitation(eventType, terminalStatus) {
				t.Fatalf("%s event mutated terminal delivery status %q", eventType, terminalStatus)
			}
		}
	}
	if !providerEventAppliesToInvitation(ProviderEventOpened, DeliveryDelivered) {
		t.Fatal("opened event did not apply to delivered invitation")
	}
	if !providerEventAppliesToInvitation(ProviderEventTemporarilyDelayed, DeliveryAccepted) {
		t.Fatal("delayed event did not apply to accepted invitation")
	}
}

func TestResponseListIncludesRecoveryNotificationSummary(t *testing.T) {
	t.Parallel()

	query := responseListColumns(true) + "\n" + responseInvitationJoin + "\n" + responseFeedbackLinkJoin + "\n" + lowScoreReviewJoin(true)

	assertContainsAll(
		t,
		query,
		"COALESCE(srn.status, '')",
		"survey_recovery_notifications",
		"LEFT JOIN LATERAL",
		"survey_response_feedback_links",
		"srfl.feedback_id",
		"ORDER BY srn.created_at DESC, srn.id DESC",
		"si.recipient_snapshot",
		"response_metadata",
		"recipient_snapshot",
	)
}

func TestSurveyResponseAccountSQLRecognizesCommonShapes(t *testing.T) {
	t.Parallel()

	keySQL := surveyResponseAccountKeySQL()
	displaySQL := surveyResponseAccountDisplaySQL()

	assertContainsAll(
		t,
		keySQL,
		"sr.metadata->>'account_key'",
		"sr.metadata->>'companyId'",
		"sr.metadata #>> '{account,key}'",
		"si.recipient_snapshot->>'accountKey'",
		"si.recipient_snapshot #>> '{company,id}'",
	)
	assertContainsAll(
		t,
		displaySQL,
		"sr.metadata->>'account_display'",
		"sr.metadata->>'companyName'",
		"si.recipient_snapshot #>> '{account,name}'",
		"si.recipient_snapshot #>> '{company,display}'",
	)
}

func assertContainsAll(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Fatalf("%q does not contain %q", haystack, needle)
		}
	}
}
