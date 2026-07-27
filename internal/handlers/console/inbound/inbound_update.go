// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound/adapter/intercom"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"

	inboundfw "github.com/Phixsura/attune/internal/inbound"
)

// sourceRenamer is the optional store facet the update handler needs
// beyond inbound.SourceStore (same assertion pattern as the adapter's
// intercomConfigUpdater). *inboundsource.Repo implements it.
type sourceRenamer interface {
	UpdateName(ctx context.Context, id, name string) error
}

func (h *Handler) renameSource(ctx context.Context, id, name string) error {
	r, ok := h.sources.(sourceRenamer)
	if !ok {
		return errUpdateUnsupported
	}
	return r.UpdateName(ctx, id, name)
}

var errUpdateUnsupported = errUnsupported("source store does not support updates")

type errUnsupported string

func (e errUnsupported) Error() string { return string(e) }

// Update handles PATCH /fb/v1/console/inbound/sources/{id}: in-place
// edits of a source's mutable settings. Delete/recreate is the wrong
// tool for a settings change — it resets the sync watermark (full
// re-backfill) and orphans existing feedback's inbound_source_id
// linkage.
//
// All validation legs run BEFORE the first write so a rejected request
// never leaves a partially-applied (and unaudited) mutation behind.
func (h *Handler) Update(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	const where = "console.inbound.Update"
	auth := ctx.Auth
	src, err := h.getOwnedSource(ctx, auth, req.GetId(), where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}

	// Validation phase — no writes yet.
	name, upd, changes, err := h.validateUpdate(ctx, auth, src, req, where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}

	// Write phase — config first (its failure leaves nothing behind),
	// rename second.
	if upd != nil {
		if err := h.persistIntercomUpdate(ctx, src.ID, ptrext.Indirect(upd)); err != nil {
			logext.Errorf(ctx, "[%s] persist config failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
			return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to persist source config")
		}
	}
	if name != "" {
		if err := h.renameSource(ctx, src.ID, name); err != nil {
			logext.Errorf(ctx, "[%s] rename failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
			// The config leg (if any) already persisted — audit it
			// before failing so the partial write is never invisible.
			h.auditUpdateBestEffort(ctx, auth, src, changes, where)
			return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to rename source")
		}
		changes["name"] = name
	}

	updated, err := h.sources.Get(ctx, src.ID)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-update reload failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
		return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "update ok but reload failed")
	}
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.update", updated.ID, "Updated inbound source settings", ctx.Request(), map[string]any{
		"id":   src.ID,
		"name": src.Name,
	}, changes); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, updated.ID, err.Error())
		return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	out := rowToProto(updated)
	h.enrichWithSyncStats(updated, out)
	h.enrichIntercomSettings(updated, out)
	return dispatcher.OK(out)
}

// validateUpdate runs the request-shape checks and the (optional)
// Intercom leg's validations without writing anything. Returns the
// effective new name ("" when not renaming), the validated settings
// update (nil when no config leg), and the audit changes map.
func (h *Handler) validateUpdate(ctx *dispatcher.RequestContext[*session.AuthCtx], auth *session.AuthCtx, src inboundfw.Source, req *attunev1.UpdateInboundSourceRequest, where string) (string, *intercom.SettingsUpdate, map[string]any, error) {
	name := strings.TrimSpace(req.GetName())
	if name == src.Name {
		name = ""
	}
	if len(name) > 200 {
		// Same cap as the create path; the column is unconstrained TEXT.
		return "", nil, nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "name must be 200 characters or fewer")
	}
	cfg := req.GetIntercomConfig()
	if cfg != nil && src.Channel != channelIntercom {
		return "", nil, nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "intercom_config only applies to intercom sources")
	}
	if name == "" && cfg == nil {
		return "", nil, nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "nothing to update")
	}
	changes := map[string]any{}
	var upd *intercom.SettingsUpdate
	if cfg != nil {
		var err error
		upd, err = h.validateIntercomUpdate(ctx, auth, src, cfg, changes, where)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return name, upd, changes, nil
}

