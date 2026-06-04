package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/wanmuchengchuan/listen/internal/domain"
	"github.com/wanmuchengchuan/listen/internal/infra/metrics"
	"github.com/wanmuchengchuan/listen/internal/logext"
	"github.com/wanmuchengchuan/listen/internal/repo"
)

// envelopeVersion is the wire-format version field. Bumping requires
// coordination with every customer's verifier — never break v1.
const envelopeVersion = "1"

// EventEnriched is the only event_type Wave 1.2 emits. Wave 2 adds
// "feedback.updated" / "feedback.deleted" — handlers should switch on
// this field, not rely on field shape.
const EventEnriched = "feedback.enriched"

// RawWebhookRouter delivers Snapshot payloads to per-tenant custom HTTPS
// destinations. Built from rows in tenant_notify_targets; each tenant
// can have at most one (destination_type=raw-webhook, audience=<a>)
// row per audience.
//
// Failure counters are kept in-memory only — Wave 2 console persists
// them to DB so PMs can see "this customer's webhook has been failing
// for 3 days".
type RawWebhookRouter struct {
	transport *Transport
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
// tenant_notify_targets. Wave 1.2: env→DB sync at startup populates the
// table; this constructor is called after that sync.
//
// Passing a nil httpClient gives a 10s-per-call default. retry should
// usually be DefaultRetry() for raw webhook (5 attempts with backoff).
func NewRawWebhookRouter(transport *Transport, targets []repo.NotifyTarget) *RawWebhookRouter {
	dests := make(map[string]map[string]*rawDestination)
	for _, t := range targets {
		if t.DestinationType != repo.DestRawWebhook {
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
		dests[t.TenantID][t.Audience] = &rawDestination{
			url:     t.URL,
			secret:  t.Secret,
			timeout: timeout,
		}
	}
	return &RawWebhookRouter{transport: transport, destinations: dests}
}

// PushPool delivers s to the tenant's audience=pool destination (or
// audience=all). Returns nil silently when the tenant has no configured
// raw-webhook destination.
func (r *RawWebhookRouter) PushPool(ctx context.Context, s domain.Snapshot) error {
	return r.dispatch(ctx, s, repo.AudiencePool)
}

// PushRadar delivers s to the tenant's audience=radar destination (or
// audience=all). Caller should filter to P0/P1 — but we don't enforce
// here, keeping the Notifier shape uniform.
func (r *RawWebhookRouter) PushRadar(ctx context.Context, s domain.Snapshot) error {
	return r.dispatch(ctx, s, repo.AudienceRadar)
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
		dest, ok = tenantDests[repo.AudienceAll]
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
		req.Header.Set("X-Listen-Signature", signRawBody(body, dest.secret))
		req.Header.Set("User-Agent", "listen/1.0")
		// 上游 req body 截断 1024 字节; X-Listen-Signature 头 skip(签名敏感)。
		logext.Infof(ctx, "[%s] upstream req,label:%s,body:%s",
			where, label, truncate(string(body), 1024))
		return req, nil
	}
	err := r.transport.Send(ctx, label, build, checkRawResponse(label, s))
	if err != nil {
		dest.failures.Add(1)
		msg := err.Error()
		dest.lastError.Store(&msg)
		reason := "transport"
		if errors.Is(err, ErrTerminal) {
			reason = "terminal"
		}
		metrics.NotifyFailuresTotal.
			WithLabelValues(repo.DestRawWebhook, reason).Inc()
		logext.Errorf(ctx, "[%s] send failed,label:%s,feedback_id:%d,reason:%s,err:%+v",
			where, label, s.ID, reason, err.Error())
		return err
	}
	now := time.Now()
	dest.lastSuccess.Store(&now)
	logext.Infof(ctx, "[%s] OK,label:%s,feedback_id:%d", where, label, s.ID)
	return nil
}

// buildRawEnvelope serializes a Snapshot into the v1 envelope JSON.
// trace_id is left empty here; the outbox worker fills it from the
// outbox row before calling this. Wave 1.2 inline path (no outbox yet)
// leaves it empty until §3.6 lands.
func buildRawEnvelope(s domain.Snapshot) ([]byte, error) {
	env := rawEnvelope{
		Version:     envelopeVersion,
		EventType:   EventEnriched,
		DeliveredAt: time.Now().UTC().Format(time.RFC3339),
		Feedback: rawFeedback{
			ID:          s.ID,
			TenantID:    s.TenantID,
			Content:     s.Content,
			Source:      s.Source,
			UserID:      s.UserID,
			SubmittedAt: s.EnrichedAt.UTC().Format(time.RFC3339), // best approx without ingest_at
			Enriched: rawEnriched{
				Title:      s.Title,
				Kind:       s.Kind,
				Severity:   s.Severity,
				Modules:    s.Modules,
				Priority:   s.Priority,
				Rationale:  "", // domain.Snapshot doesn't carry rationale yet
				EnrichedAt: s.EnrichedAt.UTC().Format(time.RFC3339),
			},
		},
	}
	return json.Marshal(env)
}

// signRawBody is the HMAC-SHA256 over the request body, encoded as
// "sha256=<hex>". Customers verify with the same construction in their
// language — see quickstart.md for 5-line examples.
func signRawBody(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// checkRawResponse maps HTTP responses to nil / retryable / ErrTerminal.
// Per §3.1 edge cases: 4xx (except 408 / 429) is terminal; everything
// else (including network errors which never reach this checker) is
// retryable up to the transport's MaxAttempts.
func checkRawResponse(label string, s domain.Snapshot) ResponseChecker {
	const where = "notify.checkRawResponse"
	return func(status int, body []byte) error {
		// 上游响应日志(每 attempt 都有,truncate 1024 字节)。
		logext.Infof(context.Background(),
			"[%s] upstream resp,label:%s,feedback_id:%d,status:%d,body:%s",
			where, label, s.ID, status, truncate(string(body), 1024))
		switch {
		case status >= 200 && status < 300:
			slog.InfoContext(context.Background(), "raw webhook delivered",
				"dest", label, "feedback_id", s.ID, "status", status)
			return nil
		case status == 408 || status == 429:
			return fmt.Errorf("raw webhook %s retryable status=%d body=%s",
				label, status, truncate(string(body), 200))
		case status >= 400 && status < 500:
			return fmt.Errorf("%w: raw webhook %s status=%d body=%s",
				ErrTerminal, label, status, truncate(string(body), 200))
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
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	Modules    []string `json:"modules"`
	Priority   float64  `json:"priority"`
	Rationale  string   `json:"rationale"`
	EnrichedAt string   `json:"enriched_at"`
}
