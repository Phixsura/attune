package customerrequest

// automation_sink.go — request-event emission for the automation surface
// (#234). Create and status-changing Update enqueue notify_outbox rows for
// matching webhook_subscriptions inside the SAME transaction as the write,
// so a rolled-back mutation never leaks an event. Follows the
// notificationSink precedent (close-the-loop): nil sink = no-op.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/logext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/webhooksub"
)

// automationSubLister is the service's tx-scoped view of the
// webhook-subscription repo.
type automationSubLister interface {
	ListActiveByTenantEventTx(ctx context.Context, tx pgx.Tx, tenantID, eventType string) ([]webhooksub.Subscription, error)
}

// automationOutbox is the service's view of the outbox writer.
type automationOutbox interface {
	Insert(ctx context.Context, tx pgx.Tx, row outboxrepo.OutboxRow) (int64, error)
}

// SetAutomationSink wires request-event emission to webhook subscriptions.
// Unset → no events (console-only deployments unaffected).
func (s *Service) SetAutomationSink(subs automationSubLister, outbox automationOutbox) {
	s.automationSubs = subs
	s.automationOutbox = outbox
}

// emitRequestEventTx enqueues one outbox row per subscription matching
// eventType, inside the caller's transaction. previousStatus is empty for
// request.created.
func (s *Service) emitRequestEventTx(
	ctx context.Context,
	tx pgx.Tx,
	summary repo.Summary,
	eventType, previousStatus string,
) error {
	const where = "service.customerrequest.emitRequestEventTx"
	if s.automationSubs == nil || s.automationOutbox == nil {
		return nil
	}
	subs, err := s.automationSubs.ListActiveByTenantEventTx(ctx, tx, summary.TenantID, eventType)
	if err != nil {
		logext.Errorf(ctx, "[%s] list subscriptions failed,tenant_id:%s,event:%s,err:%+v",
			where, summary.TenantID, eventType, err.Error())
		return fmt.Errorf("list webhook subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}
	traceID := trace.FromContext(ctx)
	payload, err := BuildRequestEnvelope(summary, eventType, previousStatus, traceID)
	if err != nil {
		logext.Errorf(ctx, "[%s] build envelope failed,request_id:%s,event:%s,err:%+v",
			where, summary.ID, eventType, err.Error())
		return fmt.Errorf("build request envelope: %w", err)
	}
	for _, sub := range subs {
		if _, err := s.automationOutbox.Insert(ctx, tx, outboxrepo.OutboxRow{
			// FeedbackID stays 0 → NULL: request events carry no feedback row.
			TenantID:          summary.TenantID,
			DestinationType:   "subscription-webhook",
			DestinationTarget: sub.ID.String(),
			Audience:          "subscription",
			Payload:           payload,
			TraceID:           traceID,
		}); err != nil {
			logext.Errorf(ctx, "[%s] outbox insert failed,request_id:%s,subscription:%s,err:%+v",
				where, summary.ID, sub.ID, err.Error())
			return fmt.Errorf("queue request event: %w", err)
		}
	}
	logext.Infof(ctx, "[%s] request events queued,tenant_id:%s,request_id:%s,event:%s,count:%d",
		where, summary.TenantID, summary.ID, eventType, len(subs))
	return nil
}

// EventForStatusChange returns the automation event emission parameters for
// a status transition (exported for the emission-point call sites' clarity).
func statusChangeEvent(before, after repo.Summary) (eventType, previousStatus string, changed bool) {
	if before.Status == after.Status {
		return "", "", false
	}
	return domain.EventRequestStatusChanged, string(before.Status), true
}
