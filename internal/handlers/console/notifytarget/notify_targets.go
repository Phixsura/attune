package notifytarget

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// notifyTargetRepo is the subset of *notifytarget.NotifyTargetRepo that the
// console handler uses. Defined here (consumer side) so unit tests can pass a
// fake.
type notifyTargetRepo interface {
	ListByTenant(ctx context.Context, tenantID string) ([]notifytarget.NotifyTarget, error)
	Insert(ctx context.Context, t notifytarget.NotifyTarget) (uuid.UUID, time.Time, error)
	GetByID(ctx context.Context, tenantID string, id uuid.UUID) (*notifytarget.NotifyTarget, error)
	UpdateByID(ctx context.Context, tenantID string, id uuid.UUID, t notifytarget.NotifyTarget) error
	Delete(ctx context.Context, tenantID string, id uuid.UUID) error
}

// NotifyTargetsHandler serves /fb/v1/console/notify-targets.
type NotifyTargetsHandler struct {
	repo  notifyTargetRepo
	audit auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func NewNotifyTargetsHandler(r notifyTargetRepo) *NotifyTargetsHandler {
	return ptrext.Of(NotifyTargetsHandler{repo: r})
}

func (h *NotifyTargetsHandler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

// toNotifyProto drops Secret (write-only) + TenantID (known via session).
func toNotifyProto(row notifytarget.NotifyTarget) *attunev1.NotifyTarget {
	t := ptrext.Of(attunev1.NotifyTarget{
		Id:              row.ID.String(),
		DestinationType: row.DestinationType,
		Audience:        row.Audience,
		Url:             row.URL,
		TimeoutSeconds:  int32(row.TimeoutSeconds),
		Disabled:        row.Disabled,
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
		LastError:       row.LastError,
	})
	if row.LastFailureAt != nil {
		t.LastFailureAt = ptrext.Of(row.LastFailureAt.UTC().Format(time.RFC3339))
	}
	return t
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

// validateNotifyCreate runs the field-level rules + normalization in place.
func validateNotifyCreate(req *createNotifyRequest) error {
	req.DestinationType = strings.TrimSpace(req.DestinationType)
	req.URL = strings.TrimSpace(req.URL)
	if req.DestinationType == "" {
		return errors.New("destination_type must not be empty")
	}
	switch req.DestinationType {
	case notifytarget.DestRawWebhook, notifytarget.DestSlack, notifytarget.DestLark,
		notifytarget.DestGitHubIssue:
	default:
		return errors.New("destination_type value is not allowed")
	}
	if req.URL == "" {
		return errors.New("url must not be empty")
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return errors.New("url must be https://... or a loopback http://127.0.0.1")
	}
	loopbackHTTP := u.Scheme == "http" && isLoopback(u.Hostname())
	if u.Scheme != "https" && !loopbackHTTP {
		return errors.New("url must be https://... or a loopback http://127.0.0.1")
	}
	if u.User != nil {
		return errors.New("url must not embed credentials")
	}
	switch req.Audience {
	case "":
		req.Audience = notifytarget.AudienceAll
	case notifytarget.AudiencePool, notifytarget.AudienceRadar, notifytarget.AudienceAll, notifytarget.AudienceDigest:
	default:
		return errors.New("audience value is not allowed")
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 10
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 60 {
		return errors.New("timeout_seconds must be between 1 and 60")
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "[::1]"
}

func (h *NotifyTargetsHandler) recordAudit(
	ctx context.Context,
	authType, actorID, tenantID, action string,
	req *http.Request,
	target notifytarget.NotifyTarget,
	summary string,
	before, after any,
) error {
	if h.audit == nil {
		return nil
	}
	if authType == "" {
		authType = "admin"
	}
	return h.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      auditlogsvc.ActorFromRequest(authType, actorID, req),
		Action:     action,
		TargetType: "notify_target",
		TargetID:   target.ID.String(),
		Summary:    summary,
		Before:     before,
		After:      after,
	})
}

func notifyTargetSummary(action string, target notifytarget.NotifyTarget) string {
	if target.DestinationType == "" {
		return action
	}
	return action + " (" + target.DestinationType + ")"
}

func auditNotifyTargetSnapshot(target notifytarget.NotifyTarget) map[string]any {
	snap := map[string]any{
		"id":               target.ID.String(),
		"destination_type": target.DestinationType,
		"audience":         target.Audience,
		"timeout_seconds":  target.TimeoutSeconds,
		"disabled":         target.Disabled,
	}
	if target.Secret != "" {
		snap["url"] = sanitizeNotifyTargetURL(target.URL)
		snap["has_secret"] = true
	} else {
		snap["url_hash"] = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(target.URL)))
	}
	return snap
}

func sanitizeNotifyTargetURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
