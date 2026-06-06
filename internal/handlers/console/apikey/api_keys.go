package apikey

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/service/apikey"
)

// APIKeysHandler serves /fb/v1/console/api-keys. All three operations
// scope to the session's tenant — see auth.RequireSession middleware
// which writes TenantID to context before this handler runs.
type APIKeysHandler struct {
	svc *apikey.APIKeys
}

func NewAPIKeysHandler(svc *apikey.APIKeys) *APIKeysHandler {
	return &APIKeysHandler{svc: svc}
}

func toProtoAPIKey(row apikeyrepo.APIKeyListRow) *attunev1.ApiKey {
	k := &attunev1.ApiKey{
		Id:        row.ID.String(),
		KeyPrefix: row.KeyPrefix,
		Label:     row.Label,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if row.LastUsedAt != nil {
		s := row.LastUsedAt.UTC().Format(time.RFC3339)
		k.LastUsedAt = &s
	}
	if row.RevokedAt != nil {
		s := row.RevokedAt.UTC().Format(time.RFC3339)
		k.RevokedAt = &s
	}
	return k
}

// List handles GET /fb/v1/console/api-keys.
func (h *APIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.List"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "api-keys list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.List failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "查询 API key 失败")
		return
	}
	items := make([]*attunev1.ApiKey, 0, len(rows))
	for _, row := range rows {
		items = append(items, toProtoAPIKey(row))
	}
	respond.Proto(w, http.StatusOK, &attunev1.ListApiKeysResponse{Items: items})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// Create handles POST /fb/v1/console/api-keys.
// Returns 201 with the raw key once. Subsequent List calls only show
// the prefix — the secret is unrecoverable.
func (h *APIKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.Create"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	var req attunev1.CreateApiKeyRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	label := strings.TrimSpace(req.GetLabel())
	if label == "" {
		logext.Warnf(ctx, "[%s] reject: missing label,tenant_id:%s", where, auth.TenantID)
		respond.Error(ctx, w, http.StatusBadRequest, "missing_label", "label 不能为空")
		return
	}
	if len(label) > 200 {
		logext.Warnf(ctx, "[%s] reject: label too long,tenant_id:%s,len:%d",
			where, auth.TenantID, len(label))
		respond.Error(ctx, w, http.StatusBadRequest, "label_too_long", "label 不能超过 200 字符")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s", where, auth.TenantID, label)

	raw, id, err := h.svc.Issue(ctx, auth.TenantID, label)
	if err != nil {
		slog.ErrorContext(ctx, "api-keys issue", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.Issue failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "签发 API key 失败")
		return
	}

	// Re-read the row so we return canonical timestamps. Cheap: N is tiny.
	rows, err := h.svc.List(ctx, auth.TenantID)
	if err != nil {
		slog.WarnContext(ctx, "api-keys post-issue list", "err", err)
		logext.Warnf(ctx, "[%s] post-issue list failed,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
	}
	var newRow apikeyrepo.APIKeyListRow
	for _, row := range rows {
		if row.ID == id {
			newRow = row
			break
		}
	}

	respond.Proto(w, http.StatusCreated, &attunev1.CreateApiKeyResponse{
		Key:    toProtoAPIKey(newRow),
		Secret: raw,
	})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
}

// Revoke handles DELETE /fb/v1/console/api-keys/{id}.
// 204 on success, 404 if id doesn't exist for this tenant.
func (h *APIKeysHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.Revoke"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		respond.Error(ctx, w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	if err := h.svc.Revoke(ctx, auth.TenantID, id); err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,key_id:%s",
				where, auth.TenantID, id)
			respond.Error(ctx, w, http.StatusNotFound, "not_found", "API key 不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "api-keys revoke", "err", err, "id", id, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.Revoke failed,tenant_id:%s,key_id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "撤销失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
}
