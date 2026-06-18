package outbox

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type auditRecorder interface {
	RecordTx(ctx context.Context, tx pgx.Tx, event auditlogsvc.Event) error
}

// SetAuditLogger injects the audit sink. Audit is optional — a nil logger makes
// recordRetryAudit a no-op (matches the tag handler contract).
func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

// recordRetryAudit writes the manual-retry trail inside the retry transaction
// (tx) so it commits or rolls back together with the re-arm. before is the
// pre-retry snapshot (the reset would otherwise erase it); after is the
// re-armed row.
func (h *Handler) recordRetryAudit(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	tx pgx.Tx,
	before, after outboxrepo.OutboxRow,
) error {
	if h.audit == nil {
		return nil
	}
	actorType := ctx.Auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return h.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   ctx.Auth.TenantID,
		Actor:      auditlogsvc.ActorFromRequest(actorType, ctx.Auth.UserID, ctx.Request()),
		Action:     "outbox.retry",
		TargetType: "outbox_delivery",
		TargetID:   strconv.FormatInt(before.ID, 10),
		Summary:    fmt.Sprintf("Retried delivery #%d to %s", before.ID, before.DestinationType),
		Before:     outboxAuditSnapshot(before),
		After:      outboxAuditSnapshot(after),
	})
}

func outboxAuditSnapshot(r outboxrepo.OutboxRow) map[string]any {
	return map[string]any{
		"status":             r.Status,
		"attempts":           r.Attempts,
		"failure_kind":       r.FailureKind,
		"http_status":        r.HTTPStatus,
		"last_error":         r.LastError,
		"dead_reason":        r.DeadReason,
		"manual_retry_count": r.ManualRetryCount,
	}
}
