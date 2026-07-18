// SPDX-License-Identifier: Apache-2.0

package requestnotification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repo) ResolvePublicRequest(ctx context.Context, tenantSlug string, publicSlug string) (PublicRequestRef, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT t.id, prp.request_id, prp.public_slug, prp.public_title, prp.public_state
		FROM tenants t
		JOIN public_visibility_policies pol
		  ON pol.tenant_id = t.id
		JOIN public_request_profiles prp
		  ON prp.tenant_id = t.id
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		WHERE t.slug = $1
		  AND t.is_active = TRUE
		  AND pol.requests_enabled = TRUE
		  AND prp.public_slug = $2
		  AND prp.included_in_portal = TRUE
		  AND pms.state = 'approved'
		  AND cr.archived_at IS NULL
		  AND cr.merged_into_request_id IS NULL
		LIMIT 1`, tenantSlug, publicSlug)
	var ref PublicRequestRef
	err := row.Scan(&ref.TenantID, &ref.RequestID, &ref.PublicSlug, &ref.PublicTitle, &ref.PublicState)
	if err != nil {
		return PublicRequestRef{}, mapNotFound(err)
	}
	return ref, nil
}

func (r *Repo) ResolveTenantIDBySlug(ctx context.Context, tenantSlug string) (string, error) {
	var tenantID string
	err := r.pool.QueryRow(ctx, `
		SELECT id
		FROM tenants
		WHERE slug = $1
		  AND is_active = TRUE
		LIMIT 1`, tenantSlug).Scan(&tenantID)
	if err != nil {
		return "", mapNotFound(err)
	}
	return tenantID, nil
}

func (r *Repo) GetRequestSummary(ctx context.Context, tenantID string, requestID uuid.UUID) (RequestSummary, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, display_id, title, description, status
		FROM customer_requests
		WHERE tenant_id = $1
		  AND id = $2
		  AND archived_at IS NULL
		  AND merged_into_request_id IS NULL`, tenantID, requestID)
	var out RequestSummary
	err := row.Scan(&out.ID, &out.DisplayID, &out.Title, &out.Description, &out.Status)
	if err != nil {
		return RequestSummary{}, mapNotFound(err)
	}
	return out, nil
}

