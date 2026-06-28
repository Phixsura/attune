// Package outbox implements the console operator surface over the notify_outbox
// delivery queue (#33): list dead / in-flight-failed deliveries and manually
// retry one in place. The retry/backoff/dead engine itself lives in the worker
// (internal/service/outbox); this handler only exposes visibility + re-arm.
package outbox

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

type outboxRepo interface {
	ListByStatus(ctx context.Context, tenantID string, statuses []string, limit int, beforeID int64) ([]outboxrepo.OutboxRow, error)
	RetryOne(ctx context.Context, tenantID string, id int64, actor string, auditFn func(context.Context, pgx.Tx, outboxrepo.OutboxRow, outboxrepo.OutboxRow) error) (outboxrepo.RetryOutcome, error)
}

type Handler struct {
	repo  outboxRepo
	audit auditRecorder
}

func NewHandler(r outboxRepo) *Handler {
	return ptrext.Of(Handler{repo: r})
}

// BindListRequest fills the list request from query params:
// ?status=dead&status=failed&limit=50&before_id=123
func BindListRequest(r *http.Request, req *attunev1.ListDeliveriesRequest) error {
	q := r.URL.Query()
	req.Status = q["status"]
	if v := q.Get("limit"); v != "" {
		// ParseInt with bitSize=32 rejects values that don't fit int32, so the
		// conversion below can't overflow (the repo clamps the value anyway).
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid limit")
		}
		req.Limit = int32(n)
	}
	if v := q.Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid before_id")
		}
		req.BeforeId = n
	}
	return nil
}

// List returns the caller's dead/failed deliveries, newest first.
func (h *Handler) List(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListDeliveriesRequest,
) (dispatcher.Result[*attunev1.ListDeliveriesResponse], error) {
	const where = "console.OutboxHandler.List"
	auth := ctx.Auth

	statuses, err := normalizeStatuses(req.GetStatus())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListDeliveriesResponse](
			http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error(),
		)
	}
	limit := int(req.GetLimit())
	rows, err := h.repo.ListByStatus(ctx, auth.TenantID, statuses, limit, req.GetBeforeId())
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListDeliveriesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list deliveries",
		)
	}

	items := make([]*attunev1.OutboxDelivery, 0, len(rows))
	for _, row := range rows {
		items = append(items, ToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListDeliveriesResponse{
		Deliveries:   items,
		NextBeforeId: nextCursor(rows, limit),
	}))
}

// Retry re-arms one dead/failed delivery. Async (the worker redelivers on its
// next poll), so success is 202.
func (h *Handler) Retry(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RetryDeliveryRequest,
) (dispatcher.Result[*attunev1.RetryDeliveryResponse], error) {
	const where = "console.OutboxHandler.Retry"
	auth := ctx.Auth

	id := req.GetId()
	if id <= 0 {
		return dispatcher.Fail[*attunev1.RetryDeliveryResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid delivery id",
		)
	}
	// The audit write runs inside RetryOne's transaction: if it fails (or the
	// action is unregistered), the re-arm rolls back too, so a retry can never
	// commit without its audit trail and we never return 500 for a half-applied
	// retry (#33).
	outcome, err := h.repo.RetryOne(ctx, auth.TenantID, id, auth.UserID,
		func(_ context.Context, tx pgx.Tx, before, after outboxrepo.OutboxRow) error {
			return h.recordRetryAudit(ctx, tx, before, after)
		})
	if err != nil {
		logext.Errorf(ctx, "[%s] retry failed,tenant_id:%s,id:%d,err:%+v", where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RetryDeliveryResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to retry delivery",
		)
	}
	if !outcome.Found {
		return dispatcher.Fail[*attunev1.RetryDeliveryResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "delivery not found",
		)
	}
	if !outcome.Retried {
		msg := fmt.Sprintf("delivery is %s, not retryable", outcome.Status)
		if outcome.InFlight {
			msg = "delivery is being sent right now; try again shortly"
		}
		return dispatcher.Fail[*attunev1.RetryDeliveryResponse](
			http.StatusConflict, attunev1.ErrorCode_CONFLICT, msg,
		)
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%d,prev_status:%s,actor:%s",
		where, auth.TenantID, id, outcome.Status, auth.UserID)
	return dispatcher.Success(http.StatusAccepted, ptrext.Of(attunev1.RetryDeliveryResponse{
		Delivery: ToProto(outcome.Updated),
	}))
}