// validateIntercomUpdate runs every Intercom-leg validation without
// writing anything: region immutability, token auth-test + workspace
// pinning, and a dry-run settings merge that surfaces filter/state
// validation errors. The returned SettingsUpdate is what the write
// phase re-applies against a fresh config read.
//
// A new access token is verified against Intercom AND pinned to the
// stored workspace before it replaces the old one — a token for a
// different workspace would corrupt idempotency keys and permalinks
// minted under the stored workspace ID.
func (h *Handler) validateIntercomUpdate(ctx *dispatcher.RequestContext[*session.AuthCtx], auth *session.AuthCtx, src inboundfw.Source, cfg *attunev1.IntercomConnConfig, changes map[string]any, where string) (*intercom.SettingsUpdate, error) {
	summary, err := intercom.DecodeConnSummary(src.Config, h.secrets)
	if err != nil {
		logext.Errorf(ctx, "[%s] decode config failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
		return nil, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to decode source config")
	}
	if region := strings.TrimSpace(cfg.GetRegion()); region != "" && !strings.EqualFold(region, summary.Region) {
		return nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
			"region is immutable — create a new source to move workspaces")
	}

	if token := strings.TrimSpace(cfg.GetAccessToken()); token != "" {
		authTest := h.intercomAuthTest
		if authTest == nil {
			authTest = intercom.AuthTest
		}
		acct, aerr := authTest(ctx, summary.Region, token)
		if aerr != nil {
			return nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, friendlyIntercomError(aerr))
		}
		if summary.WorkspaceID != "" && acct.WorkspaceID != summary.WorkspaceID {
			return nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION,
				"the new token belongs to a different Intercom workspace — create a new source instead")
		}
		changes["intercom_token_rotated"] = true
	}

	upd := intercom.SettingsUpdate{
		AccessToken:       cfg.GetAccessToken(),
		FilterStates:      cfg.GetFilterStates(),
		FilterTags:        cfg.GetFilterTags(),
		FilterExcludeTags: cfg.GetFilterExcludeTags(),
	}
	// Presence-aware optionals: absent keeps the stored value ("PATCH
	// omits it" must not degrade to "reset to default").
	if cfg.StartFrom != nil {
		upd.StartFrom = ptrext.Of(cfg.GetStartFrom())
	}
	if cfg.MaxDetailFetches != nil {
		upd.MaxDetailFetches = ptrext.Of(int(cfg.GetMaxDetailFetches()))
	}
	// Dry-run merge against the current blob so validation errors (bad
	// filter state, negative budget) reject the request before any write.
	if _, err := intercom.ApplySettingsUpdate(src.Config, h.secrets, upd); err != nil {
		return nil, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	}
	changes["intercom_filter_states"] = cfg.GetFilterStates()
	changes["intercom_filter_tags"] = cfg.GetFilterTags()
	changes["intercom_filter_exclude_tags"] = cfg.GetFilterExcludeTags()
	return ptrext.Of(upd), nil
}

// persistIntercomUpdate applies the validated settings to a FRESH
// config read and writes it back, all under the secretlock advisory
// lock + a row lock:
//   - EnsureWritableKey keeps a possibly-new encrypted token off a key
//     a concurrent DB-wide rotation is retiring (same invariant as
//     every create/rotate path).
//   - SELECT ... FOR UPDATE re-reads the blob inside the transaction so
//     a poll tick's cursor/stats persisted after the handler's initial
//     read are merged, not clobbered (the poller's own write uses a
//     compare-and-swap that yields to this row lock).
func (h *Handler) persistIntercomUpdate(ctx context.Context, sourceID string, upd intercom.SettingsUpdate) error {
	withTx := h.intercomWithTx
	if withTx == nil {
		withTx = secretlock.WithTx
	}
	return withTx(ctx, h.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, inboundfw.PrimaryKeyID(h.secrets)); err != nil {
			return err
		}
		var fresh []byte
		if err := tx.QueryRow(ctx, `SELECT config FROM inbound_sources WHERE id = $1 FOR UPDATE`, sourceID).Scan(&fresh); err != nil {
			return err
		}
		blob, err := intercom.ApplySettingsUpdate(fresh, h.secrets, upd)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE inbound_sources SET config = $2, updated_at = now() WHERE id = $1`, sourceID, blob)
		return err
	})
}

// auditUpdateBestEffort records whatever part of the update DID persist
// when a later write leg fails — a persisted mutation must never be
// invisible to the audit trail.
func (h *Handler) auditUpdateBestEffort(ctx *dispatcher.RequestContext[*session.AuthCtx], auth *session.AuthCtx, src inboundfw.Source, changes map[string]any, where string) {
	if len(changes) == 0 {
		return
	}
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.update", src.ID, "Updated inbound source settings (partial)", ctx.Request(), map[string]any{
		"id":   src.ID,
		"name": src.Name,
	}, changes); err != nil {
		logext.Errorf(ctx, "[%s] partial-update audit write failed,tenant_id:%s,id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
	}
}
