package console

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo"
	"github.com/Phixsura/attune/internal/service"
)

// APIKeysHandler serves /fb/v1/console/api-keys. All three operations
// scope to the session's tenant — see auth.RequireSession middleware
// which writes TenantID to context before this handler runs.
type APIKeysHandler struct {
	svc *service.APIKeys
}

func NewAPIKeysHandler(svc *service.APIKeys) *APIKeysHandler {
	return &APIKeysHandler{svc: svc}
}

// keyDTO mirrors openapi.yaml `ApiKey`. Optional time fields use *string
// so JSON renders null when absent (matches the `nullable: true` schema).
type keyDTO struct {
	ID         string  `json:"id"`
	KeyPrefix  string  `json:"key_prefix"`
	Label      string  `json:"label"`
	IsActive   bool    `json:"is_active"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func toDTO(row repo.APIKeyListRow) keyDTO {
	dto := keyDTO{
		ID:        row.ID.String(),
		KeyPrefix: row.KeyPrefix,
		Label:     row.Label,
		IsActive:  row.IsActive,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}
	if row.LastUsedAt != nil {
		s := row.LastUsedAt.UTC().Format(time.RFC3339)
		dto.LastUsedAt = &s
	}
	if row.RevokedAt != nil {
		s := row.RevokedAt.UTC().Format(time.RFC3339)
		dto.RevokedAt = &s
	}
	return dto
}

// List handles GET /fb/v1/console/api-keys.
func (h *APIKeysHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.List"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.svc.List(r.Context(), auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "api-keys list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.List failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "查询 API key 失败")
		return
	}
	items := make([]keyDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDTO(row))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// createRequest is the POST body. Label is required + length-bounded so
// a misbehaving client can't store huge strings.
type createRequest struct {
	Label string `json:"label"`
}

// newKeyResponse is the 201 body, matching openapi.yaml `NewApiKey`.
type newKeyResponse struct {
	keyDTO
	Secret string `json:"secret"`
}

// Create handles POST /fb/v1/console/api-keys.
// Returns 201 with the raw key once. Subsequent List calls only show
// the prefix — the secret is unrecoverable.
func (h *APIKeysHandler) Create(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.Create"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		logext.Warnf(ctx, "[%s] reject: missing label,tenant_id:%s", where, auth.TenantID)
		writeError(w, http.StatusBadRequest, "missing_label", "label 不能为空")
		return
	}
	if len(req.Label) > 200 {
		logext.Warnf(ctx, "[%s] reject: label too long,tenant_id:%s,len:%d",
			where, auth.TenantID, len(req.Label))
		writeError(w, http.StatusBadRequest, "label_too_long", "label 不能超过 200 字符")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s", where, auth.TenantID, req.Label)

	raw, id, err := h.svc.Issue(r.Context(), auth.TenantID, req.Label)
	if err != nil {
		slog.ErrorContext(ctx, "api-keys issue", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.Issue failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "签发 API key 失败")
		return
	}

	// Build response — re-read the row so we return canonical timestamps.
	// Cheap: O(N) list with N tiny for early customers.
	rows, err := h.svc.List(r.Context(), auth.TenantID)
	if err != nil {
		slog.WarnContext(ctx, "api-keys post-issue list", "err", err)
		logext.Warnf(ctx, "[%s] post-issue list failed,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
	}
	var newRow repo.APIKeyListRow
	for _, row := range rows {
		if row.ID == id {
			newRow = row
			break
		}
	}

	resp := newKeyResponse{keyDTO: toDTO(newRow), Secret: raw}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
}

// Revoke handles DELETE /fb/v1/console/api-keys/{id}.
// 204 on success, 404 if id doesn't exist for this tenant.
func (h *APIKeysHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	const where = "console.APIKeysHandler.Revoke"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s,id_str:%s",
			where, auth.TenantID, idStr)
		writeError(w, http.StatusBadRequest, "bad_id", "id 不是 UUID")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
	if err := h.svc.Revoke(r.Context(), auth.TenantID, id); err != nil {
		if errors.Is(err, repo.ErrAPIKeyNotFound) {
			logext.Warnf(ctx, "[%s] reject: not found,tenant_id:%s,key_id:%s",
				where, auth.TenantID, id)
			writeError(w, http.StatusNotFound, "not_found", "API key 不存在或不属于当前 tenant")
			return
		}
		slog.ErrorContext(ctx, "api-keys revoke", "err", err, "id", id, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] svc.Revoke failed,tenant_id:%s,key_id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "撤销失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s", where, auth.TenantID, id)
}
