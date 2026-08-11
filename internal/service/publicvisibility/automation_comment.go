package publicvisibility

// automation_comment.go — the automation surface's public-note entry point
// (#234). Unlike the portal path (tenant slug + visitor cookie), automation
// callers are already tenant-authenticated by API key and address requests
// by id. The comment still flows through the SAME moderation pipeline as
// portal comments: policy gate, default comment state, moderation subject,
// audit. Automation never bypasses review.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// automationSubjectDisplay labels automation-authored comments on the
// public portal.
const automationSubjectDisplay = "Team update"

// CreateAutomationRequestComment posts a public comment on a request as an
// automation actor (API key). Enforces the tenant's comment policy and
// routes the comment through moderation with the policy's default state.
func (s *Service) CreateAutomationRequestComment(
	ctx context.Context,
	tenantID string,
	requestID uuid.UUID,
	body, actorID string,
) error {
	const where = "service.publicvisibility.CreateAutomationRequestComment"
	body = strings.TrimSpace(body)
	if body == "" || tooLong(body, 5000) {
		return ErrValidation
	}
	policy, err := s.GetPolicy(ctx, tenantID)
	if err != nil {
		return err
	}
	if !publicCommentWriteEnabled(policy) {
		return ErrDisabled
	}

	subjectKey := "automation:" + actorID
	subjectHash := subjectkey.Hash(tenantID, subjectKey)

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	comment, err := s.repo.AddPublicRequestCommentTx(
		ctx, tx, tenantID, requestID,
		subjectKey, subjectHash, automationSubjectDisplay, body, actorID,
	)
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	subject, err := s.repo.CreateModerationSubjectTx(ctx, tx, repo.ModerationSubject{
		TenantID:               tenantID,
		Surface:                repo.SurfaceRequestComment,
		SubjectID:              comment.ID.String(),
		State:                  policy.DefaultCommentState,
		SubmittedByDisplay:     comment.SubmittedByDisplay,
		SubmittedByFingerprint: subjectHash,
	})
	if err != nil {
		return err
	}
	if s.audit != nil {
		if err := s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
			TenantID:   tenantID,
			Actor:      auditlogsvc.Actor{Type: "api_key", ID: actorID},
			Action:     "customer_request.add_comment",
			TargetType: "customer_request_comment",
			TargetID:   comment.ID.String(),
			Summary:    "Added public request comment via automation",
			After: map[string]any{
				"request_id":    requestID.String(),
				"moderation_id": subject.ID.String(),
				"state":         subject.State,
			},
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,request_id:%s,state:%s",
		where, tenantID, requestID, subject.State)
	return nil
}
