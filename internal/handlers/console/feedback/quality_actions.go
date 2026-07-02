// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
)

const (
	qualityActionMaxEvidenceBytes = 4096
	qualityActionMaxLimit         = 200
)

var qualityActionKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,119}$`)

type qualityActionStore interface {
	ListQualityActions(ctx context.Context, opts repofeedback.QualityActionListOpts) ([]repofeedback.QualityAction, error)
	UpsertQualityActionStatus(ctx context.Context, in repofeedback.QualityActionUpsert) (*repofeedback.QualityAction, error)
}

type QualityActionHandler struct {
	store qualityActionStore
}

func NewQualityActionHandler(store qualityActionStore) *QualityActionHandler {
	return ptrext.Of(QualityActionHandler{store: store})
}

func BindListQualityActionsRequest(r *http.Request, req *attunev1.ListQualityActionsRequest) error {
	q := r.URL.Query()
	req.Status = q.Get("status")
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "limit must be an integer")
		}
		req.Limit = ptrext.Of(int32(v))
	}
	return nil
}

func (h *QualityActionHandler) ListQualityActions(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListQualityActionsRequest,
) (dispatcher.Result[*attunev1.ListQualityActionsResponse], error) {
	const where = "console.QualityActionHandler.ListQualityActions"
	if h.store == nil {
		return dispatcher.Fail[*attunev1.ListQualityActionsResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"quality actions are not configured",
		)
	}
	status, err := normalizeQualityActionStatusFilter(req.GetStatus())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListQualityActionsResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			err.Error(),
		)
	}
	limit, err := qualityActionLimit(req.Limit)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListQualityActionsResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			err.Error(),
		)
	}
	rows, err := h.store.ListQualityActions(ctx, repofeedback.QualityActionListOpts{
		TenantID: ctx.Auth.TenantID,
		Status:   status,
		Limit:    limit,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListQualityActionsResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to read quality actions",
		)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListQualityActionsResponse{
		Actions: qualityActionsToProto(rows),
	}))
}

func (h *QualityActionHandler) UpdateQualityAction(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateQualityActionRequest,
) (dispatcher.Result[*attunev1.UpdateQualityActionResponse], error) {
	const where = "console.QualityActionHandler.UpdateQualityAction"
	if h.store == nil {
		return dispatcher.Fail[*attunev1.UpdateQualityActionResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"quality actions are not configured",
		)
	}
	row, err := qualityActionUpdateFromRequest(ctx.Auth, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.UpdateQualityActionResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			err.Error(),
		)
	}
	action, err := h.store.UpsertQualityActionStatus(ctx, row)
	if err != nil {
		logext.Errorf(ctx, "[%s] upsert failed,tenant_id:%s,action_key:%s,err:%+v",
			where, ctx.Auth.TenantID, row.ActionKey, err.Error())
		return dispatcher.Fail[*attunev1.UpdateQualityActionResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"failed to update quality action",
		)
	}
	return dispatcher.OK(ptrext.Of(attunev1.UpdateQualityActionResponse{
		Action: qualityActionToProto(ptrext.Indirect(action)),
	}))
}

func qualityActionUpdateFromRequest(auth *session.AuthCtx, req *attunev1.UpdateQualityActionRequest) (repofeedback.QualityActionUpsert, error) {
	key := strings.TrimSpace(req.GetActionKey())
	if !qualityActionKeyRe.MatchString(key) {
		return repofeedback.QualityActionUpsert{}, errors.New("action_key must be 1-120 chars using lowercase letters, numbers, dot, colon, underscore, or dash")
	}
	signal := strings.TrimSpace(req.GetSignal())
	if signal == "" || len(signal) > 80 {
		return repofeedback.QualityActionUpsert{}, errors.New("signal is required and must be at most 80 characters")
	}
	status, err := normalizeQualityActionStatus(req.GetStatus())
	if err != nil {
		return repofeedback.QualityActionUpsert{}, err
	}
	severity, err := normalizeQualityActionSeverity(req.GetSeverity())
	if err != nil {
		return repofeedback.QualityActionUpsert{}, err
	}
	targetPath := strings.TrimSpace(req.GetTargetPath())
	if targetPath != "" && !strings.HasPrefix(targetPath, "/") {
		return repofeedback.QualityActionUpsert{}, errors.New("target_path must be an absolute Console path")
	}
	evidenceJSON, err := normalizeQualityActionEvidence(req.GetEvidenceJson())
	if err != nil {
		return repofeedback.QualityActionUpsert{}, err
	}
	return repofeedback.QualityActionUpsert{
		TenantID:          auth.TenantID,
		ActionKey:         key,
		Signal:            truncateSearchLabel(signal, 80),
		Status:            status,
		Severity:          severity,
		TargetPath:        truncateSearchLabel(targetPath, 240),
		MetricLabel:       truncateSearchLabel(req.GetMetricLabel(), 120),
		MetricValue:       truncateSearchLabel(req.GetMetricValue(), 120),
		RecommendationKey: truncateSearchLabel(req.GetRecommendationKey(), 160),
		EvidenceJSON:      evidenceJSON,
		ActorUserID:       auth.UserID,
	}, nil
}

func normalizeQualityActionStatusFilter(raw string) (string, error) {
	status := strings.TrimSpace(strings.ToLower(raw))
	if status == "" || status == "all" {
		return "", nil
	}
	if isQualityActionStatus(status) {
		return status, nil
	}
	return "", errors.New("status must be open, acknowledged, resolved, dismissed, or all")
}

func normalizeQualityActionStatus(raw string) (string, error) {
	status := strings.TrimSpace(strings.ToLower(raw))
	if status == "" {
		return repofeedback.QualityActionStatusOpen, nil
	}
	if isQualityActionStatus(status) {
		return status, nil
	}
	return "", errors.New("status must be open, acknowledged, resolved, or dismissed")
}

func isQualityActionStatus(status string) bool {
	switch status {
	case repofeedback.QualityActionStatusOpen,
		repofeedback.QualityActionStatusAcknowledged,
		repofeedback.QualityActionStatusResolved,
		repofeedback.QualityActionStatusDismissed:
		return true
	default:
		return false
	}
}

func normalizeQualityActionSeverity(raw string) (string, error) {
	severity := strings.TrimSpace(strings.ToLower(raw))
	if severity == "" {
		return repofeedback.QualityActionSeverityWatch, nil
	}
	switch severity {
	case repofeedback.QualityActionSeverityAlert,
		repofeedback.QualityActionSeverityWatch,
		repofeedback.QualityActionSeverityNormal,
		repofeedback.QualityActionSeverityInsufficientData:
		return severity, nil
	default:
		return "", errors.New("severity must be alert, watch, normal, or insufficient_data")
	}
}

func normalizeQualityActionEvidence(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}", nil
	}
	if len(raw) > qualityActionMaxEvidenceBytes {
		return "", errors.New("evidence_json must be at most 4096 bytes")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil { // ptrext:allow json decode target
		return "", errors.New("evidence_json must be a JSON object")
	}
	if payload == nil {
		return "{}", nil
	}
	return raw, nil
}

func qualityActionLimit(limit *int32) (int, error) {
	if limit == nil {
		return 50, nil
	}
	v := int(ptrext.Indirect(limit))
	if v <= 0 || v > qualityActionMaxLimit {
		return 0, errors.New("limit must be between 1 and 200")
	}
	return v, nil
}

func qualityActionsToProto(rows []repofeedback.QualityAction) []*attunev1.QualityAction {
	out := make([]*attunev1.QualityAction, 0, len(rows))
	for _, row := range rows {
		out = append(out, qualityActionToProto(row))
	}
	return out
}

func qualityActionToProto(row repofeedback.QualityAction) *attunev1.QualityAction {
	return ptrext.Of(attunev1.QualityAction{
		ActionId:          row.ID,
		ActionKey:         row.ActionKey,
		Signal:            row.Signal,
		Status:            row.Status,
		Severity:          row.Severity,
		TargetPath:        row.TargetPath,
		MetricLabel:       row.MetricLabel,
		MetricValue:       row.MetricValue,
		RecommendationKey: row.RecommendationKey,
		EvidenceJson:      row.EvidenceJSON,
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339),
		LastSeenAt:        row.LastSeenAt.UTC().Format(time.RFC3339),
		AcknowledgedAt:    optionalTime(row.AcknowledgedAt),
		ResolvedAt:        optionalTime(row.ResolvedAt),
		DismissedAt:       optionalTime(row.DismissedAt),
		UpdatedAt:         row.UpdatedAt.UTC().Format(time.RFC3339),
		UpdatedBy:         row.UpdatedBy,
	})
}

func optionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
