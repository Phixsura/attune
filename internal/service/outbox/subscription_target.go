package outbox

// subscription_target.go — resolution + send for subscription-webhook rows
// (#234). destination_target holds the webhook_subscriptions row id; the
// generic (raw-webhook) adapter shapes the HTTP request with the
// subscription's URL and per-subscription secret. A delivery answered with
// HTTP 410 Gone auto-disables the subscription (REST-hook contract: the
// consumer told us the hook is dead), and the row goes dead.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
)

// DestSubscriptionWebhook is the notify_outbox destination_type for
// automation-subscription rows. It never appears in tenant_notify_targets —
// the destination is resolved from webhook_subscriptions instead.
const DestSubscriptionWebhook = "subscription-webhook"

// AudienceSubscription is the audience marker stored on subscription rows.
// Routing is by event_types on the subscription, not by audience; the value
// exists because notify_outbox.audience is NOT NULL and feeds ops displays.
const AudienceSubscription = "subscription"

// subscriptionStore is the worker's view of the subscription repo.
type subscriptionStore interface {
	GetByIDAny(ctx context.Context, id uuid.UUID) (*webhooksub.Subscription, error)
	Disable(ctx context.Context, id uuid.UUID, reason string) error
}

// SetSubscriptionStore wires the webhook-subscription resolver. Unset →
// subscription rows go dead (consistent with a removed destination).
func (w *OutboxWorker) SetSubscriptionStore(subs subscriptionStore) {
	w.subs = subs
}

// processSubscriptionRow handles one subscription-webhook outbox entry:
// resolve the subscription, send via the generic adapter, honor 410.
func (w *OutboxWorker) processSubscriptionRow(ctx context.Context, row outboxrepo.OutboxRow) {
	const where = "service.OutboxWorker.processSubscriptionRow"
	sub, ok := w.resolveSubscription(ctx, row)
	if !ok {
		return
	}

	if err := w.sendToSubscription(ctx, row, sub); err != nil {
		de := notify.AsDeliveryError(err)
		if de.HTTPStatus == http.StatusGone {
			w.disableGoneSubscription(ctx, row, sub)
			return
		}
		if errors.Is(err, notify.ErrTerminal) {
			logext.Warnf(ctx, "[%s] terminal failure,id:%d,kind:%s,status:%d,err:%s",
				where, row.ID, de.Kind, de.HTTPStatus, err.Error())
			w.markDeadSubscription(ctx, row, err.Error(), de.Kind, de.HTTPStatus)
			return
		}
		logext.Warnf(ctx, "[%s] retryable failure,id:%d,attempts:%d,kind:%s,status:%d,err:%s",
			where, row.ID, row.Attempts, de.Kind, de.HTTPStatus, err.Error())
		w.failOrDead(ctx, row, err.Error(), de.Kind, de.HTTPStatus)
		return
	}

	logext.Infof(ctx, "[%s] OK,id:%d,tenant:%s,subscription:%s",
		where, row.ID, row.TenantID, row.DestinationTarget)
	if n, err := w.outbox.MarkDelivered(ctx, row.ID, w.owner); err != nil {
		logext.Warnf(ctx, "[%s] mark delivered failed,id:%d,err:%+v", where, row.ID, err.Error())
	} else if n == 0 {
		logext.Warnf(ctx, "[%s] row re-claimed by another worker,id:%d", where, row.ID)
	}
}

