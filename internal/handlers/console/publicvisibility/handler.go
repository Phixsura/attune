// SPDX-License-Identifier: Apache-2.0

// Package publicvisibility implements the Console public visibility API.
package publicvisibility

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

type service interface {
	GetPolicy(ctx context.Context, tenantID string) (repo.Policy, error)
	UpdatePolicy(ctx context.Context, in svc.UpdatePolicyInput) (repo.Policy, error)
	ListModeration(ctx context.Context, in svc.ListModerationInput) (repo.ListResult, error)
	GetRequestPublication(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.RequestPublication, error)
	UpsertRequestProfile(ctx context.Context, in svc.UpsertRequestProfileInput) (repo.RequestPublication, error)
	Moderate(ctx context.Context, in svc.ModerateInput) (repo.ModerationSubject, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) GetPolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetPublicVisibilityPolicyRequest,
) (dispatcher.Result[*attunev1.PublicVisibilityPolicy], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.PublicVisibilityPolicy](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	policy, err := h.service.GetPolicy(ctx, ctx.Auth.TenantID)
	if err != nil {
		return policyError[*attunev1.PublicVisibilityPolicy](err)
	}
	return dispatcher.OK(policyToProto(policy))
}

func (h *Handler) UpdatePolicy(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdatePublicVisibilityPolicyRequest,
) (dispatcher.Result[*attunev1.PublicVisibilityPolicy], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.PublicVisibilityPolicy](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	policy, err := h.service.UpdatePolicy(ctx, svc.UpdatePolicyInput{
		TenantID:              ctx.Auth.TenantID,
		PortalAccessMode:      accessModeFromProto(req.GetPortalAccessMode()),
		SearchIndexingEnabled: req.GetSearchIndexingEnabled(),
		RequestsEnabled:       req.GetRequestsEnabled(),
		CommentsEnabled:       req.GetCommentsEnabled(),
		RoadmapEnabled:        req.GetRoadmapEnabled(),
		ChangelogEnabled:      req.GetChangelogEnabled(),
		SubmissionWriteMode:   writeModeFromProto(req.GetSubmissionWriteMode()),
		CommentWriteMode:      writeModeFromProto(req.GetCommentWriteMode()),
		VoteWriteMode:         writeModeFromProto(req.GetVoteWriteMode()),
		DefaultRequestState:   stateFromProto(req.GetDefaultRequestState()),
		DefaultCommentState:   stateFromProto(req.GetDefaultCommentState()),
		SubmitterIdentityMode: identityModeFromProto(req.GetSubmitterIdentityMode()),
		ShowVoteCount:         req.GetShowVoteCount(),
		ShowCommentCount:      req.GetShowCommentCount(),
		ShowSubmitterDisplay:  req.GetShowSubmitterDisplay(),
		HidePublicTimestamps:  req.GetHidePublicTimestamps(),
		Actor:                 actor(ctx),
	})
	if err != nil {
		return policyError[*attunev1.PublicVisibilityPolicy](err)
	}
	return dispatcher.OK(policyToProto(policy))
}

func (h *Handler) ListModeration(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListModerationSubjectsRequest,
) (dispatcher.Result[*attunev1.ListModerationSubjectsResponse], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.ListModerationSubjectsResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	result, err := h.service.ListModeration(ctx, svc.ListModerationInput{
		TenantID: ctx.Auth.TenantID,
		Surfaces: surfacesFromProto(req.GetSurface()),
		States:   statesFromProto(req.GetState()),
		Limit:    int(req.GetLimit()),
		Cursor:   req.GetCursor(),
	})
	if err != nil {
		return policyError[*attunev1.ListModerationSubjectsResponse](err)
	}
	out := ptrext.Of(attunev1.ListModerationSubjectsResponse{
		Subjects: make([]*attunev1.ModerationSubject, 0, len(result.Items)),
	})
	for _, item := range result.Items {
		out.Subjects = append(out.Subjects, subjectToProto(item))
	}
	if result.NextCursor != "" {
		out.NextCursor = ptrext.Of(result.NextCursor)
	}
	return dispatcher.OK(out)
}

