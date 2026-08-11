// Package webhooksub implements the automation webhook-subscription CRUD
// handlers (/v1/hooks — #234): Zapier-style REST hook subscribe/unsubscribe
// plus the performList samples endpoint. API-key surface only, scope
// hooks:manage (explicit grant).
package webhooksub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/webhooksub"
)

// maxSubscriptionsPerTenant caps fan-out volume per tenant. Create returns
// 409 at the cap; delivery trusts stored rows and never re-checks.
const maxSubscriptionsPerTenant = 25

// subRepo is the handler's view of the subscription repo.
type subRepo interface {
	Insert(ctx context.Context, s repo.Subscription) (repo.Subscription, error)
	ListByTenant(ctx context.Context, tenantID string) ([]repo.Subscription, error)
	Delete(ctx context.Context, tenantID string, id uuid.UUID) (bool, error)
	CountByTenant(ctx context.Context, tenantID string) (int, error)
}

// SampleSource provides recent real envelopes for the performList endpoint.
// Nil source → static fixtures only (empty tenants still get one sample).
type SampleSource interface {
	// RecentEnvelopes returns up to limit envelope JSON payloads for the
	// event type, newest first, schema-identical to live deliveries.
	RecentEnvelopes(ctx context.Context, tenantID, eventType string, limit int) ([][]byte, error)
}

type Handler struct {
	repo    subRepo
	samples SampleSource
	audit   auditRecorder
}

func NewHandler(r subRepo, samples SampleSource) *Handler {
	return ptrext.Of(Handler{repo: r, samples: samples})
}

// Create implements POST /v1/hooks (Zapier REST-hook subscribe).
func (h *Handler) Create(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateWebhookSubscriptionRequest,
) (dispatcher.Result[*attunev1.WebhookSubscription], error) {
	const where = "console.WebhookSubHandler.Create"
	auth := ctx.Auth

	in, err := validateCreate(req)
	if err != nil {
		return dispatcher.Fail[*attunev1.WebhookSubscription](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
	}

	n, err := h.repo.CountByTenant(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] count failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.WebhookSubscription](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create webhook subscription")
	}
	if n >= maxSubscriptionsPerTenant {
		return dispatcher.Fail[*attunev1.WebhookSubscription](
			http.StatusConflict, attunev1.ErrorCode_CONFLICT,
			fmt.Sprintf("subscription limit reached (%d); delete unused hooks first", maxSubscriptionsPerTenant))
	}

	in.TenantID = auth.TenantID
	// API-key sessions carry "apikey:<uuid>" as the user id (apikeyToSession).
	if keyID, err := uuid.Parse(strings.TrimPrefix(auth.UserID, "apikey:")); err == nil {
		in.CreatedByKeyID = ptrext.Of(keyID)
	}
	created, err := h.repo.Insert(ctx, in)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.WebhookSubscription](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create webhook subscription")
	}
	h.recordAudit(ctx, "webhook_subscription.create", created,
		"Created webhook subscription", nil, auditView(created))
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,events:%s,consumer:%s",
		where, auth.TenantID, created.ID, strings.Join(created.EventTypes, ","), created.Consumer)
	return dispatcher.Created(toProto(created))
}

// List implements GET /v1/hooks.
func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListWebhookSubscriptionsRequest,
) (dispatcher.Result[*attunev1.ListWebhookSubscriptionsResponse], error) {
	const where = "console.WebhookSubHandler.List"
	subs, err := h.repo.ListByTenant(ctx, ctx.Auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, ctx.Auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListWebhookSubscriptionsResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list webhook subscriptions")
	}
	items := make([]*attunev1.WebhookSubscription, 0, len(subs))
	for _, s := range subs {
		items = append(items, toProto(s))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListWebhookSubscriptionsResponse{Subscriptions: items}))
}