// resolveSubscription loads and validates the row's subscription. On any
// unusable state the row goes dead and ok=false. A queued envelope may
// outlive its subscription — that's the expected disable/delete race, not
// an error worth retrying.
func (w *OutboxWorker) resolveSubscription(
	ctx context.Context,
	row outboxrepo.OutboxRow,
) (*webhooksub.Subscription, bool) {
	const where = "service.OutboxWorker.resolveSubscription"
	if w.subs == nil {
		w.markDeadSubscription(ctx, row, "subscription store not configured", notify.KindTerminal, 0)
		return nil, false
	}
	subID, err := uuid.Parse(row.DestinationTarget)
	if err != nil {
		w.markDeadSubscription(ctx, row,
			"invalid subscription id "+row.DestinationTarget, notify.KindTerminal, 0)
		return nil, false
	}
	sub, err := w.subs.GetByIDAny(ctx, subID)
	if errors.Is(err, webhooksub.ErrSubscriptionNotFound) {
		w.markDeadSubscription(ctx, row, "subscription deleted mid-flight", notify.KindTerminal, 0)
		return nil, false
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] lookup failed,id:%d,err:%+v", where, row.ID, err.Error())
		w.failOrDead(ctx, row, fmt.Sprintf("lookup subscription: %v", err), notify.KindOther, 0)
		return nil, false
	}
	if sub.Status != webhooksub.StatusActive {
		w.markDeadSubscription(ctx, row, "subscription disabled ("+sub.DisabledReason+")",
			notify.KindTerminal, 0)
		return nil, false
	}
	return sub, true
}

// sendToSubscription renders via the generic raw-webhook adapter with the
// subscription's URL/secret and POSTs through the shared transport.
func (w *OutboxWorker) sendToSubscription(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	sub *webhooksub.Subscription,
) error {
	const where = "service.OutboxWorker.sendToSubscription"
	ch := outbound.LookupEvent("raw-webhook")
	if ch == nil {
		return fmt.Errorf("%w: raw-webhook adapter not registered", notify.ErrTerminal)
	}
	env, err := unmarshalEnvelope(row.Payload)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad payload,id:%d,err:%s", where, row.ID, err.Error())
		return fmt.Errorf("%w: unmarshal envelope: %w", notify.ErrTerminal, err)
	}
	env.DeliveryID = fmt.Sprintf("%d", row.ID)

	dst := outbound.Target{
		ID:               sub.ID.String(),
		TenantID:         sub.TenantID,
		URL:              sub.TargetURL,
		Secret:           sub.Secret,
		SignatureVersion: outbound.SignatureVersionContentHash,
		DestinationType:  DestSubscriptionWebhook,
	}
	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		logext.Errorf(ctx, "[%s] render failed,id:%d,err:%+v", where, row.ID, err.Error())
		return err
	}
	label := fmt.Sprintf("outbox-subscription-%d", row.ID)
	return w.transport.Send(ctx, label, rendered.Build, wrapCheck(rendered.Check, row))
}

// disableGoneSubscription handles the 410 contract: stop sending forever.
func (w *OutboxWorker) disableGoneSubscription(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	sub *webhooksub.Subscription,
) {
	const where = "service.OutboxWorker.processSubscriptionRow"
	logext.Infof(ctx, "[%s] consumer answered 410,disabling subscription,id:%d,subscription:%s",
		where, row.ID, sub.ID)
	if err := w.subs.Disable(ctx, sub.ID, webhooksub.ReasonGone); err != nil {
		logext.Errorf(ctx, "[%s] disable failed,subscription:%s,err:%+v",
			where, sub.ID, err.Error())
	}
	w.markDeadSubscription(ctx, row, "gone: consumer answered 410",
		notify.KindHTTP4xx, http.StatusGone)
}

// markDeadSubscription is markDead without the notify-target failure-touch
// (subscription rows have no tenant_notify_targets row to alert on).
func (w *OutboxWorker) markDeadSubscription(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	reason string,
	kind notify.FailureKind,
	httpStatus int,
) {
	const where = "service.OutboxWorker.markDeadSubscription"
	if _, err := w.outbox.MarkDead(ctx, row.ID, w.owner, reason, string(kind), httpStatus); err != nil {
		logext.Errorf(ctx, "[%s] mark dead failed,id:%d,err:%+v", where, row.ID, err.Error())
	}
}
