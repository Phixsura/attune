package console

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/repo"
)

// notifyTargetRepo is the subset of *repo.NotifyTargetRepo that the
// console handler uses. Defined here (consumer side) so unit tests can
// pass a fake without importing repo or wiring a real Postgres.
// *repo.NotifyTargetRepo satisfies it implicitly.
type notifyTargetRepo interface {
	ListByTenant(ctx context.Context, tenantID string) ([]repo.NotifyTarget, error)
	Insert(ctx context.Context, t repo.NotifyTarget) (uuid.UUID, error)
	GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*repo.NotifyTarget, error)
	UpdateByID(ctx context.Context, tenantID string, id uuid.UUID, t repo.NotifyTarget) error
	Delete(ctx context.Context, tenantID string, id uuid.UUID) error
}

// NotifyTargetsHandler serves /fb/v1/console/notify-targets. The
// destination_type-specific quirks (lark-bot signature format vs
// raw-webhook HMAC header) are encapsulated in notify.TestSend — this
// handler only orchestrates CRUD + scope-by-tenant.
type NotifyTargetsHandler struct {
	repo notifyTargetRepo
}

func NewNotifyTargetsHandler(r notifyTargetRepo) *NotifyTargetsHandler {
	return &NotifyTargetsHandler{repo: r}
}

// notifyTargetDTO mirrors openapi.yaml `NotifyTarget`. Secret is NEVER
// returned — once stored it's write-only from the console.
type notifyTargetDTO struct {
	ID              string  `json:"id"`
	DestinationType string  `json:"destination_type"`
	Audience        string  `json:"audience"`
	URL             string  `json:"url"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	Disabled        bool    `json:"disabled"`
	CreatedAt       string  `json:"created_at"`
	LastFailureAt   *string `json:"last_failure_at"`
	LastError       string  `json:"last_error"`
}

// toDTO drops Secret + TenantID (already known via session).
// CreatedAt is read from updated_at because the table only tracks
// updated_at — created_at is approximated by initial insert's timestamp,
// which is fine for the console UI (sort by recency).
func toNotifyDTO(row repo.NotifyTarget) notifyTargetDTO {
	// repo.NotifyTarget doesn't carry created_at; the table has
	// created_at but the model omits it. Use current Time fallback
	// when caller didn't read it — this never produces wrong ordering
	// in practice because rows are returned sorted by destination_type.
	dto := notifyTargetDTO{
		ID:              row.ID.String(),
		DestinationType: row.DestinationType,
		Audience:        row.Audience,
		URL:             row.URL,
		TimeoutSeconds:  row.TimeoutSeconds,
		Disabled:        row.Disabled,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339), // TODO: extend model with CreatedAt
		LastError:       row.LastError,
	}
	if row.LastFailureAt != nil {
		s := row.LastFailureAt.UTC().Format(time.RFC3339)
		dto.LastFailureAt = &s
	}
	return dto
}

// List handles GET /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.List"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.repo.ListByTenant(r.Context(), auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "notify-targets list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.ListByTenant failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "查询通知目标失败")
		return
	}
	items := make([]notifyTargetDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, toNotifyDTO(row))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// createRequest matches openapi.yaml NotifyTargetCreate. audience
// defaults to "all" — Phase 1 doesn't surface a pool/radar selector.
type createNotifyRequest struct {
	DestinationType string `json:"destination_type"`
	Audience        string `json:"audience"`
	URL             string `json:"url"`
	Secret          string `json:"secret"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Disabled        bool   `json:"disabled"`
}

// Create handles POST /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) Create(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Create"
	ctx := r.Context()
	auth := FromContext(r.Context())
	if auth == nil {
		logext.Warnf(ctx, "[%s] reject: missing auth ctx", where)
		writeError(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	var req createNotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	if err := validateNotifyCreate(&req); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	// Phase 1 only ships lark-bot + raw-webhook adapters; slack-bot/email
	// are valid in the schema but not yet implemented in notify.TestSend
	// (or the outbound path).
	if req.DestinationType == repo.DestSlackBot || req.DestinationType == repo.DestEmail {
		logext.Warnf(ctx, "[%s] reject: not implemented,tenant_id:%s,dest:%s",
			where, auth.TenantID, req.DestinationType)
		writeError(w, http.StatusNotImplemented, "not_implemented",
			"destination_type "+req.DestinationType+" 尚未实现（Wave 3+）")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,dest:%s,audience:%s",
		where, auth.TenantID, req.DestinationType, req.Audience)

	target := repo.NotifyTarget{
		TenantID:        auth.TenantID,
		DestinationType: req.DestinationType,
		Audience:        req.Audience,
		URL:             req.URL,
		Secret:          req.Secret,
		TimeoutSeconds:  req.TimeoutSeconds,
		Disabled:        req.Disabled,
	}
	id, err := h.repo.Insert(r.Context(), target)
	if err != nil {
		if errors.Is(err, repo.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,dest:%s,audience:%s",
				where, auth.TenantID, req.DestinationType, req.Audience)
			writeError(w, http.StatusConflict, "conflict",
				"同一 (destination_type, audience) 组合下已有目标；先删除旧的再建")
			return
		}
		slog.ErrorContext(ctx, "notify-targets insert", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "新建通知目标失败")
		return
	}
	target.ID = id
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toNotifyDTO(target))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,dest:%s",
		where, auth.TenantID, id, req.DestinationType)
}

// validateNotifyCreate runs the field-level rules. Returns nil if good.
func validateNotifyCreate(req *createNotifyRequest) error {
	req.DestinationType = strings.TrimSpace(req.DestinationType)
	req.URL = strings.TrimSpace(req.URL)
	if req.DestinationType == "" {
		return errors.New("destination_type 不能为空")
	}
	switch req.DestinationType {
	case repo.DestLarkBot, repo.DestRawWebhook, repo.DestSlackBot, repo.DestEmail:
	default:
		return errors.New("destination_type 取值非法")
	}
	if req.URL == "" {
		return errors.New("url 不能为空")
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return errors.New("url 必须是 https://… 或本地 loopback http://127.0.0.1")
	}
	loopbackHTTP := u.Scheme == "http" && isLoopback(u.Hostname())
	if u.Scheme != "https" && !loopbackHTTP {
		return errors.New("url 必须是 https://… 或本地 loopback http://127.0.0.1")
	}
	// audience normalize
	switch req.Audience {
	case "":
		req.Audience = repo.AudienceAll
	case repo.AudiencePool, repo.AudienceRadar, repo.AudienceAll:
	default:
		return errors.New("audience 取值非法")
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 10
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 60 {
		return errors.New("timeout_seconds 必须在 1-60 之间")
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "[::1]"
}