// Delete implements DELETE /v1/hooks/{id} (Zapier REST-hook unsubscribe).
func (h *Handler) Delete(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteWebhookSubscriptionRequest,
) (dispatcher.Result[*attunev1.DeleteWebhookSubscriptionResponse], error) {
	const where = "console.WebhookSubHandler.Delete"
	auth := ctx.Auth
	id, err := uuid.Parse(strings.TrimSpace(req.GetId()))
	if err != nil {
		return dispatcher.Fail[*attunev1.DeleteWebhookSubscriptionResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id must be a UUID")
	}
	ok, err := h.repo.Delete(ctx, auth.TenantID, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteWebhookSubscriptionResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete webhook subscription")
	}
	if !ok {
		return dispatcher.Fail[*attunev1.DeleteWebhookSubscriptionResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "webhook subscription not found")
	}
	h.recordAudit(ctx, "webhook_subscription.delete", repo.Subscription{ID: id, TenantID: auth.TenantID},
		"Deleted webhook subscription", map[string]any{"id": id.String()}, nil)
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.NoContent[*attunev1.DeleteWebhookSubscriptionResponse]()
}

func validateCreate(req *attunev1.CreateWebhookSubscriptionRequest) (repo.Subscription, error) {
	targetURL := strings.TrimSpace(req.GetTargetUrl())
	if err := validateTargetURL(targetURL); err != nil {
		return repo.Subscription{}, err
	}
	events := req.GetEventTypes()
	if len(events) == 0 {
		return repo.Subscription{}, fmt.Errorf("event_types must contain at least one of: %s",
			strings.Join(domain.AutomationEvents, ", "))
	}
	seen := make(map[string]struct{}, len(events))
	normalized := make([]string, 0, len(events))
	for _, e := range events {
		e = strings.TrimSpace(e)
		if !domain.IsAutomationEvent(e) {
			return repo.Subscription{}, fmt.Errorf("unknown event type %q; valid: %s",
				e, strings.Join(domain.AutomationEvents, ", "))
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		normalized = append(normalized, e)
	}
	secret := req.GetSecret()
	if secret == "" {
		secret = generateSecret()
	} else if len(secret) < 16 {
		return repo.Subscription{}, errors.New("secret must be at least 16 characters (or omit it to auto-generate)")
	}
	consumer := strings.TrimSpace(req.GetConsumer())
	switch consumer {
	case "":
		consumer = repo.ConsumerGeneric
	case repo.ConsumerZapier, repo.ConsumerGeneric:
	default:
		return repo.Subscription{}, errors.New("consumer must be zapier or generic")
	}
	return repo.Subscription{
		TargetURL:  targetURL,
		Secret:     secret,
		EventTypes: normalized,
		Consumer:   consumer,
	}, nil
}

// validateTargetURL mirrors the notify-target rule: HTTPS, or loopback HTTP
// for local development; never credentials in the URL.
func validateTargetURL(raw string) error {
	if raw == "" {
		return errors.New("target_url must not be empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("target_url must be https://... or a loopback http://127.0.0.1")
	}
	loopbackHTTP := u.Scheme == "http" && nethardening.IsLoopbackHost(u.Hostname())
	if u.Scheme != "https" && !loopbackHTTP {
		return errors.New("target_url must be https://... or a loopback http://127.0.0.1")
	}
	if u.User != nil {
		return errors.New("target_url must not embed credentials")
	}
	return nil
}

func generateSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b) // 48 hex chars
}

// toProto converts a repo row to the wire shape. The secret is write-only
// and never leaves the server.
func toProto(s repo.Subscription) *attunev1.WebhookSubscription {
	return ptrext.Of(attunev1.WebhookSubscription{
		Id:             s.ID.String(),
		TargetUrl:      s.TargetURL,
		EventTypes:     s.EventTypes,
		Status:         s.Status,
		DisabledReason: s.DisabledReason,
		Consumer:       s.Consumer,
		CreatedAt:      s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	})
}

func auditView(s repo.Subscription) map[string]any {
	return map[string]any{
		"id":          s.ID.String(),
		"target_host": urlHost(s.TargetURL),
		"event_types": s.EventTypes,
		"consumer":    s.Consumer,
	}
}

// urlHost extracts the host for audit display — the full URL may embed
// per-hook capability tokens (Zapier targetUrls do), so treat the path as
// secret, same rule as Slack/Lark incoming webhooks.
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}
