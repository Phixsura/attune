package notifytarget

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/handlers/console/internal/respond"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// notifyTargetRepo is the subset of *notifytarget.NotifyTargetRepo that the console
// handler uses. Defined here (consumer side) so unit tests can pass a fake.
type notifyTargetRepo interface {
	ListByTenant(ctx context.Context, tenantID string) ([]notifytarget.NotifyTarget, error)
	Insert(ctx context.Context, t notifytarget.NotifyTarget) (uuid.UUID, time.Time, error)
	GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*notifytarget.NotifyTarget, error)
	UpdateByID(ctx context.Context, tenantID string, id uuid.UUID, t notifytarget.NotifyTarget) error
	Delete(ctx context.Context, tenantID string, id uuid.UUID) error
}

// NotifyTargetsHandler serves /fb/v1/console/notify-targets.
type NotifyTargetsHandler struct {
	repo notifyTargetRepo
}

func NewNotifyTargetsHandler(r notifyTargetRepo) *NotifyTargetsHandler {
	return &NotifyTargetsHandler{repo: r}
}

// toNotifyProto drops Secret (write-only) + TenantID (known via session).
func toNotifyProto(row notifytarget.NotifyTarget) *attunev1.NotifyTarget {
	t := &attunev1.NotifyTarget{
		Id:              row.ID.String(),
		DestinationType: row.DestinationType,
		Audience:        row.Audience,
		Url:             row.URL,
		TimeoutSeconds:  int32(row.TimeoutSeconds),
		Disabled:        row.Disabled,
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
		LastError:       row.LastError,
	}
	if row.LastFailureAt != nil {
		s := row.LastFailureAt.UTC().Format(time.RFC3339)
		t.LastFailureAt = &s
	}
	return t
}

// List handles GET /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) List(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.List"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)
	rows, err := h.repo.ListByTenant(ctx, auth.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "notify-targets list", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] notifytarget.ListByTenant failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "查询通知目标失败")
		return
	}
	items := make([]*attunev1.NotifyTarget, 0, len(rows))
	for _, row := range rows {
		items = append(items, toNotifyProto(row))
	}
	respond.Proto(w, http.StatusOK, &attunev1.ListNotifyTargetsResponse{Items: items})
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
}

// createNotifyRequest carries normalized create/patch fields through validation.
type createNotifyRequest struct {
	DestinationType string
	Audience        string
	URL             string
	Secret          string
	TimeoutSeconds  int
	Disabled        bool
}

// Create handles POST /fb/v1/console/notify-targets.
func (h *NotifyTargetsHandler) Create(w http.ResponseWriter, r *http.Request) {
	const where = "console.NotifyTargetsHandler.Create"
	ctx := r.Context()
	auth := session.FromContext(ctx)
	var req attunev1.CreateNotifyTargetRequest
	if err := respond.Decode(r.Body, &req); err != nil {
		if errors.Is(err, respond.ErrBodyTooLarge) {
			logext.Warnf(ctx, "[%s] reject: body too large,tenant_id:%s", where, auth.TenantID)
			respond.Error(ctx, w, http.StatusRequestEntityTooLarge, "body_too_large", "请求体超过 1 MiB 上限")
			return
		}
		logext.Warnf(ctx, "[%s] reject: bad json,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "bad_request", "请求体不是合法 JSON")
		return
	}
	nreq := &createNotifyRequest{
		DestinationType: req.GetDestinationType(),
		Audience:        req.GetAudience(),
		URL:             req.GetUrl(),
		Secret:          req.GetSecret(),
		TimeoutSeconds:  int(req.GetTimeoutSeconds()),
		Disabled:        req.GetDisabled(),
	}
	if err := validateNotifyCreate(nreq); err != nil {
		logext.Warnf(ctx, "[%s] reject: validation,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	// Phase 1 only ships lark-bot + raw-webhook adapters.
	if nreq.DestinationType == notifytarget.DestSlackBot || nreq.DestinationType == notifytarget.DestEmail {
		logext.Warnf(ctx, "[%s] reject: not implemented,tenant_id:%s,dest:%s",
			where, auth.TenantID, nreq.DestinationType)
		respond.Error(ctx, w, http.StatusNotImplemented, "not_implemented",
			"destination_type "+nreq.DestinationType+" 尚未实现（Wave 3+）")
		return
	}
	logext.Infof(ctx, "[%s] start,tenant_id:%s,dest:%s,audience:%s",
		where, auth.TenantID, nreq.DestinationType, nreq.Audience)

	target := notifytarget.NotifyTarget{
		TenantID:        auth.TenantID,
		DestinationType: nreq.DestinationType,
		Audience:        nreq.Audience,
		URL:             nreq.URL,
		Secret:          nreq.Secret,
		TimeoutSeconds:  nreq.TimeoutSeconds,
		Disabled:        nreq.Disabled,
	}
	id, createdAt, err := h.repo.Insert(ctx, target)
	if err != nil {
		if errors.Is(err, notifytarget.ErrNotifyTargetConflict) {
			logext.Warnf(ctx, "[%s] reject: conflict,tenant_id:%s,dest:%s,audience:%s",
				where, auth.TenantID, nreq.DestinationType, nreq.Audience)
			respond.Error(ctx, w, http.StatusConflict, "conflict",
				"同一 (destination_type, audience) 组合下已有目标；先删除旧的再建")
			return
		}
		slog.ErrorContext(ctx, "notify-targets insert", "err", err, "tenant_id", auth.TenantID)
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		respond.Error(ctx, w, http.StatusInternalServerError, "internal", "新建通知目标失败")
		return
	}
	target.ID = id
	target.CreatedAt = createdAt
	respond.Proto(w, http.StatusCreated, toNotifyProto(target))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,dest:%s",
		where, auth.TenantID, id, nreq.DestinationType)
}

// validateNotifyCreate runs the field-level rules + normalization in place.
func validateNotifyCreate(req *createNotifyRequest) error {
	req.DestinationType = strings.TrimSpace(req.DestinationType)
	req.URL = strings.TrimSpace(req.URL)
	if req.DestinationType == "" {
		return errors.New("destination_type 不能为空")
	}
	switch req.DestinationType {
	case notifytarget.DestLarkBot, notifytarget.DestRawWebhook, notifytarget.DestSlackBot, notifytarget.DestEmail:
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
	switch req.Audience {
	case "":
		req.Audience = notifytarget.AudienceAll
	case notifytarget.AudiencePool, notifytarget.AudienceRadar, notifytarget.AudienceAll:
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