// normalizeStatuses validates the requested status filter against the known
// set and defaults to dead-only when empty.
func normalizeStatuses(in []string) ([]string, error) {
	valid := map[string]bool{
		outboxrepo.OutboxStatusPending:   true,
		outboxrepo.OutboxStatusDelivered: true,
		outboxrepo.OutboxStatusFailed:    true,
		outboxrepo.OutboxStatusDead:      true,
	}
	if len(in) == 0 {
		return []string{outboxrepo.OutboxStatusDead}, nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if !valid[s] {
			return nil, fmt.Errorf("invalid status %q", s)
		}
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out, nil
}

// nextCursor returns the last row's id when the page came back full (so there
// may be more), else 0.
func nextCursor(rows []outboxrepo.OutboxRow, limit int) int64 {
	if limit <= 0 || limit > outboxrepo.MaxListLimit {
		limit = outboxrepo.MaxListLimit
	}
	if len(rows) < limit {
		return 0
	}
	return rows[len(rows)-1].ID
}

// ToProto maps a repo row to the wire delivery. Timestamps are RFC3339 ("" when
// unset), matching the rest of the console contract.
func ToProto(r outboxrepo.OutboxRow) *attunev1.OutboxDelivery {
	// The webhook URL can carry an auth token in its path (Slack/Lark/Discord),
	// so redact it to scheme://host on every operator-facing field — including
	// last_error / dead_reason, which may embed the full URL via a *url.Error.
	return ptrext.Of(attunev1.OutboxDelivery{
		Id:                r.ID,
		FeedbackId:        r.FeedbackID,
		DestinationType:   r.DestinationType,
		DestinationTarget: nethardening.RedactURL(r.DestinationTarget),
		Audience:          r.Audience,
		Status:            r.Status,
		Attempts:          int32(r.Attempts),
		FailureKind:       failureKindToProto(r.FailureKind),
		HttpStatus:        int32(r.HTTPStatus),
		LastError:         nethardening.RedactURLIn(r.LastError, r.DestinationTarget),
		DeadReason:        nethardening.RedactURLIn(r.DeadReason, r.DestinationTarget),
		TraceId:           r.TraceID,
		NextRetryAt:       formatTime(r.NextRetryAt),
		CreatedAt:         formatTime(r.CreatedAt),
		DeliveredAt:       formatTimePtr(r.DeliveredAt),
		LastManualRetryAt: formatTimePtr(r.LastManualRetryAt),
		RetriedBy:         r.RetriedBy,
		ManualRetryCount:  int32(r.ManualRetryCount),
		InFlight:          r.ClaimedAt != nil,
	})
}

var failureKindProto = map[string]attunev1.OutboxFailureKind{
	"http_4xx":   attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_HTTP_4XX,
	"http_5xx":   attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_HTTP_5XX,
	"timeout":    attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_TIMEOUT,
	"dns":        attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_DNS,
	"connection": attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_CONNECTION,
	"tls":        attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_TLS,
	"terminal":   attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_TERMINAL,
	"other":      attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_OTHER,
}

func failureKindToProto(s string) attunev1.OutboxFailureKind {
	if k, ok := failureKindProto[s]; ok {
		return k
	}
	return attunev1.OutboxFailureKind_OUTBOX_FAILURE_KIND_UNSPECIFIED
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(ptrext.Indirect(t))
}
