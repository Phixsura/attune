// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// insertRow — direct INSERT against inbound_sources. The repo layer
// exposes List/Get/UpdateState/SetEnabled (the adapter contract); a
// create path lives only in the console handler, so we own the SQL
// here. The caller has already validated channel/name/slug.
func (h *Handler) insertRow(ctx context.Context, id, tenantID, channel, name, slug string, envelope []byte) error {
	if h.pool == nil {
		return errors.New("inbound: pool not configured")
	}
	_, err := h.pool.Exec(
		ctx,
		`INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE)`,
		id, tenantID, channel, name, slug, envelope,
	)
	return err
}

// handleInsertErr maps DB errors at insert time. The unique constraint
// on (tenant_id, channel, slug) is the only operator-actionable case;
// everything else is internal.
func (h *Handler) insertErr(ctx context.Context, where, tenantID string, err error) error {
	if isUniqueViolation(err) {
		logext.Warnf(ctx, "[%s] reject: slug conflict,tenant_id:%s", where, tenantID)
		return dispatcher.NewError(http.StatusConflict, attunev1.ErrorCode_CONFLICT,
			"another inbound source with the same name already exists; pick a different name")
	}
	logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,err:%+v", where, tenantID, err.Error())
	return dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create inbound source")
}

// isUniqueViolation — pgx unique-constraint check. Used at insert time
// so duplicate (tenant_id, channel, slug) returns 409 not 500.
func isUniqueViolation(err error) bool {
	type sqlState interface{ SQLState() string }
	var st sqlState
	return errors.As(err, &st) && st.SQLState() == "23505"
}
