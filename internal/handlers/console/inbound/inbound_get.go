// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// Get handles GET /fb/v1/console/inbound/sources/{id}.
func (h *Handler) Get(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	const where = "console.inbound.Get"
	src, err := h.getOwnedSource(ctx, ctx.Auth, req.GetId(), where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}
	out := rowToProto(src)
	// Best-effort sync stats extraction from encrypted config.
	h.enrichWithSyncStats(src, out)
	// Editable settings read-back so the Console edit form can prefill
	// stored values instead of silently resetting them on save.
	h.enrichIntercomSettings(src, out)
	return dispatcher.OK(out)
}

// enrichIntercomSettings populates the operator-editable Intercom
// settings (never credentials) on the detail response. Failures are
// silently ignored — the edit form degrades to empty defaults.
func (h *Handler) enrichIntercomSettings(src inbound.Source, out *attunev1.InboundSource) {
	if src.Channel != channelIntercom || h.secrets == nil || len(src.Config) == 0 {
		return
	}
	summary, err := intercom.DecodeConnSummary(src.Config, h.secrets)
	if err != nil {
		return
	}
	out.IntercomSettings = ptrext.Of(attunev1.IntercomSettings{
		Region:            summary.Region,
		StartFrom:         summary.StartFrom,
		FilterStates:      summary.FilterStates,
		FilterTags:        summary.FilterTags,
		FilterExcludeTags: summary.FilterExcludeTags,
		MaxDetailFetches:  int32(summary.MaxDetailFetches), //nolint:gosec // budget capped at 500 by validation
		WorkspaceId:       summary.WorkspaceID,
	})
}

// enrichWithSyncStats attempts to decrypt the config blob and populate
// the proto sync-stats fields. Failures are silently ignored — stats
// are a non-critical enhancement.
func (h *Handler) enrichWithSyncStats(src inbound.Source, out *attunev1.InboundSource) {
	if h.secrets == nil || len(src.Config) == 0 {
		return
	}
	decoded, err := h.secrets.Decrypt(src.Config)
	if err != nil {
		return
	}
	// Quick JSON extraction — we only need the sync_stats subtree.
	// The proto fields are channel-generic: Zendesk fills tickets_synced,
	// Intercom fills conversations_synced; both surface as TicketsSynced.
	var wrapper struct {
		SyncStats struct {
			TicketsSynced       int64 `json:"tickets_synced"`
			ConversationsSynced int64 `json:"conversations_synced"`
			LastTicketID        int64 `json:"last_ticket_id"`
			BackfillDone        bool  `json:"backfill_done"`
		} `json:"sync_stats"`
	}
	if err := json.Unmarshal(decoded, &wrapper); err != nil { // ptrext:allow json-unmarshal
		return
	}
	synced := wrapper.SyncStats.TicketsSynced
	if synced == 0 {
		synced = wrapper.SyncStats.ConversationsSynced
	}
	if synced > 0 || wrapper.SyncStats.BackfillDone {
		// backfill_done can be true over an empty window (0 synced) —
		// still worth surfacing so "done, nothing found" is
		// distinguishable from "not started".
		out.TicketsSynced = ptrext.Of(synced)
		out.BackfillDone = ptrext.Of(wrapper.SyncStats.BackfillDone)
	}
	if wrapper.SyncStats.LastTicketID > 0 {
		// Zendesk-only; Intercom has no numeric last-ticket concept, so
		// never emit a present-but-zero value.
		out.LastSyncedTicketId = ptrext.Of(wrapper.SyncStats.LastTicketID)
	}
}

func (h *Handler) getOwnedSource(ctx context.Context, auth *session.AuthCtx, id, where string) (inbound.Source, error) {
	if !isUUID(id) {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s", where, auth.TenantID)
		return inbound.Source{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			return inbound.Source{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "inbound source not found")
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return inbound.Source{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load inbound source")
	}
	if src.TenantID != auth.TenantID {
		return inbound.Source{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "inbound source not found")
	}
	return src, nil
}

// isUUID — quick UUID-shape gate before issuing DB ops.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
