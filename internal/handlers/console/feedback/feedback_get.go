package feedback

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// Get handles GET /fb/v1/console/feedback/{id}.
func (h *FeedbackHandler) Get(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetFeedbackRequest) (dispatcher.Result[*attunev1.FeedbackDetail], error) {
	const where = "console.FeedbackHandler.Get"
	auth := ctx.Auth
	id := req.GetId()
	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%d", where, auth.TenantID, id)
	row, err := h.repo.GetForConsole(ctx, auth.TenantID, id)
	if err != nil {
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,id:%d",
				where, auth.TenantID, id)
			return dispatcher.Fail[*attunev1.FeedbackDetail](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "feedback not found or not owned by tenant")
		}
		logext.Errorf(ctx, "[%s] feedback.GetForConsole failed,tenant_id:%s,id:%d,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.FeedbackDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load feedback detail")
	}

	f := toProtoFeedback(row.ConsoleListRow)
	detail := ptrext.Of(attunev1.FeedbackDetail{
		Id:                       f.GetId(),
		Content:                  f.GetContent(),
		Source:                   f.GetSource(),
		Type:                     f.GetType(),
		UserId:                   f.GetUserId(),
		Language:                 f.Language,
		PageUrl:                  f.GetPageUrl(),
		EnrichedTitle:            f.EnrichedTitle,
		EnrichedDisplayTitle:     f.EnrichedDisplayTitle,
		EnrichedDisplayLocale:    f.EnrichedDisplayLocale,
		EnrichedAttrs:            f.GetEnrichedAttrs(),
		IsUrgent:                 f.GetIsUrgent(),
		ClassificationConfidence: f.ClassificationConfidence,
		EnrichmentStatus:         f.GetEnrichmentStatus(),
		CreatedAt:                f.GetCreatedAt(),
		EnrichmentError:          nullableString(row.EnrichmentError),
		EnrichedRationale:        nullableString(row.EnrichedRationale),
		EnrichedDisplayRationale: nullableString(row.EnrichedDisplayRationale),
		ReplyDraft:               nullableString(row.ReplyDraft),
		ReplyDraftEnabled:        row.ReplyDraftEnabled,
		EnrichmentAttempts:       f.EnrichmentAttempts,
		EnrichmentNextRetryAt:    f.EnrichmentNextRetryAt,
	})
	if len(row.SourceMeta) > 0 {
		var m map[string]any
		if json.Unmarshal(row.SourceMeta, &m) == nil {
			if s, err := structpb.NewStruct(m); err == nil {
				detail.SourceMeta = s
			}
		}
	}
	if len(row.Attachments) > 0 {
		var atts []struct {
			URL  string  `json:"url"`
			Mime *string `json:"mime"`
			Size *int64  `json:"size"`
		}
		if json.Unmarshal(row.Attachments, &atts) == nil {
			for _, a := range atts {
				detail.Attachments = append(detail.Attachments, ptrext.Of(attunev1.Attachment{
					Url:  a.URL,
					Mime: a.Mime,
					Size: a.Size,
				}))
			}
		}
	}
	if row.EnrichedAt != nil {
		detail.EnrichedAt = ptrext.Of(row.EnrichedAt.UTC().Format(time.RFC3339))
	}
	if row.ReplyDraftGeneratedAt != nil {
		detail.ReplyDraftGeneratedAt = ptrext.Of(row.ReplyDraftGeneratedAt.UTC().Format(time.RFC3339))
	}
	if h.tagAssignments != nil {
		tags, err := h.tagAssignments.ListByFeedback(ctx, auth.TenantID, id)
		if err != nil {
			logext.Warnf(ctx, "[%s] tag load failed,tenant_id:%s,id:%d,err:%+v",
				where, auth.TenantID, id, err.Error())
		} else {
			for _, info := range tags {
				detail.Tags = append(detail.Tags, tagInfoToProto(info))
			}
		}
	}
	h.enrichDetailWithWorkflowState(ctx, where, auth.TenantID, row.WorkflowStateID, detail)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d", where, auth.TenantID, id)
	return dispatcher.OK(detail)
}
