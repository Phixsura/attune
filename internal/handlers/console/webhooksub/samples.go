package webhooksub

import (
	"encoding/json"
	"net/http"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// sampleLimit bounds the performList page. Zapier fetches this only in the
// editor to build field mappings; 10 items is plenty and stays well under
// its 30-second budget.
const sampleLimit = 10

// Samples implements GET /v1/hooks/samples/{event_type} — the Zapier
// performList contract: a reverse-chronological array whose items are
// schema-identical to live webhook payloads. Real recent events when the
// tenant has any; a canned static sample otherwise, so the Zap editor's
// "test trigger" always has something to map fields from.
func (h *Handler) Samples(
	ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListWebhookSamplesRequest,
) (dispatcher.Result[*attunev1.ListWebhookSamplesResponse], error) {
	const where = "console.WebhookSubHandler.Samples"
	eventType := req.GetEventType()
	if !domain.IsAutomationEvent(eventType) {
		return dispatcher.Fail[*attunev1.ListWebhookSamplesResponse](
			http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "unknown event type "+eventType)
	}

	var envelopes [][]byte
	if h.samples != nil {
		recent, err := h.samples.RecentEnvelopes(ctx, ctx.Auth.TenantID, eventType, sampleLimit)
		if err != nil {
			// Degrade to the static sample rather than failing the Zap
			// editor: samples are a discovery aid, not business data.
			logext.Warnf(ctx, "[%s] recent envelopes failed,tenant_id:%s,event:%s,err:%+v",
				where, ctx.Auth.TenantID, eventType, err.Error())
		} else {
			envelopes = recent
		}
	}
	if len(envelopes) == 0 {
		envelopes = [][]byte{staticSample(eventType)}
	}

	out := make([]*structpb.Struct, 0, len(envelopes))
	for _, raw := range envelopes {
		// Convert the STORED payload into the WIRE shape through the same
		// mapping the delivery adapter uses (outbound.FromStoredPayload) —
		// Zapier's T004 check requires performList items to be
		// schema-identical to live webhook payloads, so this conversion
		// must never be skipped or reimplemented here.
		env, err := outbound.FromStoredPayload(raw, ctx.Auth.TenantID)
		if err != nil {
			logext.Warnf(ctx, "[%s] skip malformed envelope,err:%s", where, err.Error())
			continue
		}
		wire, err := json.Marshal(env)
		if err != nil {
			logext.Warnf(ctx, "[%s] skip unmarshalable envelope,err:%s", where, err.Error())
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(wire, &m); err != nil {
			logext.Warnf(ctx, "[%s] skip malformed wire envelope,err:%s", where, err.Error())
			continue
		}
		st, err := structpb.NewStruct(m)
		if err != nil {
			logext.Warnf(ctx, "[%s] skip unconvertible envelope,err:%s", where, err.Error())
			continue
		}
		out = append(out, st)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListWebhookSamplesResponse{Samples: out}))
}
