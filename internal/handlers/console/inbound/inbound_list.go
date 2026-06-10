// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// List handles GET /fb/v1/console/inbound/sources.
//
// SourceStore.List filters on (channel, enabled=TRUE); the operator UI
// needs to see paused rows too, so we query the table directly. The
// alternative — extending SourceStore with a per-tenant "list all" —
// would break the adapter contract just to feed the console.
func (h *Handler) List(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListInboundSourcesRequest) (dispatcher.Result[*attunev1.ListInboundSourcesResponse], error) {
	const where = "console.inbound.List"
	auth := ctx.Auth
	logext.Infof(ctx, "[%s] start,tenant_id:%s", where, auth.TenantID)

	rows, err := h.listAllForTenant(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListInboundSourcesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list inbound sources")
	}
	items := make([]*attunev1.InboundSource, 0, len(rows))
	for _, row := range rows {
		items = append(items, rowToProto(row))
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,count:%d", where, auth.TenantID, len(items))
	return dispatcher.OK(ptrext.Of(attunev1.ListInboundSourcesResponse{Items: items}))
}

// listAllForTenant — direct query that ignores the enabled flag (so the
// UI can show paused rows). Kept inside the handler to avoid widening
// the SourceStore contract for a console-only need.
func (h *Handler) listAllForTenant(ctx context.Context, tenantID string) ([]inbound.Source, error) {
	if h.pool == nil {
		return nil, errors.New("inbound: pool not configured")
	}
	rows, err := h.pool.Query(
		ctx,
		`SELECT id, tenant_id, channel, name, slug, enabled,
		        last_event_at, last_uid, last_error, created_at, updated_at
		   FROM inbound_sources
		  WHERE tenant_id = $1
		  ORDER BY channel ASC, name ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("inbound: query: %w", err)
	}
	defer rows.Close()
	var out []inbound.Source
	for rows.Next() {
		var s inbound.Source
		var lastEventAt *time.Time
		var lastError *string
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Channel, &s.Name, &s.Slug,
			&s.Enabled, &lastEventAt, &s.State.LastUID, &lastError,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("inbound: scan: %w", err)
		}
		s.State.LastEventAt = lastEventAt
		s.State.LastError = ptrext.Indirect(lastError)
		out = append(out, s)
	}
	return out, rows.Err()
}
