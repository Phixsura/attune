package feedback

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

const maxIdentityEvidenceValueLength = 512

var identityAssessmentStableKinds = []string{
	"email",
	"external_id",
	"source_contact_id",
	"crm_id",
	"support_id",
}

type identityEvidenceCollector struct {
	items []*attunev1.FeedbackIdentityKey
	seen  map[string]struct{}
}

type identityEvidenceProfile struct {
	present        map[string]bool
	sourceCount    int
	stableKeyCount int
}

func buildFeedbackIdentityEvidence(
	userID string, sourceMeta []byte,
) *attunev1.FeedbackIdentityEvidence {
	collector := identityEvidenceCollector{seen: make(map[string]struct{})}
	sourceUser := strings.TrimSpace(userID)
	if sourceUser != "" {
		collector.add("source_user", sourceUser, "user_id")
	}

	if len(sourceMeta) > 0 {
		var decoded map[string]any
		if json.Unmarshal(sourceMeta, &decoded) == nil && decoded != nil {
			collector.collectFromMap(decoded, "source_meta")
		}
	}

	sort.SliceStable(collector.items, func(i, j int) bool {
		left := collector.items[i]
		right := collector.items[j]
		leftRank := identityEvidenceKindRank(left.GetKind())
		rightRank := identityEvidenceKindRank(right.GetKind())
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.GetSource() != right.GetSource() {
			return left.GetSource() < right.GetSource()
		}
		return left.GetValue() < right.GetValue()
	})

	evidence := ptrext.Of(attunev1.FeedbackIdentityEvidence{
		SourceUser:          sourceUser,
		Keys:                collector.items,
		MergeCandidateCount: int32(len(collector.items)),
		Assessment:          buildFeedbackIdentityAssessment(collector.items),
	})
	for _, key := range collector.items {
		switch key.GetKind() {
		case "email":
			evidence.HasEmail = true
		case "external_id":
			evidence.HasExternalId = true
		case "source_contact_id":
			evidence.HasSourceContactId = true
		case "crm_id":
			evidence.HasCrmId = true
		case "support_id":
			evidence.HasSupportId = true
		}
	}
	return evidence
}

func buildFeedbackIdentityAssessment(keys []*attunev1.FeedbackIdentityKey) *attunev1.FeedbackIdentityAssessment {
	profile := feedbackIdentityProfile(keys)
	strength := feedbackIdentityStrength(profile)
	return ptrext.Of(attunev1.FeedbackIdentityAssessment{
		Strength:          strength,
		RecommendedAction: feedbackIdentityRecommendedAction(strength),
		MissingKinds:      feedbackIdentityMissingKinds(profile),
		RiskReasons:       feedbackIdentityRiskReasons(profile),
		StableKeyCount:    int32(profile.stableKeyCount),
		SourceCount:       int32(profile.sourceCount),
	})
}

func feedbackIdentityProfile(keys []*attunev1.FeedbackIdentityKey) identityEvidenceProfile {
	sources := make(map[string]struct{})
	profile := identityEvidenceProfile{present: make(map[string]bool)}
	for _, key := range keys {
		kind := key.GetKind()
		if kind == "" {
			continue
		}
		profile.present[kind] = true
		if source := key.GetSource(); source != "" {
			sources[source] = struct{}{}
		}
		if kind != "source_user" {
			profile.stableKeyCount++
		}
	}
	profile.sourceCount = len(sources)
	return profile
}

func feedbackIdentityStrength(profile identityEvidenceProfile) attunev1.FeedbackIdentityResolutionStrength {
	if profile.stableKeyCount >= 2 && profile.sourceCount >= 2 && feedbackIdentityHasAnchoredKey(profile) {
		return attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG
	}
	if profile.stableKeyCount >= 1 {
		return attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_MEDIUM
	}
	return attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_WEAK
}

func feedbackIdentityHasAnchoredKey(profile identityEvidenceProfile) bool {
	return profile.present["email"] || profile.present["crm_id"] || profile.present["support_id"]
}

