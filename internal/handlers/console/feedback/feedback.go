package feedback

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	"github.com/Phixsura/attune/internal/service/workflow"
)

type tagAssignmentReader interface {
	ListByFeedback(ctx context.Context, tenantID string, feedbackID int64) ([]feedbacktagassignment.TagInfo, error)
	ListByFeedbackBatch(ctx context.Context, tenantID string, feedbackIDs []int64) (map[int64][]feedbacktagassignment.TagInfo, error)
}

// FeedbackHandler serves /fb/v1/console/feedback. All queries scope to the
// session's tenant via FromContext — never taking tenant_id from query params.
//
// The handler also reads the tenant's EnrichConfig to translate the
// stable wire query (`?type=bug&severity=critical&labels=payment`)
// into the per-dim AttrFilter shape the repo's JSONB containment
// queries expect.
type workflowTransitioner interface {
	Transition(ctx context.Context, tenantID string, feedbackID int64,
		toStateID string, byUser string, comment string) (*workflow.TransitionResult, error)
	BatchTransition(ctx context.Context, tenantID string, feedbackIDs []int64,
		toStateID string, byUser string, comment string) (*workflow.BatchResult, error)
}

type auditReader interface {
	List(ctx context.Context, tenantID string, feedbackID int64, cursor string, limit int) ([]feedbackaudit.Entry, string, error)
}

type workflowStateReader interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error)
	ListTransitions(ctx context.Context, tenantID string) ([]workflowstate.Transition, error)
	AllowedNext(ctx context.Context, tenantID, fromID string) ([]workflowstate.WorkflowState, error)
}

type FeedbackHandler struct {
	repo           feedbackRepo
	tenants        tenantConfigRepo
	drafter        Drafter            // optional reply-draft generator; nil disables Regenerate
	regenLimiter   *ratelimit.Limiter // optional per-tenant rate limit on Regenerate; nil disables
	tagAssignments tagAssignmentReader
	workflow       workflowTransitioner
	replyWorkflow  replyDraftWorkflow
	auditReader    auditReader
	workflowStates workflowStateReader
	audit          auditRecorder // optional writer for retry-enrichment audit trail
	similarFinder  similarFeedbackFinder
	requestLinks   requestLinkReader
}

// Drafter regenerates a reply draft synchronously, sharing the worker's
// Generate core. Optional — nil disables the Regenerate endpoint. Precheck
// supplies the tenant-scoped guard inputs (ownership / enrichment status /
// opt-in / last-drafted time) so the handler enforces them — including the
// per-row cooldown — before spending an LLM call.
type Drafter interface {
	Precheck(ctx context.Context, feedbackID int64, tenantID string) (status string, enabled, found bool, lastGeneratedAt *time.Time, err error)
	Generate(ctx context.Context, feedbackID int64, tenantID string) (string, time.Time, error)
}

type replyDraftWorkflow interface {
	Snapshot(ctx context.Context, tenantID string, feedbackID int64) (replydraftsvc.Snapshot, error)
	Edit(ctx context.Context, tenantID string, feedbackID int64, content string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error)
	Approve(ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error)
	Reject(ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.Snapshot, error)
	Send(ctx context.Context, tenantID string, feedbackID int64, idempotencyKey string, expectedRevision int64, actor replydraftrepo.Actor) (replydraftsvc.SendResult, error)
	UpsertHook(ctx context.Context, tenantID string, name string, rawURL string, rawSecret string, enabled bool, actorID string) (replydraftsvc.HookConfig, error)
	GetHook(ctx context.Context, tenantID string) (replydraftsvc.HookConfig, error)
	DisableHook(ctx context.Context, tenantID, actorID string) (replydraftsvc.HookConfig, error)
	ListDeliveries(ctx context.Context, tenantID string, limit int) ([]replydraftrepo.DeliveryAttempt, error)
	DeliveryHealth(ctx context.Context, tenantID string) (replydraftrepo.DeliveryHealth, error)
	TestHook(ctx context.Context, tenantID string, idempotencyKey string, actor replydraftrepo.Actor) (replydraftsvc.HookTestResult, error)
	Redeliver(ctx context.Context, tenantID string, attemptID string, actor replydraftrepo.Actor) (replydraftrepo.DeliveryAttempt, error)
}

