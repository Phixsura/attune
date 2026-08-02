package feedback

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	signalgraphrepo "github.com/Phixsura/attune/internal/repo/signalgraph"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	signalgraphsvc "github.com/Phixsura/attune/internal/service/signalgraph"
)

const (
	identityReviewScanLimit    = 250
	identityReviewMergeLimit   = 10
	identityReviewSubjectLimit = 6
	identityReviewEventLimit   = 20
	identityReviewExcerpt      = 140
)

type identityCandidateGroup struct {
	kind     string
	value    string
	evidence []*attunev1.FeedbackIdentityReviewEvidence
	sources  map[string]struct{}
	latest   time.Time
}

func (h *FeedbackHandler) GetIdentityReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetFeedbackIdentityReviewRequest,
) (dispatcher.Result[*attunev1.GetFeedbackIdentityReviewResponse], error) {
	const where = "console.FeedbackHandler.GetIdentityReview"
	auth := ctx.Auth
	rows, err := h.repo.IdentityReviewRows(ctx, auth.TenantID, identityReviewScanLimit)
	if err != nil {
		logext.Errorf(ctx, "[%s] feedback.IdentityReviewRows failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.GetFeedbackIdentityReviewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load identity review")
	}
	resp := buildFeedbackIdentityReview(rows)
	if h.identityGraph != nil {
		recentMerges, err := h.identityGraph.RecentIdentityMerges(ctx, auth.TenantID, identityReviewMergeLimit)
		if err != nil {
			logext.Errorf(ctx, "[%s] signalgraph.RecentIdentityMerges failed,tenant_id:%s,err:%+v",
				where, auth.TenantID, err.Error())
			return dispatcher.Fail[*attunev1.GetFeedbackIdentityReviewResponse](
				http.StatusInternalServerError,
				attunev1.ErrorCode_INTERNAL,
				"failed to load recent identity merges",
			)
		}
		resp.RecentMerges = signalRecentMergesToProto(recentMerges)
		subjectRoster, err := h.identityGraph.SubjectRoster(ctx, auth.TenantID, identityReviewSubjectLimit)
		if err != nil {
			logext.Errorf(ctx, "[%s] signalgraph.SubjectRoster failed,tenant_id:%s,err:%+v",
				where, auth.TenantID, err.Error())
			return dispatcher.Fail[*attunev1.GetFeedbackIdentityReviewResponse](
				http.StatusInternalServerError,
				attunev1.ErrorCode_INTERNAL,
				"failed to load identity subject roster",
			)
		}
		resp.SubjectRoster = signalSubjectRosterToProto(subjectRoster)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,scanned:%d,candidates:%d,needs_evidence:%d",
		where, auth.TenantID, len(rows), len(resp.GetMergeCandidates()), len(resp.GetNeedsEvidence()))
	return dispatcher.OK(resp)
}

func (h *FeedbackHandler) MergeIdentityReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.MergeFeedbackIdentityReviewRequest,
) (dispatcher.Result[*attunev1.MergeFeedbackIdentityReviewResponse], error) {
	const where = "console.FeedbackHandler.MergeIdentityReview"
	if h.identityGraph == nil {
		return dispatcher.Fail[*attunev1.MergeFeedbackIdentityReviewResponse](
			http.StatusNotImplemented,
			attunev1.ErrorCode_INTERNAL,
			"identity graph merge is not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.identityGraph.MergeIdentityReview(ctx, signalgraphsvc.MergeIdentityReviewInput{
		TenantID:      auth.TenantID,
		IdentityKind:  req.GetIdentityKind(),
		IdentityValue: req.GetIdentityValue(),
		FeedbackIDs:   req.GetFeedbackIds(),
		Note:          req.GetNote(),
		Actor:         auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, ctx.Request()),
	})
	if err != nil {
		logext.Warnf(ctx, "[%s] merge failed,tenant_id:%s,identity_kind:%s,feedback_count:%d,err:%+v",
			where, auth.TenantID, req.GetIdentityKind(), len(req.GetFeedbackIds()), err.Error())
		return mergeIdentityReviewError(err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,subject_id:%s,evidence_count:%d",
		where, auth.TenantID, result.Subject.ID.String(), result.EvidenceCount)
	return dispatcher.OK(ptrext.Of(attunev1.MergeFeedbackIdentityReviewResponse{
		Subject:        signalSubjectToProto(result.Subject),
		EvidenceCount:  int32(result.EvidenceCount),
		CreatedSubject: result.CreatedSubject,
		Action:         "signal_subject.merge",
	}))
}

