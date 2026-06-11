// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (h *Handler) insertRowTx(
	ctx context.Context,
	tx secretlock.Tx,
	id, tenantID, channel, name, slug string,
	envelope []byte,
) error {
	_, err := tx.Exec(
		ctx,
		insertRowSQL,
		id, tenantID, channel, name, slug, envelope,
	)
	return err
}

const insertRowSQL = `
	INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
	VALUES ($1, $2, $3, $4, $5, $6, TRUE)`

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