// SetDrafter wires the reply-draft generator used by Regenerate.
func (h *FeedbackHandler) SetDrafter(d Drafter) { h.drafter = d }

// SetReplyDraftWorkflow wires the review/edit/approve/send workflow.
func (h *FeedbackHandler) SetReplyDraftWorkflow(w replyDraftWorkflow) { h.replyWorkflow = w }

// SetRegenLimiter wires the per-tenant token-bucket that backstops the
// synchronous Regenerate endpoint: the per-row cooldown caps one row, this caps
// a tenant's total regenerations so a script can't spread unbounded LLM spend
// across many owned rows. nil leaves Regenerate unlimited.
func (h *FeedbackHandler) SetRegenLimiter(l *ratelimit.Limiter) { h.regenLimiter = l }

// SetTagAssignments wires the tag assignment reader used by List/Get to
// hydrate per-feedback tags. nil leaves tags empty.
func (h *FeedbackHandler) SetTagAssignments(r tagAssignmentReader) { h.tagAssignments = r }

// SetWorkflow wires the workflow transition service. nil disables transition endpoints.
func (h *FeedbackHandler) SetWorkflow(w workflowTransitioner) { h.workflow = w }

// SetAuditReader wires the audit log reader. nil returns empty audit lists.
func (h *FeedbackHandler) SetAuditReader(r auditReader) { h.auditReader = r }

// SetWorkflowStates wires the state reader for hydrating workflow state on list/detail.
func (h *FeedbackHandler) SetWorkflowStates(r workflowStateReader) { h.workflowStates = r }

// SetAuditLogger wires the audit writer for retry-enrichment audit trail.
func (h *FeedbackHandler) SetAuditLogger(audit auditRecorder) { h.audit = audit }

type feedbackRepo interface {
	ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error)
	GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error)
	UsageByDay(ctx context.Context, tenantID string, from, to time.Time) ([]feedback.UsageBucket, error)
	UrgentCount(ctx context.Context, tenantID string, from, to time.Time) (int64, error)
	TopValuesByDim(ctx context.Context, tenantID, dim string, multi bool, from, to time.Time, limit int) ([]feedback.ValueCount, error)
	TerminalFailureWorkbench(ctx context.Context, tenantID string, from, to time.Time) (*feedback.TerminalFailureWorkbench, error)
	RefreshClassificationQuality(ctx context.Context, opts feedback.ClassificationQualityRefreshOpts) error
	ClassificationQualityAggregates(ctx context.Context, opts feedback.ClassificationQualityQueryOpts) (feedback.ClassificationQualitySignalAggregate, []feedback.ClassificationQualityValueAggregate, error)
	ClassificationQualitySeries(ctx context.Context, opts feedback.ClassificationQualityQueryOpts) ([]feedback.ClassificationQualitySeriesBucket, error)
	ClassificationQualitySamples(ctx context.Context, tenantID string, ids []int64) ([]feedback.ClassificationQualitySample, error)
	RetryEnrichment(ctx context.Context, tenantID string, id int64) (*feedback.RetryResult, error)
}

type tenantConfigRepo interface {
	GetEnrichConfig(ctx context.Context, tenantID string) (tenant.EnrichConfig, error)
}

func NewFeedbackHandler(r *feedback.FeedbackRepo, t *tenant.TenantRepo) *FeedbackHandler {
	return ptrext.Of(FeedbackHandler{repo: r, tenants: t})
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return ptrext.Of(s)
}

// attrsToStruct decodes the raw JSONB attrs payload into a structpb
// for proto wire output. Empty / missing returns an empty Struct so
// the SPA can rely on the field always being present.
func attrsToStruct(raw []byte) *structpb.Struct {
	return jsonObjectStruct(raw)
}

