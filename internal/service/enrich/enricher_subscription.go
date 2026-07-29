package enrich

// enricher_subscription.go — automation-subscription fan-out (#234).
// Alongside the legacy tenant_notify_targets rows, each enriched snapshot
// fans out to webhook_subscriptions matching feedback.created (every row)
// and feedback.urgent (urgent rows only, the radar predicate). A
// subscription selecting both event types receives both deliveries by
// design: Zapier Zaps bind one trigger each, so the two events are
// independent streams even when one hook URL serves both.

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
)

// subscriptionLister is the enricher's view of the webhook-subscription
// repo — an interface so unit tests can fan out against fakes.
type subscriptionLister interface {
	ListActiveByTenantEvent(ctx context.Context, tenantID, eventType string) ([]webhooksub.Subscription, error)
}

// SetSubscriptions wires automation-subscription fan-out. Unset → no
// subscription rows (legacy notify targets are unaffected either way).
func (e *Enricher) SetSubscriptions(subs subscriptionLister) {
	e.subs = subs
}

// planSubscriptionRows lists matching subscriptions (outside the tx, same
// rule as the notify-target list) and builds one outbox row per
// (subscription, event) pair.
func (e *Enricher) planSubscriptionRows(
	ctx context.Context,
	s domain.Snapshot,
	traceID string,
) ([]outboxrepo.OutboxRow, error) {
	const where = "service.Enricher.planSubscriptionRows"
	if e.subs == nil {
		return nil, nil
	}
	events := []string{domain.EventFeedbackCreated}
	if s.IsUrgent {
		events = append(events, domain.EventFeedbackUrgent)
	}
	var rows []outboxrepo.OutboxRow
	for _, eventType := range events {
		subs, err := e.subs.ListActiveByTenantEvent(ctx, s.TenantID, eventType)
		if err != nil {
			logext.Errorf(ctx, "[%s] list subscriptions failed,tenant_id:%s,event:%s,err:%+v",
				where, s.TenantID, eventType, err.Error())
			return nil, fmt.Errorf("list webhook subscriptions: %w", err)
		}
		if len(subs) == 0 {
			continue
		}
		payload, err := buildOutboxEnvelopeTyped(s, traceID, e.sourceDisplay(s.Source), eventType)
		if err != nil {
			logext.Errorf(ctx, "[%s] build envelope failed,feedback_id:%d,event:%s,err:%+v",
				where, s.ID, eventType, err.Error())
			return nil, fmt.Errorf("build subscription envelope: %w", err)
		}
		for _, sub := range subs {
			rows = append(rows, outboxrepo.OutboxRow{
				FeedbackID:        s.ID,
				TenantID:          s.TenantID,
				DestinationType:   "subscription-webhook",
				DestinationTarget: sub.ID.String(),
				Audience:          "subscription",
				Payload:           payload,
				TraceID:           traceID,
			})
		}
	}
	return rows, nil
}
