package feedback

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
)

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func (h *BatchHandler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func (h *BatchHandler) recordDeleteAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *feedbackbatch.BatchRequest,
	resp *feedbackbatch.BatchResponse,
) error {
	if h.audit == nil || req == nil || resp == nil || req.DryRun || resp.FromCache {
		return nil
	}
	if req.Operation == nil || req.Operation.GetDelete() == nil {
		return nil
	}
	summary := "Deleted feedback in batch"
	if resp.JobID != "" {
		summary = "Queued batch feedback delete"
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	targetID := "query"
	if resp.JobID != "" {
		targetID = resp.JobID
	} else if len(req.FeedbackIDs) > 0 {
		targetID = fmt.Sprintf("%d:%d", req.FeedbackIDs[0], len(req.FeedbackIDs))
	}
	before := map[string]any{
		"selection_mode":          batchSelectionMode(req),
		"feedback_id_count":       len(req.FeedbackIDs),
		"hard_delete":             req.Operation.GetDelete().GetHard(),
		"if_unmodified_since_set": req.IfUnmodifiedSince != nil,
		"idempotency_key_present": req.IdempotencyKey != "",
	}
	if len(req.FeedbackIDs) > 0 && len(req.FeedbackIDs) <= 100 {
		before["feedback_ids"] = req.FeedbackIDs
	}
	if query := feedbackFilterSnapshot(req.Filter); query != nil {
		before["query"] = query
	}
	after := map[string]any{
		"async":         resp.JobID != "",
		"job_id":        resp.JobID,
		"total_matched": resp.TotalMatched,
		"succeeded":     resp.Succeeded,
		"skipped":       resp.Skipped,
		"failed":        len(resp.Failed),
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     "feedback.batch_delete",
		TargetType: "feedback_batch",
		TargetID:   targetID,
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func batchSelectionMode(req *feedbackbatch.BatchRequest) string {
	if len(req.FeedbackIDs) > 0 {
		return "ids"
	}
	return "query"
}

func feedbackFilterSnapshot(filter *attunev1.FeedbackFilter) map[string]any {
	if filter == nil {
		return nil
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(filter)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if len(out) == 0 {
		return nil
	}
	return out
}