func (h *Handler) GetRequestProfile(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetPublicRequestProfileRequest,
) (dispatcher.Result[*attunev1.PublicRequestPublication], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.PublicRequestPublication](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	requestID, err := uuid.Parse(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicRequestPublication](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	publication, err := h.service.GetRequestPublication(ctx, ctx.Auth.TenantID, requestID)
	if err != nil {
		return policyError[*attunev1.PublicRequestPublication](err)
	}
	return dispatcher.OK(publicationToProto(publication))
}

func (h *Handler) UpsertRequestProfile(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpsertPublicRequestProfileRequest,
) (dispatcher.Result[*attunev1.PublicRequestPublication], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.PublicRequestPublication](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	requestID, err := uuid.Parse(req.GetRequestId())
	if err != nil {
		return dispatcher.Fail[*attunev1.PublicRequestPublication](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid customer request id")
	}
	publication, err := h.service.UpsertRequestProfile(ctx, svc.UpsertRequestProfileInput{
		TenantID:           ctx.Auth.TenantID,
		RequestID:          requestID,
		PublicSlug:         req.GetPublicSlug(),
		PublicTitle:        req.GetPublicTitle(),
		PublicSummary:      req.GetPublicSummary(),
		PublicState:        req.GetPublicState(),
		RoadmapColumn:      req.GetRoadmapColumn(),
		IncludedInPortal:   req.GetIncludedInPortal(),
		IncludedInRoadmap:  req.GetIncludedInRoadmap(),
		SubmittedByDisplay: req.GetSubmittedByDisplay(),
		Actor:              actor(ctx),
	})
	if err != nil {
		return policyError[*attunev1.PublicRequestPublication](err)
	}
	return dispatcher.OK(publicationToProto(publication))
}

func (h *Handler) Approve(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ApproveModerationSubjectRequest,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	return h.moderate(ctx, req.GetId(), req.GetReasonCode(), req.GetReasonNote(), svc.ActionApprove)
}

func (h *Handler) Reject(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RejectModerationSubjectRequest,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	return h.moderate(ctx, req.GetId(), req.GetReasonCode(), req.GetReasonNote(), svc.ActionReject)
}

func (h *Handler) Hide(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.HideModerationSubjectRequest,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	return h.moderate(ctx, req.GetId(), req.GetReasonCode(), req.GetReasonNote(), svc.ActionHide)
}

func (h *Handler) MarkSpam(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.MarkModerationSubjectSpamRequest,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	return h.moderate(ctx, req.GetId(), req.GetReasonCode(), req.GetReasonNote(), svc.ActionMarkSpam)
}

func (h *Handler) Restore(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.RestoreModerationSubjectRequest,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	return h.moderate(ctx, req.GetId(), req.GetReasonCode(), req.GetReasonNote(), svc.ActionRestore)
}

func (h *Handler) moderate(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	rawID string,
	reasonCode string,
	reasonNote string,
	action svc.ModerationAction,
) (dispatcher.Result[*attunev1.ModerationSubject], error) {
	if h.service == nil {
		return dispatcher.Fail[*attunev1.ModerationSubject](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "public visibility not configured")
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return dispatcher.Fail[*attunev1.ModerationSubject](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid moderation subject id")
	}
	subject, err := h.service.Moderate(ctx, svc.ModerateInput{
		TenantID:   ctx.Auth.TenantID,
		ID:         id,
		Action:     action,
		ReasonCode: reasonCode,
		ReasonNote: reasonNote,
		Actor:      actor(ctx),
	})
	if err != nil {
		return policyError[*attunev1.ModerationSubject](err)
	}
	return dispatcher.OK(subjectToProto(subject))
}

func policyError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, svc.ErrValidation), errors.Is(err, repo.ErrInvalidInput), errors.Is(err, svc.ErrInvalidTransition):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "invalid public visibility request")
	case errors.Is(err, svc.ErrNotFound), errors.Is(err, repo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public visibility resource not found")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public visibility request failed")
	}
}

func actor(ctx *dispatcher.RequestContext[*session.AuthCtx]) auditlogsvc.Actor {
	return auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request())
}

func policyToProto(policy repo.Policy) *attunev1.PublicVisibilityPolicy {
	return ptrext.Of(attunev1.PublicVisibilityPolicy{
		TenantId:              policy.TenantID,
		PortalAccessMode:      accessModeToProto(policy.PortalAccessMode),
		SearchIndexingEnabled: policy.SearchIndexingEnabled,
		RequestsEnabled:       policy.RequestsEnabled,
		CommentsEnabled:       policy.CommentsEnabled,
		RoadmapEnabled:        policy.RoadmapEnabled,
		ChangelogEnabled:      policy.ChangelogEnabled,
		SubmissionWriteMode:   writeModeToProto(policy.SubmissionWriteMode),
		CommentWriteMode:      writeModeToProto(policy.CommentWriteMode),
		VoteWriteMode:         writeModeToProto(policy.VoteWriteMode),
		DefaultRequestState:   stateToProto(policy.DefaultRequestState),
		DefaultCommentState:   stateToProto(policy.DefaultCommentState),
		SubmitterIdentityMode: identityModeToProto(policy.SubmitterIdentityMode),
		ShowVoteCount:         policy.ShowVoteCount,
		ShowCommentCount:      policy.ShowCommentCount,
		ShowSubmitterDisplay:  policy.ShowSubmitterDisplay,
		HidePublicTimestamps:  policy.HidePublicTimestamps,
		UpdatedBy:             policy.UpdatedBy,
		CreatedAt:             formatTime(ptrext.Of(policy.CreatedAt)),
		UpdatedAt:             formatTime(ptrext.Of(policy.UpdatedAt)),
	})
}

