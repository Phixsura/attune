// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditLogService interface {
	List(ctx context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error)
}

type Handler struct {
	svc auditLogService
}

func NewHandler(svc auditLogService) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func BindListRequest(r *http.Request, req *attunev1.ListAuditLogRequest) error {
	q := r.URL.Query()
	req.Actions = collectQueryValues(q, "actions", "action")
	if len(req.Actions) == 1 {
		req.Action = req.Actions[0]
	}
	req.ActorType = firstQueryValue(q, "actor_type", "actorType")
	req.ActorId = firstQueryValue(q, "actor_id", "actorId")
	req.TargetType = firstQueryValue(q, "target_type", "targetType")
	req.TargetId = firstQueryValue(q, "target_id", "targetId")
	req.From = strings.TrimSpace(q.Get("from"))
	req.To = strings.TrimSpace(q.Get("to"))
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "limit must be an integer")
		}
		req.Limit = int32(limit)
	}
	if cursor := strings.TrimSpace(q.Get("cursor")); cursor != "" {
		req.Cursor = ptrext.Of(cursor)
	}
	return nil
}

func (h *Handler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListAuditLogRequest) (dispatcher.Result[*attunev1.ListAuditLogResponse], error) {
	const where = "console.auditlog.List"
	filter, err := buildFilter(ctx.Auth.TenantID, req)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListAuditLogResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,action:%s,target_type:%s",
		where, ctx.Auth.TenantID, strings.Join(filter.Actions, ","), filter.TargetType)
	result, err := h.svc.List(ctx, filter)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListAuditLogResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list audit log")
	}
	items := make([]*attunev1.AuditLogEntry, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, toProto(row))
	}
	resp := ptrext.Of(attunev1.ListAuditLogResponse{Items: items})
	if result.NextCursor != "" {
		resp.NextCursor = ptrext.Of(result.NextCursor)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	const where = "console.auditlog.ExportCSV"
	auth := session.FromContext(r.Context())
	req := ptrext.Of(attunev1.ListAuditLogRequest{})
	if err := BindListRequest(r, req); err != nil {
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		return
	}
	filter, err := buildFilter(auth.TenantID, req)
	if err != nil {
		dispatcher.Reject(r.Context(), w, http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		return
	}
	filter.Cursor = ""
	filter.Unbounded = true
	result, err := h.svc.List(r.Context(), filter)
	if err != nil {
		logext.Errorf(r.Context(), "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		dispatcher.Reject(r.Context(), w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to export audit log")
		return
	}
	filename := fmt.Sprintf("audit-log-%s.csv", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{
		"id", "created_at", "actor_type", "actor_id", "actor_email", "actor_ip", "actor_user_agent", "action",
		"target_type", "target_id", "summary", "before_json", "after_json",
	}); err != nil {
		logext.Errorf(r.Context(), "[%s] csv header write failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return
	}
	for _, row := range result.Items {
		if err := writer.Write([]string{
			strconv.FormatInt(row.ID, 10),
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.ActorType,
			row.ActorID,
			row.ActorEmail,
			row.ActorIP,
			row.ActorUserAgent,
			row.Action,
			row.TargetType,
			row.TargetID,
			row.Summary,
			string(row.BeforeJSON),
			string(row.AfterJSON),
		}); err != nil {
			logext.Warnf(r.Context(), "[%s] csv row write failed,tenant_id:%s,row_id:%d,err:%+v",
				where, auth.TenantID, row.ID, err.Error())
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		logext.Warnf(r.Context(), "[%s] csv flush failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
	}
}

func buildFilter(tenantID string, req *attunev1.ListAuditLogRequest) (auditlogsvc.ListFilter, error) {
	filter := auditlogsvc.ListFilter{
		TenantID:   tenantID,
		Actions:    combinedActions(req),
		ActorType:  req.GetActorType(),
		ActorID:    req.GetActorId(),
		TargetType: req.GetTargetType(),
		TargetID:   req.GetTargetId(),
		Limit:      int(req.GetLimit()),
		Cursor:     req.GetCursor(),
	}
	if req.GetFrom() != "" {
		from, err := time.Parse(time.RFC3339, req.GetFrom())
		if err != nil {
			return auditlogsvc.ListFilter{}, fmt.Errorf("from must be RFC3339")
		}
		filter.From = ptrext.Of(from)
	}
	if req.GetTo() != "" {
		to, err := time.Parse(time.RFC3339, req.GetTo())
		if err != nil {
			return auditlogsvc.ListFilter{}, fmt.Errorf("to must be RFC3339")
		}
		filter.To = ptrext.Of(to)
	}
	if strings.TrimSpace(filter.Cursor) != "" {
		if _, _, err := auditlogrepo.ParseCursor(filter.Cursor); err != nil {
			return auditlogsvc.ListFilter{}, fmt.Errorf("cursor must be audit pagination token")
		}
	}
	return filter, nil
}

func firstQueryValue(q url.Values, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(q.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func collectQueryValues(q url.Values, keys ...string) []string {
	var values []string
	seen := make(map[string]struct{})
	for _, key := range keys {
		for _, raw := range q[key] {
			for _, part := range strings.Split(raw, ",") {
				trimmed := strings.TrimSpace(part)
				if trimmed == "" {
					continue
				}
				if _, ok := seen[trimmed]; ok {
					continue
				}
				seen[trimmed] = struct{}{}
				values = append(values, trimmed)
			}
		}
	}
	return values
}

func combinedActions(req *attunev1.ListAuditLogRequest) []string {
	actions := make([]string, 0, len(req.GetActions())+1)
	seen := map[string]struct{}{}
	for _, action := range append(append([]string{}, req.GetActions()...), req.GetAction()) {
		trimmed := strings.TrimSpace(action)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		actions = append(actions, trimmed)
	}
	return actions
}

func toProto(row auditlogrepo.Entry) *attunev1.AuditLogEntry {
	return ptrext.Of(attunev1.AuditLogEntry{
		Id:             row.ID,
		ActorType:      row.ActorType,
		ActorId:        row.ActorID,
		ActorEmail:     row.ActorEmail,
		ActorIp:        row.ActorIP,
		ActorUserAgent: row.ActorUserAgent,
		Action:         row.Action,
		TargetType:     row.TargetType,
		TargetId:       row.TargetID,
		Summary:        row.Summary,
		BeforeJson:     string(row.BeforeJSON),
		AfterJson:      string(row.AfterJSON),
		CreatedAt:      row.CreatedAt.UTC().Format(time.RFC3339),
	})
}
