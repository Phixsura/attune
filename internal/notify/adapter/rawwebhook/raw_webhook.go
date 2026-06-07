package rawwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/notify/sig"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// EventEnriched is the only event_type emits. a follow-up will add
// "feedback.updated" / "feedback.deleted" — handlers should switch on
// this field, not rely on field shape.
const EventEnriched = "feedback.enriched"

// RawWebhookRouter delivers Snapshot payloads to per-tenant custom HTTPS
// destinations. Built from rows in tenant_notify_targets; each tenant
// can have at most one (destination_type=raw-webhook, audience=<a>)
// row per audience.
//
// Failure counters are kept in-memory only — a follow-up will persist
// them to DB so PMs can see "this customer's webhook has been failing
// for 3 days".
type RawWebhookRouter struct {
	transport *notify.Transport
	// destinations: map[tenant_id]map[audience]*rawDestination
	destinations map[string]map[string]*rawDestination
}

type rawDestination struct {
	url         string
	secret      string
	timeout     time.Duration
	failures    atomic.Uint64
	lastError   atomic.Pointer[string]
	lastSuccess atomic.Pointer[time.Time]
}

// NewRawWebhookRouter builds the router from active rows in
// tenant_notify_targets. : env→DB sync at startup populates the
// table; this constructor is called after that sync.
//
// Passing a nil httpClient gives a 10s-per-call default. retry should
// usually be DefaultRetry() for raw webhook (5 attempts with backoff).
func NewRawWebhookRouter(transport *notify.Transport, targets []notifytarget.NotifyTarget) *RawWebhookRouter {
	dests := make(map[string]map[string]*rawDestination)
	for _, t := range targets {
		if t.DestinationType != notifytarget.DestRawWebhook {
			continue
		}
		if t.URL == "" || t.Secret == "" {
			slog.WarnContext(context.Background(), "raw webhook target missing url/secret, skipping",
				"tenant_id", t.TenantID, "audience", t.Audience)
			continue
		}
		if _, ok := dests[t.TenantID]; !ok {
			dests[t.TenantID] = make(map[string]*rawDestination)
		}
		timeout := time.Duration(t.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		dests[t.TenantID][t.Audience] = ptrext.Of(rawDestination{
			url:     t.URL,
			secret:  t.Secret,
			timeout: timeout,
		})
	}
	return ptrext.Of(RawWebhookRouter{transport: transport, destinations: dests})
}

// PushPool delivers s to the tenant's audience=pool destination (or
// audience=all). Returns nil silently when the tenant has no configured
// raw-webhook destination.
func (r *RawWebhookRouter) PushPool(ctx context.Context, s domain.Snapshot) error {
	return r.dispatch(ctx, s, notifytarget.AudiencePool)
}

// PushRadar delivers s to the tenant's audience=radar destination (or
// audience=all). Caller should filter to P0/P1 — but we don't enforce
// here, keeping the Notifier shape uniform.
func (r *RawWebhookRouter) PushRadar(ctx context.Context, s domain.Snapshot) error {
	return r.dispatch(ctx, s, notifytarget.AudienceRadar)
}

// dispatch finds the right destination by (tenant, audience), tries
// audience=all as a fallback, and sends via transport. A snapshot whose
// tenant isn't in the routing table is a no-op (return nil).
func (r *RawWebhookRouter) dispatch(ctx context.Context, s domain.Snapshot, audience string) error {
	tenantDests, ok := r.destinations[s.TenantID]
	if !ok {
		return nil
	}
	dest, ok := tenantDests[audience]
	if !ok {
		dest, ok = tenantDests[notifytarget.AudienceAll]
	}
	if !ok {
		return nil
	}
	return r.send(ctx, audience, dest, s)
}

func (r *RawWebhookRouter) send(
	ctx context.Context,
	audience string,
	dest *rawDestination,
	s domain.Snapshot,
) error {
	const where = "notify.RawWebhookRouter.send"
	label := fmt.Sprintf("raw-%s-%s", audience, s.TenantID)
	logext.Infof(ctx, "[%s] start,label:%s,feedback_id:%d,url:%s",
		where, label, s.ID, dest.url)
	build := func(ctx context.Context) (*http.Request, error) {
		body, err := buildRawEnvelope(s)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("X-Attune-Signature", sig.SignRaw(body, dest.secret))
		req.Header.Set("User-Agent", "attune/1.0")
		// Upstream request body — truncated at 1024 bytes; the
		// X-Attune-Signature header is intentionally not logged.
		logext.Infof(ctx, "[%s] upstream req,label:%s,body:%s",
			where, label, truncate(string(body), 1024))
		return req, nil
	}
	err := r.transport.Send(ctx, label, build, checkRawResponse(label, s))
	if err != nil {
		dest.failures.Add(1)
		dest.lastError.Store(ptrext.Of(err.Error()))
		reason := "transport"
		if errors.Is(err, notify.ErrTerminal) {
			reason = "terminal"
		}
		metrics.NotifyFailuresTotal.
			WithLabelValues(notifytarget.DestRawWebhook, reason).Inc()
		logext.Errorf(ctx, "[%s] send failed,label:%s,feedback_id:%d,reason:%s,err:%+v",
			where, label, s.ID, reason, err.Error())
		return err
	}
	dest.lastSuccess.Store(ptrext.Of(time.Now()))
	logext.Infof(ctx, "[%s] OK,label:%s,feedback_id:%d", where, label, s.ID)
	return nil
}

// buildRawEnvelope serializes a Snapshot into the v1 envelope JSON.
// trace_id is left empty here; the outbox worker fills it from the
// outbox row before calling this. the inline path (no outbox yet)
// leaves it empty until §3.6 lands.
func buildRawEnvelope(s domain.Snapshot) ([]byte, error) {
	env := rawEnvelope{
		Version:     sig.EnvelopeVersion,
		EventType:   EventEnriched,
		DeliveredAt: time.Now().UTC().Format(time.RFC3339),
		Feedback: rawFeedback{
			ID:          s.ID,
			TenantID:    s.TenantID,
			Content:     s.Content,
			Source:      s.Source,
			UserID:      s.UserID,
			SubmittedAt: s.SubmittedAt.UTC().Format(time.RFC3339), // #82: actual ingest time (user_feedback.created_at)
			Enriched: rawEnriched{
				Title:      s.Title,
				Attrs:      nilSafeAttrs(s.Attrs),
				IsUrgent:   s.IsUrgent,
				Rationale:  s.Rationale,
				EnrichedAt: s.EnrichedAt.UTC().Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(env)
}

// signRawBody moved to `internal/notify/sig.SignRaw` — single
// canonical implementation shared by rawwebhook, test_send, outbox.
// Customers verify with the same "sha256=<hex>" construction in their
// language — see quickstart.md for 5-line examples.

// checkRawResponse maps HTTP responses to nil / retryable / ErrTerminal.
// Per §3.1 edge cases: 4xx (except 408 / 429) is terminal; everything
// else (including network errors which never reach this checker) is
// retryable up to the transport's MaxAttempts.
func checkRawResponse(label string, s domain.Snapshot) notify.ResponseChecker {
	const where = "notify.checkRawResponse"
	return func(ctx context.Context, status int, body []byte) error {
		// Upstream response log — fires per attempt; body truncated at 1024 bytes.
		logext.Infof(ctx,
			"[%s] upstream resp,label:%s,feedback_id:%d,status:%d,body:%s",
			where, label, s.ID, status, truncate(string(body), 1024))
		switch {
		case status >= 200 && status < 300:
			slog.InfoContext(ctx, "raw webhook delivered",
				"dest", label, "feedback_id", s.ID, "status", status)
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("raw webhook %s retryable status=%d body=%s",
				label, status, truncate(string(body), 200))
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: raw webhook %s status=%d body=%s",
				notify.ErrTerminal, label, status, truncate(string(body), 200))
		default:
			return fmt.Errorf("raw webhook %s status=%d body=%s",
				label, status, truncate(string(body), 200))
		}
	}
}

// rawEnvelope and inner types — kept un-exported because customers
// receive JSON, not Go types. Field tags are the contract.

type rawEnvelope struct {
	Version     string      `json:"version"`
	EventType   string      `json:"event_type"`
	DeliveredAt string      `json:"delivered_at"`
	TraceID     string      `json:"trace_id,omitempty"`
	Feedback    rawFeedback `json:"feedback"`
}

type rawFeedback struct {
	ID          int64       `json:"id"`
	TenantID    string      `json:"tenant_id"`
	Content     string      `json:"content"`
	Source      string      `json:"source"`
	UserID      string      `json:"user_id"`
	SubmittedAt string      `json:"submitted_at"`
	Enriched    rawEnriched `json:"enriched"`
}

type rawEnriched struct {
	Title      string         `json:"title"`
	Attrs      map[string]any `json:"attrs"`
	IsUrgent   bool           `json:"is_urgent"`
	Rationale  string         `json:"rationale"`
	EnrichedAt string         `json:"enriched_at"`
}

// nilSafeAttrs guarantees a non-nil map so the JSON encodes as `{}`
// instead of `null`. Customers' verifiers may type-check the field.
func nilSafeAttrs(a map[string]any) map[string]any {
	if a == nil {
		return map[string]any{}
	}
	return a
}