func subjectToProto(subject repo.ModerationSubject) *attunev1.ModerationSubject {
	return ptrext.Of(attunev1.ModerationSubject{
		Id:                 subject.ID.String(),
		TenantId:           subject.TenantID,
		Surface:            surfaceToProto(subject.Surface),
		SubjectId:          subject.SubjectID,
		State:              stateToProto(subject.State),
		ReasonCode:         subject.ReasonCode,
		ReasonNote:         subject.ReasonNote,
		SubmittedByDisplay: subject.SubmittedByDisplay,
		ReviewedBy:         subject.ReviewedBy,
		ReviewedAt:         optionalTime(subject.ReviewedAt),
		CreatedAt:          formatTime(ptrext.Of(subject.CreatedAt)),
		UpdatedAt:          formatTime(ptrext.Of(subject.UpdatedAt)),
	})
}

func publicationToProto(publication repo.RequestPublication) *attunev1.PublicRequestPublication {
	return ptrext.Of(attunev1.PublicRequestPublication{
		Profile:    profileToProto(publication.Profile),
		Moderation: subjectToProto(publication.Moderation),
	})
}

func profileToProto(profile repo.RequestProfile) *attunev1.PublicRequestProfile {
	return ptrext.Of(attunev1.PublicRequestProfile{
		Id:                profile.ID.String(),
		TenantId:          profile.TenantID,
		RequestId:         profile.RequestID.String(),
		PublicSlug:        profile.PublicSlug,
		PublicTitle:       profile.PublicTitle,
		PublicSummary:     profile.PublicSummary,
		PublicState:       profile.PublicState,
		RoadmapColumn:     profile.RoadmapColumn,
		IncludedInPortal:  profile.IncludedInPortal,
		IncludedInRoadmap: profile.IncludedInRoadmap,
		PublishedAt:       optionalTime(profile.PublishedAt),
		UpdatedBy:         profile.UpdatedBy,
		CreatedAt:         formatTime(ptrext.Of(profile.CreatedAt)),
		UpdatedAt:         formatTime(ptrext.Of(profile.UpdatedAt)),
	})
}

func accessModeFromProto(mode attunev1.PublicAccessMode) repo.AccessMode {
	switch mode {
	case attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_PUBLIC:
		return repo.AccessModePublic
	case attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_AUTHENTICATED:
		return repo.AccessModeAuthenticated
	case attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_INVITE_ONLY:
		return repo.AccessModeInviteOnly
	default:
		return repo.AccessModeDisabled
	}
}

func accessModeToProto(mode repo.AccessMode) attunev1.PublicAccessMode {
	switch mode {
	case repo.AccessModePublic:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_PUBLIC
	case repo.AccessModeAuthenticated:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_AUTHENTICATED
	case repo.AccessModeInviteOnly:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_INVITE_ONLY
	default:
		return attunev1.PublicAccessMode_PUBLIC_ACCESS_MODE_DISABLED
	}
}

func writeModeFromProto(mode attunev1.PublicWriteMode) repo.WriteMode {
	switch mode {
	case attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_ANONYMOUS:
		return repo.WriteModeAnonymous
	case attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_IDENTIFIED:
		return repo.WriteModeIdentified
	default:
		return repo.WriteModeDisabled
	}
}

func writeModeToProto(mode repo.WriteMode) attunev1.PublicWriteMode {
	switch mode {
	case repo.WriteModeAnonymous:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_ANONYMOUS
	case repo.WriteModeIdentified:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_IDENTIFIED
	default:
		return attunev1.PublicWriteMode_PUBLIC_WRITE_MODE_DISABLED
	}
}

func identityModeFromProto(mode attunev1.PublicIdentityMode) repo.IdentityMode {
	switch mode {
	case attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_DISPLAY_NAME:
		return repo.IdentityModeDisplayName
	case attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ORGANIZATION:
		return repo.IdentityModeOrganization
	default:
		return repo.IdentityModeAnonymous
	}
}

func identityModeToProto(mode repo.IdentityMode) attunev1.PublicIdentityMode {
	switch mode {
	case repo.IdentityModeDisplayName:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_DISPLAY_NAME
	case repo.IdentityModeOrganization:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ORGANIZATION
	default:
		return attunev1.PublicIdentityMode_PUBLIC_IDENTITY_MODE_ANONYMOUS
	}
}

