package notifytarget

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
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
	repo notifyTargetRepo
}

func NewNotifyTargetsHandler(r notifyTargetRepo) *NotifyTargetsHandler {
	return ptrext.Of(NotifyTargetsHandler{repo: r})
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
	case notifytarget.DestRawWebhook, notifytarget.DestSlackBot, notifytarget.DestEmail:
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