func jsonObjectStruct(raw []byte) *structpb.Struct {
	m := map[string]any{}
	if len(raw) == 0 {
		s, _ := structpb.NewStruct(m)
		return s
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err == nil && decoded != nil {
		m = decoded
	}
	s, _ := structpb.NewStruct(m)
	return s
}

func toProtoFeedback(row feedback.ConsoleListRow) *attunev1.Feedback {
	f := ptrext.Of(attunev1.Feedback{
		Id:                                 row.ID,
		Content:                            row.Content,
		Source:                             row.Source,
		Type:                               row.Type,
		UserId:                             row.UserID,
		Language:                           nullableString(row.Language),
		PageUrl:                            row.PageURL,
		EnrichedTitle:                      nullableString(row.EnrichedTitle),
		EnrichedDisplayTitle:               nullableString(row.EnrichedDisplayTitle),
		EnrichedDisplayLocale:              nullableString(row.EnrichedDisplayLocale),
		EnrichedAttrs:                      attrsToStruct(row.EnrichedAttrs),
		IsUrgent:                           row.IsUrgent,
		ClassificationConfidence:           row.ClassificationConfidence,
		EnrichmentStatus:                   row.EnrichmentStatus,
		CreatedAt:                          row.CreatedAt.UTC().Format(time.RFC3339),
		EnrichmentAttempts:                 ptrext.Of(int32(row.EnrichmentAttempts)),
		EnrichmentFailureReasonClass:       nullableString(row.TerminalFailureReasonClass),
		EnrichmentFailureModel:             nullableString(row.TerminalFailureModel),
		EnrichmentFailureChannelId:         nullableString(row.TerminalFailureChannelID),
		EnrichmentFailureChannelName:       nullableString(row.TerminalFailureChannelName),
		EnrichmentFailureConfigFingerprint: nullableString(row.TerminalFailureConfigFingerprint),
		EnrichmentFailurePromptVersion:     nullableString(row.TerminalFailurePromptVersion),
	})
	if row.EnrichmentNextRetryAt != nil {
		f.EnrichmentNextRetryAt = ptrext.Of(row.EnrichmentNextRetryAt.UTC().Format(time.RFC3339))
	}
	return f
}

// extractAttrFilters walks the query string for keys matching a
// Dimension.Name in the tenant's set, building one AttrFilter per
// value. Each per-dim filter is AND-composed downstream; multiple
// values for the same dim become multiple filters (also AND-composed,
// effectively requiring all of them).
func extractAttrFilters(q map[string][]string, dims []domain.Dimension) []feedback.AttrFilter {
	if len(dims) == 0 || len(q) == 0 {
		return nil
	}
	dimIdx := make(map[string]domain.Dimension, len(dims))
	for _, d := range dims {
		dimIdx[d.Name] = d
	}
	var out []feedback.AttrFilter
	for k, vs := range q {
		d, ok := dimIdx[k]
		if !ok {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			out = append(out, feedback.AttrFilter{
				Dim:   d.Name,
				Value: v,
				Multi: d.Kind == domain.DimMulti,
			})
		}
	}
	return out
}

func tagInfoToProto(info feedbacktagassignment.TagInfo) *attunev1.Tag {
	return ptrext.Of(attunev1.Tag{
		Id:             info.TagID.String(),
		Name:           info.Name,
		Color:          info.Color,
		Description:    info.Description,
		ExclusiveScope: info.ExclusiveScope,
		UsageCount:     int32(info.UsageCount),
		Archived:       info.Archived,
		CreatedBy:      info.CreatedBy,
		CreatedAt:      info.TagCreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      info.TagUpdatedAt.UTC().Format(time.RFC3339),
	})
}

func attrFiltersFromProto(filters []*attunev1.AttrFilter, dims []domain.Dimension) []feedback.AttrFilter {
	if len(filters) == 0 {
		return nil
	}
	q := make(map[string][]string, len(filters))
	for _, f := range filters {
		q[f.GetDim()] = append(q[f.GetDim()], f.GetValue())
	}
	return extractAttrFilters(q, dims)
}
