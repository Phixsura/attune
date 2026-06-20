package outbox

// outbox_worker_send.go — per-destination sender via the outbound framework.
// Replaces the hardcoded switch with outbound.LookupEvent.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

// sendByDestType dispatches to the appropriate outbound channel adapter.
// The outbox row's payload is the same generic attune-envelope for every dest;
// the adapter shapes the actual HTTP request.
func (w *OutboxWorker) sendByDestType(
	ctx context.Context,
	row outboxrepo.OutboxRow,
	target *notifytarget.NotifyTarget,
) error {
	const where = "service.OutboxWorker.sendByDestType"

	ch := outbound.LookupEvent(row.DestinationType)
	if ch == nil {
		return fmt.Errorf("%w: no outbound channel for dest_type %q",
			notify.ErrTerminal, row.DestinationType)
	}

	env, err := unmarshalEnvelope(row.Payload)
	if err != nil {
		logext.Warnf(ctx, "[%s] reject: bad payload,id:%d,err:%s", where, row.ID, err.Error())
		return fmt.Errorf("%w: unmarshal envelope: %w", notify.ErrTerminal, err)
	}
	// Stamp the stable per-row delivery id so adapters can emit it as a dedup
	// header — the row id is constant across at-least-once retries.
	env.DeliveryID = fmt.Sprintf("%d", row.ID)

	dst := toOutboundTarget(target)
	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		logext.Errorf(ctx, "[%s] render failed,id:%d,dest_type:%s,err:%+v",
			where, row.ID, row.DestinationType, err.Error())
		return err
	}

	label := fmt.Sprintf("outbox-%d", row.ID)
	return w.transport.Send(ctx, label, rendered.Build, wrapCheck(rendered.Check, row))
}

// unmarshalEnvelope converts the outbox payload JSON into an outbound.Envelope.
// The outbox format uses "delivered_at" and nests tenant_id inside feedback;
// Envelope expects "timestamp" and top-level "tenant_id". We fix up after unmarshal.
func unmarshalEnvelope(payload []byte) (*outbound.Envelope, error) {
	var env outbound.Envelope
	if err := json.Unmarshal(payload, &env); err != nil { // ptrext:allow unmarshal-out-param
		return nil, err
	}
	if env.Timestamp == "" {
		var raw struct {
			DeliveredAt string `json:"delivered_at"`
		}
		_ = json.Unmarshal(payload, &raw) // ptrext:allow unmarshal-out-param
		env.Timestamp = raw.DeliveredAt
	}
	if env.TenantID == "" {
		if tid, ok := env.Feedback["tenant_id"].(string); ok {
			env.TenantID = tid
		}
	}
	return ptrext.Of(env), nil
}

// toOutboundTarget converts a repo NotifyTarget to an outbound.Target.
func toOutboundTarget(t *notifytarget.NotifyTarget) outbound.Target {
	sigVersion := t.SignatureVersion
	if sigVersion == "" {
		sigVersion = outbound.SignatureVersionContentHash
	}
	return outbound.Target{
		ID:               t.ID.String(),
		TenantID:         t.TenantID,
		URL:              t.URL,
		Secret:           t.Secret,
		SignatureVersion: sigVersion,
		DestinationType:  t.DestinationType,
	}
}

// wrapCheck wraps the adapter's response checker with logging for the outbox row.
func wrapCheck(check outbound.ResponseChecker, row outboxrepo.OutboxRow) notify.ResponseChecker {
	const where = "service.checkOutboxResponse"
	return func(ctx context.Context, status int, body []byte) error {
		logext.Infof(ctx,
			"[%s] upstream resp,id:%d,status:%d,body:%s",
			where, row.ID, status, truncateStr(string(body), 1024))

		err := check(ctx, status, body)
		if err == nil {
			logext.Infof(ctx, "[%s] row delivered,id:%d,tenant:%s,inbound_trace_id:%s,status:%d",
				where, row.ID, row.TenantID, row.TraceID, status)
			return nil
		}
		// outbound.ErrTerminal and notify.ErrTerminal are separate sentinels
		// (depguard forbids outbound → notify). Translate here so the
		// transport's retry loop recognises adapter-terminal failures.
		if errors.Is(err, outbound.ErrTerminal) {
			return fmt.Errorf("%w: %w", notify.ErrTerminal, err)
		}
		return err
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