func (r *Repo) GetEventContext(ctx context.Context, eventID uuid.UUID) (EventContext, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT t.slug, cr.id, cr.display_id, cr.title, cr.description, cr.status,
		 p.id, p.title, p.body, p.kind
		FROM customer_request_notification_events e
		JOIN tenants t
		  ON t.id = e.tenant_id
		JOIN customer_requests cr
		  ON cr.tenant_id = e.tenant_id
		 AND cr.id = e.primary_request_id
		LEFT JOIN public_update_posts p
		  ON p.tenant_id = e.tenant_id
		 AND p.id = e.update_id
		WHERE e.id = $1`, eventID)
	var out EventContext
	err := row.Scan(
		&out.TenantSlug,
		&out.Request.ID,
		&out.Request.DisplayID,
		&out.Request.Title,
		&out.Request.Description,
		&out.Request.Status,
		&out.UpdateID,
		&out.UpdateTitle,
		&out.UpdateBody,
		&out.UpdateKind,
	)
	if err != nil {
		return EventContext{}, mapNotFound(err)
	}
	return out, nil
}

func (r *Repo) CreatePublicUpdateEventTx(ctx context.Context, tx pgx.Tx, in PublicUpdateInput) (Event, error) {
	if in.Kind == "" {
		in.Kind = "status_change"
	}
	if in.EventType == "" {
		if in.Kind == "changelog_post" {
			in.EventType = EventTypeChangelog
		} else {
			in.EventType = EventTypeStatusChanged
		}
	}
	threadID, err := insertPublicUpdateThread(ctx, tx, in)
	if err != nil {
		return Event{}, err
	}
	updateID, err := insertPublicUpdatePost(ctx, tx, threadID, in)
	if err != nil {
		return Event{}, err
	}
	if err := insertPublicUpdateRequestLink(ctx, tx, updateID, in); err != nil {
		return Event{}, err
	}
	return insertNotificationEvent(ctx, tx, updateID, in)
}

func (r *Repo) ClaimEvents(ctx context.Context, limit int, owner string) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE customer_request_notification_events
		 SET status = 'resolving',
		     attempts = attempts + 1,
		     claimed_at = NOW(),
		     claimed_by = $2
		 WHERE id IN (
			SELECT id
			FROM customer_request_notification_events
			WHERE status IN ('pending', 'failed')
			  AND next_retry_at <= NOW()
			  AND (claimed_at IS NULL OR claimed_at < NOW() - INTERVAL '10 minutes')
			ORDER BY next_retry_at ASC, created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, tenant_id, primary_request_id, update_id, direct_followup_id,
		 event_type, audience_scope, dedupe_key, old_status, new_status,
		 actor_type, actor_id, status, attempts, recipient_snapshot, created_at`,
		boundedLimit(limit), owner)
	if err != nil {
		return nil, fmt.Errorf("claim request notification events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (r *Repo) MarkEventResolved(ctx context.Context, id uuid.UUID, owner string, snapshot map[string]any) error {
	raw, err := jsonObject(snapshot)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE customer_request_notification_events
		 SET status = 'resolved',
		     resolved_at = NOW(),
		     recipient_snapshot = $3,
		     claimed_at = NULL,
		     claimed_by = '',
		     last_error = ''
		 WHERE id = $1 AND claimed_by = $2`, id, owner, raw)
	if err != nil {
		return fmt.Errorf("mark request notification event resolved: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) MarkEventFailed(ctx context.Context, id uuid.UUID, owner string, errMsg string, delay time.Duration) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE customer_request_notification_events
		 SET status = 'failed',
		     last_error = $3,
		     next_retry_at = NOW() + make_interval(secs => $4),
		     claimed_at = NULL,
		     claimed_by = ''
		 WHERE id = $1 AND claimed_by = $2`, id, owner, errMsg, int(delay.Seconds()))
	if err != nil {
		return fmt.Errorf("mark request notification event failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func insertPublicUpdateThread(ctx context.Context, tx pgx.Tx, in PublicUpdateInput) (uuid.UUID, error) {
	var id uuid.UUID
	surface := "request_update"
	if strings.TrimSpace(in.Kind) == "changelog_post" {
		surface = "changelog_post"
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO public_update_threads (
			tenant_id, surface, state, created_by
		) VALUES (
			$1, $3, 'published', $2
		)
		RETURNING id`, in.TenantID, actorID(in.ActorID), surface).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert public update thread: %w", err)
	}
	return id, nil
}

func insertPublicUpdatePost(ctx context.Context, tx pgx.Tx, threadID uuid.UUID, in PublicUpdateInput) (uuid.UUID, error) {
	var id uuid.UUID
	hash := contentHash(in.Title, in.Body, in.OldStatus, in.NewStatus)
	err := tx.QueryRow(ctx, `
		INSERT INTO public_update_posts (
			tenant_id, thread_id, kind, state, title, body,
			notify_subscribers, content_hash, published_by,
			published_at, created_by
		) VALUES (
			$1, $2, $3, 'published', $4, $5,
			$6, $7, $8, NOW(), $8
		)
		RETURNING id`,
		in.TenantID,
		threadID,
		in.Kind,
		in.Title,
		in.Body,
		in.Notify,
		hash,
		actorID(in.ActorID),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert public update post: %w", err)
	}
	return id, nil
}

func insertPublicUpdateRequestLink(ctx context.Context, tx pgx.Tx, updateID uuid.UUID, in PublicUpdateInput) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO public_update_request_links (
			tenant_id, update_id, request_id, role, old_status, new_status
		) VALUES (
			$1, $2, $3, 'primary', $4, $5
		)`, in.TenantID, updateID, in.RequestID, in.OldStatus, in.NewStatus)
	if err != nil {
		return fmt.Errorf("insert public update request link: %w", err)
	}
	return nil
}

func insertNotificationEvent(ctx context.Context, tx pgx.Tx, updateID uuid.UUID, in PublicUpdateInput) (Event, error) {
	dedupeKey := strings.TrimSpace(in.DedupeKey)
	if dedupeKey == "" {
		dedupeKey = "public-update:" + updateID.String()
	}
	snapshot := channelsSnapshot(in.Channels)
	row := tx.QueryRow(ctx, `
		INSERT INTO customer_request_notification_events (
			tenant_id, primary_request_id, update_id, event_type,
			audience_scope, dedupe_key, old_status, new_status,
			actor_type, actor_id, status, recipient_snapshot
		) VALUES (
			$1, $2, $3, $4, 'public_broadcast', $5, $6, $7,
			$8, $9, 'pending', $10
		)
		ON CONFLICT (tenant_id, dedupe_key) DO UPDATE SET
			last_error = ''
		RETURNING id, tenant_id, primary_request_id, update_id, direct_followup_id,
		 event_type, audience_scope, dedupe_key, old_status, new_status,
		 actor_type, actor_id, status, attempts, recipient_snapshot, created_at`,
		in.TenantID,
		in.RequestID,
		updateID,
		in.EventType,
		dedupeKey,
		in.OldStatus,
		in.NewStatus,
		actorType(in.ActorType),
		actorID(in.ActorID),
		snapshot,
	)
	return scanEvent(row)
}

func channelsSnapshot(channels []string) []byte {
	var b strings.Builder
	b.WriteString(`{"channels":[`)
	for idx, channel := range channels {
		if idx > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(channel))
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	var snapshot []byte
	err := row.Scan(
		&e.ID,
		&e.TenantID,
		&e.PrimaryRequestID,
		&e.UpdateID,
		&e.DirectFollowupID,
		&e.EventType,
		&e.AudienceScope,
		&e.DedupeKey,
		&e.OldStatus,
		&e.NewStatus,
		&e.ActorType,
		&e.ActorID,
		&e.Status,
		&e.Attempts,
		&snapshot,
		&e.CreatedAt,
	)
	if err != nil {
		return Event{}, mapNotFound(err)
	}
	decoded, err := decodeObject(snapshot)
	if err != nil {
		return Event{}, err
	}
	e.RecipientSnapshot = decoded
	return e, nil
}

func contentHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func actorID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "system"
	}
	return value
}

func actorType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "system"
	}
	return value
}