func stateFromProto(state attunev1.ModerationState) repo.ModerationState {
	switch state {
	case attunev1.ModerationState_MODERATION_STATE_APPROVED:
		return repo.ModerationStateApproved
	case attunev1.ModerationState_MODERATION_STATE_REJECTED:
		return repo.ModerationStateRejected
	case attunev1.ModerationState_MODERATION_STATE_HIDDEN:
		return repo.ModerationStateHidden
	case attunev1.ModerationState_MODERATION_STATE_SPAM:
		return repo.ModerationStateSpam
	default:
		return repo.ModerationStatePending
	}
}

func stateToProto(state repo.ModerationState) attunev1.ModerationState {
	switch state {
	case repo.ModerationStateApproved:
		return attunev1.ModerationState_MODERATION_STATE_APPROVED
	case repo.ModerationStateRejected:
		return attunev1.ModerationState_MODERATION_STATE_REJECTED
	case repo.ModerationStateHidden:
		return attunev1.ModerationState_MODERATION_STATE_HIDDEN
	case repo.ModerationStateSpam:
		return attunev1.ModerationState_MODERATION_STATE_SPAM
	default:
		return attunev1.ModerationState_MODERATION_STATE_PENDING
	}
}

func surfaceToProto(surface repo.Surface) attunev1.PublicSurface {
	switch surface {
	case repo.SurfaceRequestComment:
		return attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST_COMMENT
	case repo.SurfaceRoadmapItem:
		return attunev1.PublicSurface_PUBLIC_SURFACE_ROADMAP_ITEM
	case repo.SurfaceChangelogPost:
		return attunev1.PublicSurface_PUBLIC_SURFACE_CHANGELOG_POST
	case repo.SurfacePortalSubmission:
		return attunev1.PublicSurface_PUBLIC_SURFACE_PORTAL_SUBMISSION
	default:
		return attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST
	}
}

func surfaceFromProto(surface attunev1.PublicSurface) repo.Surface {
	switch surface {
	case attunev1.PublicSurface_PUBLIC_SURFACE_REQUEST_COMMENT:
		return repo.SurfaceRequestComment
	case attunev1.PublicSurface_PUBLIC_SURFACE_ROADMAP_ITEM:
		return repo.SurfaceRoadmapItem
	case attunev1.PublicSurface_PUBLIC_SURFACE_CHANGELOG_POST:
		return repo.SurfaceChangelogPost
	case attunev1.PublicSurface_PUBLIC_SURFACE_PORTAL_SUBMISSION:
		return repo.SurfacePortalSubmission
	default:
		return repo.SurfaceRequest
	}
}

func surfacesFromProto(values []attunev1.PublicSurface) []repo.Surface {
	out := make([]repo.Surface, 0, len(values))
	for _, value := range values {
		if value == attunev1.PublicSurface_PUBLIC_SURFACE_UNSPECIFIED {
			continue
		}
		out = append(out, surfaceFromProto(value))
	}
	return out
}

func statesFromProto(values []attunev1.ModerationState) []repo.ModerationState {
	out := make([]repo.ModerationState, 0, len(values))
	for _, value := range values {
		if value == attunev1.ModerationState_MODERATION_STATE_UNSPECIFIED {
			continue
		}
		out = append(out, stateFromProto(value))
	}
	return out
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func optionalTime(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	return ptrext.Of(formatTime(t))
}

func BindListRequest(r *http.Request, req *attunev1.ListModerationSubjectsRequest) error {
	q := r.URL.Query()
	req.Surface = parseSurfaces(q["surface"])
	req.State = parseStates(q["state"])
	if limit := strings.TrimSpace(q.Get("limit")); limit != "" {
		parsed, err := parseUint32(limit)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid moderation limit")
		}
		req.Limit = parsed
	}
	req.Cursor = strings.TrimSpace(q.Get("cursor"))
	return nil
}

func parseSurfaces(raw []string) []attunev1.PublicSurface {
	var out []attunev1.PublicSurface
	for _, item := range splitCSV(raw) {
		if value, ok := attunev1.PublicSurface_value[item]; ok {
			out = append(out, attunev1.PublicSurface(value))
		}
	}
	return out
}

func parseStates(raw []string) []attunev1.ModerationState {
	var out []attunev1.ModerationState
	for _, item := range splitCSV(raw) {
		if value, ok := attunev1.ModerationState_value[item]; ok {
			out = append(out, attunev1.ModerationState(value))
		}
	}
	return out
}

func splitCSV(raw []string) []string {
	var out []string
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func parseUint32(raw string) (uint32, error) {
	var value uint64
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, errors.New("invalid uint32")
		}
		value = value*10 + uint64(ch-'0')
		if value > uint64(^uint32(0)) {
			return 0, errors.New("invalid uint32")
		}
	}
	return uint32(value), nil
}