func feedbackIdentityRecommendedAction(
	strength attunev1.FeedbackIdentityResolutionStrength,
) attunev1.FeedbackIdentityRecommendedAction {
	switch strength {
	case attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG:
		return attunev1.FeedbackIdentityRecommendedAction_FEEDBACK_IDENTITY_RECOMMENDED_ACTION_REVIEW_MERGE
	case attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_MEDIUM:
		return attunev1.FeedbackIdentityRecommendedAction_FEEDBACK_IDENTITY_RECOMMENDED_ACTION_REVIEW_WITH_CONTEXT
	default:
		return attunev1.FeedbackIdentityRecommendedAction_FEEDBACK_IDENTITY_RECOMMENDED_ACTION_CAPTURE_MORE_KEYS
	}
}

func feedbackIdentityMissingKinds(profile identityEvidenceProfile) []string {
	missing := make([]string, 0, len(identityAssessmentStableKinds))
	for _, kind := range identityAssessmentStableKinds {
		if !profile.present[kind] {
			missing = append(missing, kind)
		}
	}
	return missing
}

func feedbackIdentityRiskReasons(profile identityEvidenceProfile) []string {
	reasons := make([]string, 0, 5)
	if profile.stableKeyCount == 0 {
		reasons = append(reasons, feedbackIdentityNoStableKeyRisk(profile))
	}
	if profile.stableKeyCount == 1 {
		reasons = append(reasons, "single_stable_key")
	}
	if !profile.present["email"] {
		reasons = append(reasons, "missing_email")
	}
	if !profile.present["crm_id"] && !profile.present["support_id"] {
		reasons = append(reasons, "missing_system_context")
	}
	if profile.sourceCount <= 1 {
		reasons = append(reasons, "single_source_path")
	}
	return reasons
}

func feedbackIdentityNoStableKeyRisk(profile identityEvidenceProfile) string {
	if profile.present["source_user"] {
		return "only_source_user"
	}
	return "no_identity_keys"
}

func (collector *identityEvidenceCollector) collectFromMap(values map[string]any, path string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		nextPath := path + "." + key
		if kind, ok := identityEvidenceKindForKey(key); ok {
			collector.addValue(kind, value, nextPath)
		}
	}
	for _, key := range keys {
		value := values[key]
		nextPath := path + "." + key
		collector.collectNested(value, nextPath)
	}
}

func (collector *identityEvidenceCollector) collectNested(value any, path string) {
	switch typed := value.(type) {
	case map[string]any:
		collector.collectFromMap(typed, path)
	case []any:
		for idx, item := range typed {
			collector.collectNested(item, fmt.Sprintf("%s[%d]", path, idx))
		}
	}
}

func (collector *identityEvidenceCollector) addValue(kind string, value any, source string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collector.addValue(kind, item, source)
		}
	default:
		if normalized := identityEvidenceScalarValue(typed); normalized != "" {
			collector.add(kind, normalized, source)
		}
	}
}

func (collector *identityEvidenceCollector) add(kind string, value string, source string) {
	if len(value) > maxIdentityEvidenceValueLength {
		return
	}
	dedupeKey := kind + "\x00" + strings.ToLower(value)
	if _, ok := collector.seen[dedupeKey]; ok {
		return
	}
	collector.seen[dedupeKey] = struct{}{}
	collector.items = append(collector.items, ptrext.Of(attunev1.FeedbackIdentityKey{
		Kind:   kind,
		Source: source,
		Value:  value,
	}))
}

func identityEvidenceScalarValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func identityEvidenceKindForKey(key string) (string, bool) {
	switch normalizeIdentityEvidenceKey(key) {
	case "email", "useremail", "customeremail", "contactemail", "requesteremail",
		"submitteremail", "authoremail":
		return "email", true
	case "externalid", "externaluserid", "userexternalid", "customerexternalid",
		"sourceexternalid":
		return "external_id", true
	case "sourcecontactid", "sourcecontact", "contactid", "requesterid",
		"customerid":
		return "source_contact_id", true
	case "crmid", "crmcontactid", "salesforceid", "salesforcecontactid",
		"hubspotid", "hubspotcontactid":
		return "crm_id", true
	case "supportid", "supportuserid", "zendeskid", "zendeskuserid",
		"intercomid", "intercomuserid", "freshdeskid", "freshdeskuserid":
		return "support_id", true
	default:
		return "", false
	}
}

func normalizeIdentityEvidenceKey(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func identityEvidenceKindRank(kind string) int {
	switch kind {
	case "source_user":
		return 0
	case "email":
		return 1
	case "external_id":
		return 2
	case "source_contact_id":
		return 3
	case "crm_id":
		return 4
	case "support_id":
		return 5
	default:
		return 9
	}
}