func (h *FeedbackHandler) GetIdentitySubject(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetFeedbackIdentitySubjectRequest,
) (dispatcher.Result[*attunev1.FeedbackIdentitySubjectDetail], error) {
	const where = "console.FeedbackHandler.GetIdentitySubject"
	if h.identityGraph == nil {
		return dispatcher.Fail[*attunev1.FeedbackIdentitySubjectDetail](
			http.StatusNotImplemented,
			attunev1.ErrorCode_INTERNAL,
			"identity graph subject detail is not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.identityGraph.SubjectDetail(ctx, auth.TenantID, req.GetSubjectId(), identityReviewEventLimit)
	if err != nil {
		logext.Warnf(ctx, "[%s] subject detail failed,tenant_id:%s,subject_id:%s,err:%+v",
			where, auth.TenantID, req.GetSubjectId(), err.Error())
		return identitySubjectDetailError(err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,subject_id:%s,identities:%d,events:%d",
		where, auth.TenantID, result.Subject.ID.String(), len(result.Identities), len(result.Events))
	return dispatcher.OK(signalSubjectDetailToProto(result))
}

func (h *FeedbackHandler) SplitIdentityReview(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SplitFeedbackIdentityReviewRequest,
) (dispatcher.Result[*attunev1.SplitFeedbackIdentityReviewResponse], error) {
	const where = "console.FeedbackHandler.SplitIdentityReview"
	if h.identityGraph == nil {
		return dispatcher.Fail[*attunev1.SplitFeedbackIdentityReviewResponse](
			http.StatusNotImplemented,
			attunev1.ErrorCode_INTERNAL,
			"identity graph split is not configured",
		)
	}
	auth := ctx.Auth
	result, err := h.identityGraph.SplitIdentityReview(ctx, signalgraphsvc.SplitIdentityReviewInput{
		TenantID:      auth.TenantID,
		SubjectID:     req.GetSubjectId(),
		IdentityKind:  req.GetIdentityKind(),
		IdentityValue: req.GetIdentityValue(),
		Note:          req.GetNote(),
		Actor:         auditlogsvc.ActorFromRequest(auth.UserType, auth.UserID, ctx.Request()),
	})
	if err != nil {
		logext.Warnf(ctx, "[%s] split failed,tenant_id:%s,subject_id:%s,identity_kind:%s,err:%+v",
			where, auth.TenantID, req.GetSubjectId(), req.GetIdentityKind(), err.Error())
		return splitIdentityReviewError(err)
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,subject_id:%s,evidence_count:%d",
		where, auth.TenantID, result.Subject.ID.String(), result.EvidenceCount)
	return dispatcher.OK(ptrext.Of(attunev1.SplitFeedbackIdentityReviewResponse{
		Subject:       signalSubjectToProto(result.Subject),
		EvidenceCount: int32(result.EvidenceCount),
		Action:        "signal_subject.split",
	}))
}

func mergeIdentityReviewError(
	err error,
) (dispatcher.Result[*attunev1.MergeFeedbackIdentityReviewResponse], error) {
	switch {
	case errors.Is(err, signalgraphsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.MergeFeedbackIdentityReviewResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"identity merge request is invalid",
		)
	case errors.Is(err, signalgraphsvc.ErrFeedbackNotFound):
		return dispatcher.Fail[*attunev1.MergeFeedbackIdentityReviewResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_NOT_FOUND,
			"identity merge evidence was not found",
		)
	case errors.Is(err, signalgraphsvc.ErrConflict):
		return dispatcher.Fail[*attunev1.MergeFeedbackIdentityReviewResponse](
			http.StatusConflict,
			attunev1.ErrorCode_CONFLICT,
			"identity key is already linked to another subject",
		)
	default:
		return dispatcher.Fail[*attunev1.MergeFeedbackIdentityReviewResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to merge identity review",
		)
	}
}

func splitIdentityReviewError(
	err error,
) (dispatcher.Result[*attunev1.SplitFeedbackIdentityReviewResponse], error) {
	switch {
	case errors.Is(err, signalgraphsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.SplitFeedbackIdentityReviewResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"identity split request is invalid",
		)
	case errors.Is(err, signalgraphsvc.ErrIdentityNotFound):
		return dispatcher.Fail[*attunev1.SplitFeedbackIdentityReviewResponse](
			http.StatusNotFound,
			attunev1.ErrorCode_NOT_FOUND,
			"identity merge was not found",
		)
	default:
		return dispatcher.Fail[*attunev1.SplitFeedbackIdentityReviewResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to split identity review",
		)
	}
}

func identitySubjectDetailError(
	err error,
) (dispatcher.Result[*attunev1.FeedbackIdentitySubjectDetail], error) {
	switch {
	case errors.Is(err, signalgraphsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.FeedbackIdentitySubjectDetail](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"identity subject request is invalid",
		)
	case errors.Is(err, signalgraphsvc.ErrSubjectNotFound):
		return dispatcher.Fail[*attunev1.FeedbackIdentitySubjectDetail](
			http.StatusNotFound,
			attunev1.ErrorCode_NOT_FOUND,
			"identity subject was not found",
		)
	default:
		return dispatcher.Fail[*attunev1.FeedbackIdentitySubjectDetail](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to load identity subject",
		)
	}
}

func signalSubjectToProto(subject signalgraphrepo.Subject) *attunev1.FeedbackIdentitySubject {
	return ptrext.Of(attunev1.FeedbackIdentitySubject{
		Id:                   subject.ID.String(),
		DisplayName:          subject.DisplayName,
		PrimaryIdentityKind:  subject.PrimaryIdentityKind,
		PrimaryIdentityValue: subject.PrimaryIdentityValue,
		Status:               subject.Status,
		IdentityCount:        int32(subject.IdentityCount),
		EvidenceCount:        int32(subject.EvidenceCount),
		CreatedAt:            subject.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:            subject.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func signalRecentMergesToProto(items []signalgraphrepo.RecentMerge) []*attunev1.FeedbackIdentityReviewedMerge {
	out := make([]*attunev1.FeedbackIdentityReviewedMerge, 0, len(items))
	for _, item := range items {
		out = append(out, ptrext.Of(attunev1.FeedbackIdentityReviewedMerge{
			EventId:       item.EventID.String(),
			Subject:       signalSubjectToProto(item.Subject),
			IdentityKind:  item.IdentityKind,
			IdentityValue: item.IdentityValue,
			EvidenceCount: int32(item.EvidenceCount),
			FeedbackIds:   item.FeedbackIDs,
			CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339),
			CreatedBy:     item.CreatedBy,
		}))
	}
	return out
}

func signalSubjectRosterToProto(roster signalgraphrepo.SubjectRoster) *attunev1.FeedbackIdentitySubjectRoster {
	subjects := make([]*attunev1.FeedbackIdentitySubject, 0, len(roster.Subjects))
	for _, subject := range roster.Subjects {
		subjects = append(subjects, signalSubjectToProto(subject))
	}
	return ptrext.Of(attunev1.FeedbackIdentitySubjectRoster{
		ActiveSubjectCount:  int32(roster.ActiveSubjectCount),
		ActiveIdentityCount: int32(roster.ActiveIdentityCount),
		EvidenceCount:       int32(roster.EvidenceCount),
		Subjects:            subjects,
	})
}

func signalSubjectDetailToProto(detail signalgraphrepo.SubjectDetail) *attunev1.FeedbackIdentitySubjectDetail {
	return ptrext.Of(attunev1.FeedbackIdentitySubjectDetail{
		Subject:    signalSubjectToProto(detail.Subject),
		Identities: signalSubjectIdentitiesToProto(detail.Identities),
		Events:     signalSubjectEventsToProto(detail.Events),
	})
}

func signalSubjectIdentitiesToProto(items []signalgraphrepo.SubjectIdentity) []*attunev1.FeedbackIdentitySubjectIdentity {
	out := make([]*attunev1.FeedbackIdentitySubjectIdentity, 0, len(items))
	for _, item := range items {
		revokedAt := ""
		if item.RevokedAt.Valid {
			revokedAt = item.RevokedAt.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, ptrext.Of(attunev1.FeedbackIdentitySubjectIdentity{
			Id:               item.ID.String(),
			Kind:             item.Kind,
			Value:            item.Value,
			Source:           item.Source,
			Confidence:       item.Confidence,
			EvidenceCount:    int32(item.EvidenceCount),
			FirstFeedbackId:  item.FirstFeedbackID,
			LatestFeedbackId: item.LatestFeedbackID,
			Revoked:          item.Revoked,
			RevokedAt:        revokedAt,
			CreatedAt:        item.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:        item.UpdatedAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func signalSubjectEventsToProto(items []signalgraphrepo.SubjectEvent) []*attunev1.FeedbackIdentitySubjectEvent {
	out := make([]*attunev1.FeedbackIdentitySubjectEvent, 0, len(items))
	for _, item := range items {
		out = append(out, ptrext.Of(attunev1.FeedbackIdentitySubjectEvent{
			Id:            item.ID.String(),
			Action:        item.Action,
			IdentityKind:  item.IdentityKind,
			IdentityValue: item.IdentityValue,
			EvidenceCount: int32(item.EvidenceCount),
			FeedbackIds:   item.FeedbackIDs,
			Evidence:      signalSubjectEventEvidenceToProto(item.Evidence),
			Note:          item.Note,
			CreatedBy:     item.CreatedBy,
			CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339),
		}))
	}
	return out
}

func signalSubjectEventEvidenceToProto(
	items []signalgraphrepo.SubjectEventEvidence,
) []*attunev1.FeedbackIdentityReviewEvidence {
	out := make([]*attunev1.FeedbackIdentityReviewEvidence, 0, len(items))
	for _, item := range items {
		identity := buildFeedbackIdentityEvidence(item.UserID, item.SourceMeta)
		out = append(out, ptrext.Of(attunev1.FeedbackIdentityReviewEvidence{
			FeedbackId: item.ID,
			Source:     item.Source,
			SourceUser: item.UserID,
			CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339),
			Excerpt:    truncateRunes(strings.TrimSpace(item.Content), identityReviewExcerpt),
			Keys:       identity.GetKeys(),
		}))
	}
	return out
}

func buildFeedbackIdentityReview(
	rows []feedbackrepo.IdentityReviewRow,
) *attunev1.GetFeedbackIdentityReviewResponse {
	groups := make(map[string]*identityCandidateGroup)
	needsEvidence := make([]*attunev1.FeedbackIdentityNeedsEvidenceItem, 0)
	weakCount := 0
	for _, row := range rows {
		evidence := identityReviewEvidence(row)
		identity := buildFeedbackIdentityEvidence(row.UserID, row.SourceMeta)
		assessment := identity.GetAssessment()
		if assessment.GetStrength() == attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_WEAK {
			weakCount++
			needsEvidence = append(needsEvidence, identityNeedsEvidenceItem(evidence, assessment))
		}
		addIdentityReviewGroups(groups, identity.GetKeys(), evidence, row.CreatedAt)
	}
	candidates, strongCount := identityMergeCandidates(groups)
	return ptrext.Of(attunev1.GetFeedbackIdentityReviewResponse{
		Summary: ptrext.Of(attunev1.FeedbackIdentityReviewSummary{
			ScannedFeedbackCount: int32(len(rows)),
			MergeCandidateCount:  int32(len(candidates)),
			NeedsEvidenceCount:   int32(len(needsEvidence)),
			StrongCandidateCount: int32(strongCount),
			WeakFeedbackCount:    int32(weakCount),
		}),
		MergeCandidates: candidates,
		NeedsEvidence:   limitNeedsEvidence(needsEvidence, 10),
	})
}

func identityReviewEvidence(row feedbackrepo.IdentityReviewRow) *attunev1.FeedbackIdentityReviewEvidence {
	identity := buildFeedbackIdentityEvidence(row.UserID, row.SourceMeta)
	return ptrext.Of(attunev1.FeedbackIdentityReviewEvidence{
		FeedbackId: row.ID,
		Source:     row.Source,
		SourceUser: row.UserID,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		Excerpt:    truncateRunes(strings.TrimSpace(row.Content), identityReviewExcerpt),
		Keys:       identity.GetKeys(),
	})
}

func identityNeedsEvidenceItem(
	evidence *attunev1.FeedbackIdentityReviewEvidence,
	assessment *attunev1.FeedbackIdentityAssessment,
) *attunev1.FeedbackIdentityNeedsEvidenceItem {
	return ptrext.Of(attunev1.FeedbackIdentityNeedsEvidenceItem{
		Evidence:   evidence,
		Assessment: assessment,
	})
}

func addIdentityReviewGroups(
	groups map[string]*identityCandidateGroup,
	keys []*attunev1.FeedbackIdentityKey,
	evidence *attunev1.FeedbackIdentityReviewEvidence,
	createdAt time.Time,
) {
	for _, key := range keys {
		if !identityReviewStableKind(key.GetKind()) {
			continue
		}
		group := identityReviewGroup(groups, key)
		group.evidence = append(group.evidence, evidence)
		group.sources[evidence.GetSource()] = struct{}{}
		if createdAt.After(group.latest) {
			group.latest = createdAt
		}
	}
}

func identityReviewGroup(
	groups map[string]*identityCandidateGroup,
	key *attunev1.FeedbackIdentityKey,
) *identityCandidateGroup {
	groupKey := key.GetKind() + "\x00" + strings.ToLower(key.GetValue())
	if group, ok := groups[groupKey]; ok {
		return group
	}
	group := ptrext.Of(identityCandidateGroup{
		kind:    key.GetKind(),
		value:   key.GetValue(),
		sources: make(map[string]struct{}),
	})
	groups[groupKey] = group
	return group
}

func identityMergeCandidates(groups map[string]*identityCandidateGroup) ([]*attunev1.FeedbackIdentityMergeCandidate, int) {
	candidates := make([]*attunev1.FeedbackIdentityMergeCandidate, 0, len(groups))
	strongCount := 0
	for _, group := range groups {
		if len(group.evidence) < 2 {
			continue
		}
		candidate := identityMergeCandidate(group)
		if candidate.GetStrength() == attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG {
			strongCount++
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return identityCandidateLess(candidates[i], candidates[j])
	})
	return candidates, strongCount
}

func identityMergeCandidate(group *identityCandidateGroup) *attunev1.FeedbackIdentityMergeCandidate {
	strength := identityCandidateStrength(group)
	return ptrext.Of(attunev1.FeedbackIdentityMergeCandidate{
		IdentityKind:      group.kind,
		IdentityValue:     group.value,
		FeedbackCount:     int32(len(group.evidence)),
		SourceCount:       int32(len(group.sources)),
		StableKeyCount:    int32(identityCandidateStableKeyCount(group.evidence)),
		LatestFeedbackAt:  group.latest.UTC().Format(time.RFC3339),
		Strength:          strength,
		RecommendedAction: feedbackIdentityRecommendedAction(strength),
		RiskReasons:       identityCandidateRiskReasons(group),
		Evidence:          limitIdentityEvidence(group.evidence, 5),
	})
}

func identityCandidateStrength(group *identityCandidateGroup) attunev1.FeedbackIdentityResolutionStrength {
	if len(group.evidence) >= 2 && len(group.sources) >= 2 {
		return attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_STRONG
	}
	return attunev1.FeedbackIdentityResolutionStrength_FEEDBACK_IDENTITY_RESOLUTION_STRENGTH_MEDIUM
}

func identityCandidateStableKeyCount(evidence []*attunev1.FeedbackIdentityReviewEvidence) int {
	seen := make(map[string]struct{})
	for _, item := range evidence {
		for _, key := range item.GetKeys() {
			if identityReviewStableKind(key.GetKind()) {
				seen[key.GetKind()+"\x00"+strings.ToLower(key.GetValue())] = struct{}{}
			}
		}
	}
	return len(seen)
}

func identityCandidateRiskReasons(group *identityCandidateGroup) []string {
	if len(group.sources) <= 1 {
		return []string{"single_source_path"}
	}
	return nil
}

func identityCandidateLess(
	left *attunev1.FeedbackIdentityMergeCandidate,
	right *attunev1.FeedbackIdentityMergeCandidate,
) bool {
	if left.GetStrength() != right.GetStrength() {
		return left.GetStrength() < right.GetStrength()
	}
	if left.GetFeedbackCount() != right.GetFeedbackCount() {
		return left.GetFeedbackCount() > right.GetFeedbackCount()
	}
	return left.GetLatestFeedbackAt() > right.GetLatestFeedbackAt()
}

func identityReviewStableKind(kind string) bool {
	return kind != "" && kind != "source_user"
}

func limitIdentityEvidence(
	items []*attunev1.FeedbackIdentityReviewEvidence,
	limit int,
) []*attunev1.FeedbackIdentityReviewEvidence {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func limitNeedsEvidence(
	items []*attunev1.FeedbackIdentityNeedsEvidenceItem,
	limit int,
) []*attunev1.FeedbackIdentityNeedsEvidenceItem {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
