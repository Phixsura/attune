package feedback

import (
	"testing"

	"github.com/stretchr/testify/require"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

func TestBuildFeedbackIdentityEvidence_NormalizesSourceMeta(t *testing.T) {
	t.Parallel()

	evidence := buildFeedbackIdentityEvidence(" user-42 ", []byte(`{
		"email": "Ada@example.com",
		"external_id": "ext-42",
		"customer": {
			"email": "ada@example.com",
			"sourceContactId": "contact-77",
			"crmId": "crm-9"
		},
		"support": {
			"zendesk_user_id": 12345
		}
	}`))

	require.Equal(t, "user-42", evidence.GetSourceUser())
	require.True(t, evidence.GetHasEmail())
	require.True(t, evidence.GetHasExternalId())
	require.True(t, evidence.GetHasSourceContactId())
	require.True(t, evidence.GetHasCrmId())
	require.True(t, evidence.GetHasSupportId())
	require.Equal(t, int32(6), evidence.GetMergeCandidateCount())
	assessment := evidence.GetAssessment()
	require.Equal(t, attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG, assessment.GetStrength())
	require.Equal(t, attunev1.FeedbackIdentityRecommendedAction_FEEDBACK_IDENTITY_RECOMMENDED_ACTION_REVIEW_MERGE, assessment.GetRecommendedAction())
	require.Equal(t, int32(5), assessment.GetStableKeyCount())
	require.Empty(t, assessment.GetMissingKinds())

	actual := make([][3]string, 0, len(evidence.GetKeys()))
	for _, key := range evidence.GetKeys() {
		actual = append(actual, [3]string{key.GetKind(), key.GetValue(), key.GetSource()})
	}
	require.Equal(t, [][3]string{
		{"source_user", "user-42", "user_id"},
		{"email", "Ada@example.com", "source_meta.email"},
		{"external_id", "ext-42", "source_meta.external_id"},
		{"source_contact_id", "contact-77", "source_meta.customer.sourceContactId"},
		{"crm_id", "crm-9", "source_meta.customer.crmId"},
		{"support_id", "12345", "source_meta.support.zendesk_user_id"},
	}, actual)
}

func TestBuildFeedbackIdentityEvidence_IgnoresBadMetaAndLongValues(t *testing.T) {
	t.Parallel()

	evidence := buildFeedbackIdentityEvidence("user-7", []byte(`{"email":`))
	require.Equal(t, "user-7", evidence.GetSourceUser())
	require.Equal(t, int32(1), evidence.GetMergeCandidateCount())
	require.False(t, evidence.GetHasEmail())
	require.Equal(t, attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_WEAK, evidence.GetAssessment().GetStrength())
	require.Equal(t, attunev1.FeedbackIdentityRecommendedAction_FEEDBACK_IDENTITY_RECOMMENDED_ACTION_CAPTURE_MORE_KEYS, evidence.GetAssessment().GetRecommendedAction())
	require.Contains(t, evidence.GetAssessment().GetRiskReasons(), "only_source_user")

	longValue := make([]byte, maxIdentityEvidenceValueLength+1)
	for idx := range longValue {
		longValue[idx] = 'a'
	}
	evidence = buildFeedbackIdentityEvidence("", []byte(`{"email":"`+string(longValue)+`"}`))
	require.Empty(t, evidence.GetKeys())
	require.Equal(t, int32(0), evidence.GetMergeCandidateCount())
	require.Contains(t, evidence.GetAssessment().GetRiskReasons(), "no_identity_keys")
}
