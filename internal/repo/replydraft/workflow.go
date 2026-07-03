// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	StatusSuggested   = "suggested"
	StatusEdited      = "edited"
	StatusApproved    = "approved"
	StatusSendPending = "send_pending"
	StatusSendFailed  = "send_failed"
	StatusRejected    = "rejected"
	StatusSent        = "sent"
	StatusStale       = "stale"

	DeliveryEventReplySend = "reply.send"
	DeliveryEventReplyTest = "reply.test"
	DeliveryStatusPending  = "pending"
	DeliveryStatusAccepted = "accepted"
	DeliveryStatusFailed   = "failed"
	DeliveryStatusDead     = "dead"
)

var (
	ErrDraftNotFound       = errors.New("reply draft not found")
	ErrInvalidDraftState   = errors.New("reply draft state does not allow action")
	ErrHookNotFound        = errors.New("reply send hook not found")
	ErrDeliveryNotFound    = errors.New("reply delivery attempt not found")
	ErrAlreadySent         = errors.New("reply draft already sent")
	ErrRequestInProgress   = errors.New("reply send request already in progress")
	ErrIdempotencyConflict = errors.New("reply send idempotency key reused with a different request")
	ErrStaleDraft          = errors.New("reply draft source changed")
	ErrRevisionConflict    = errors.New("reply draft revision conflict")
)

const maxReplyDeliveryAttempts = 8

type Actor struct {
	Type string
	ID   string
}

type Draft struct {
	ID                      string
	TenantID                string
	FeedbackID              int64
	CycleNo                 int
	Status                  string
	ActiveRevisionID        string
	ApprovedRevisionID      string
	SentRevisionID          string
	ApprovedHookID          string
	ApprovedHookFingerprint string
	ActiveContent           string
	SourceFingerprint       string
	LastBlocker             string
	ExternalDeliveryStatus  string
	ExternalMessageID       string
	GeneratedAt             *time.Time
	GeneratedBy             string
	EditedAt                *time.Time
	EditedBy                string
	ApprovedAt              *time.Time
	ApprovedBy              string
	RejectedAt              *time.Time
	RejectedBy              string
	SentAt                  *time.Time
	SentBy                  string
	Revision                int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type Revision struct {
	ID                string
	DraftID           string
	TenantID          string
	FeedbackID        int64
	CycleNo           int
	RevisionNo        int
	Origin            string
	Content           string
	SourceFingerprint string
	Metadata          []byte
	CreatedBy         string
	CreatedAt         time.Time
}

type Event struct {
	ID         string
	DraftID    string
	TenantID   string
	FeedbackID int64
	RevisionID string
	HookID     string
	EventType  string
	ActorType  string
	ActorID    string
	Blocker    string
	Metadata   []byte
	CreatedAt  time.Time
}

type Hook struct {
	ID               string
	TenantID         string
	Name             string
	URLCiphertext    []byte
	URLKeyID         string
	URLFingerprint   string
	URLHost          string
	SecretCiphertext []byte
	SecretKeyID      string
	Enabled          bool
	CreatedBy        string
	UpdatedBy        string
	DisabledAt       sql.NullTime
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type HookUpsert struct {
	TenantID         string
	Name             string
	URLCiphertext    []byte
	URLKeyID         string
	URLFingerprint   string
	URLHost          string
	SecretCiphertext []byte
	SecretKeyID      string
	Enabled          bool
	ActorID          string
}

type DeliveryPrepare struct {
	AttemptID      string
	Draft          Draft
	Hook           Hook
	Revision       Revision
	IdempotencyKey string
	EventType      string
	Actor          Actor
	FromCache      bool
}

type DeliveryAttempt struct {
	ID                string
	TenantID          string
	DraftID           string
	FeedbackID        int64
	HookID            string
	HookHost          string
	HookFingerprint   string
	RevisionID        string
	EventType         string
	IdempotencyKey    string
	Status            string
	HTTPStatus        int
	Attempts          int
	MaxAttempts       int
	NextRetryAt       *time.Time
	ExternalMessageID string
	Error             string
	RequestedByType   string
	RequestedBy       string
	RequestedAt       time.Time
	CompletedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DeliveryHealth struct {
	Total         int64
	Accepted      int64
	Failed        int64
	Dead          int64
	Pending       int64
	Retryable     int64
	Latest        *DeliveryAttempt
	LatestProblem *DeliveryAttempt
}

// StoreGeneratedDraft persists an AI-generated draft as a revision and keeps
// the legacy user_feedback.reply_draft projection in sync.
func (r *DraftTaskRepo) StoreGeneratedDraft(ctx context.Context, feedbackID int64, tenantID, draft, actorID string) (time.Time, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin generated draft: %w", err)
	}
	defer rollback(ctx, tx)
	generatedAt, err := r.storeGeneratedDraftTx(ctx, tx, feedbackID, tenantID, draft, actorID)
	if err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit generated draft: %w", err)
	}
	return generatedAt, nil
}

func (r *DraftTaskRepo) storeGeneratedDraftTx(
	ctx context.Context, tx pgx.Tx, feedbackID int64, tenantID, content, actorID string,
) (time.Time, error) {
	snapshot, err := r.feedbackSnapshotTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return time.Time{}, err
	}
	draft, err := r.ensureWritableDraftTx(ctx, tx, tenantID, feedbackID, snapshot)
	if err != nil {
		return time.Time{}, err
	}
	rev, err := r.insertRevisionTx(ctx, tx, draft, "ai", content, snapshot.Metadata, actorID)
	if err != nil {
		return time.Time{}, err
	}
	generatedAt, err := r.markGeneratedTx(ctx, tx, draft.ID, rev.ID, content, actorID, snapshot)
	if err != nil {
		return time.Time{}, err
	}
	if err := r.insertEventTx(ctx, tx, draft, rev.ID, "", "generate", Actor{Type: "system", ID: actorID}, "", nil); err != nil {
		return time.Time{}, err
	}
	return generatedAt, nil
}

type feedbackSnapshot struct {
	Fingerprint string
	Metadata    []byte
}

func (r *DraftTaskRepo) feedbackSnapshotTx(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (feedbackSnapshot, error) {
	var content, title, rationale, language, source, userID, status string
	var attrs, sourceMeta []byte
	err := tx.QueryRow(ctx, `
		SELECT content, source, user_id, COALESCE(source_meta, '{}'::jsonb),
		       COALESCE(enriched_title, ''), COALESCE(enriched_rationale, ''),
		       COALESCE(enriched_attrs, '{}'::jsonb), COALESCE(language, ''), enrichment_status
		FROM user_feedback
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, feedbackID,
	).Scan(&content, &source, &userID, &sourceMeta, &title, &rationale, &attrs, &language, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return feedbackSnapshot{}, ErrNotFound
	}
	if err != nil {
		return feedbackSnapshot{}, fmt.Errorf("load feedback snapshot: %w", err)
	}
	meta, err := json.Marshal(map[string]string{
		"enrichment_status": status,
		"language":          language,
		"source":            source,
	})
	if err != nil {
		return feedbackSnapshot{}, fmt.Errorf("marshal feedback snapshot: %w", err)
	}
	return feedbackSnapshot{
		Fingerprint: fingerprint(content, source, userID, string(sourceMeta), title, rationale, string(attrs), language, status),
		Metadata:    meta,
	}, nil
}

func (r *DraftTaskRepo) ensureWritableDraftTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, snapshot feedbackSnapshot,
) (Draft, error) {
	draft, err := r.loadActiveDraftForUpdateTx(ctx, tx, tenantID, feedbackID)
	if errors.Is(err, ErrDraftNotFound) {
		return r.insertDraftCycleTx(ctx, tx, tenantID, feedbackID, 1, snapshot)
	}
	if err != nil {
		return Draft{}, err
	}
	if draft.Status == StatusSendPending {
		return Draft{}, ErrRequestInProgress
	}
	if draft.Status != StatusSent && draft.Status != StatusRejected {
		return draft, nil
	}
	if err := r.archiveDraftTx(ctx, tx, draft.ID); err != nil {
		return Draft{}, err
	}
	return r.insertDraftCycleTx(ctx, tx, tenantID, feedbackID, draft.CycleNo+1, snapshot)
}

func (r *DraftTaskRepo) insertDraftCycleTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, cycleNo int, snapshot feedbackSnapshot,
) (Draft, error) {
	var draft Draft
	err := tx.QueryRow(ctx, `
		INSERT INTO reply_drafts (tenant_id, feedback_id, cycle_no, status, source_fingerprint, source_meta)
		VALUES ($1, $2, $3, 'suggested', $4, $5::jsonb)
		RETURNING id, tenant_id, feedback_id, cycle_no, status, source_fingerprint, last_blocker,
		          external_delivery_status, external_message_id, revision, created_at, updated_at`,
		tenantID, feedbackID, cycleNo, snapshot.Fingerprint, snapshot.Metadata,
	).Scan(&draft.ID, &draft.TenantID, &draft.FeedbackID, &draft.CycleNo, &draft.Status,
		&draft.SourceFingerprint, &draft.LastBlocker, &draft.ExternalDeliveryStatus,
		&draft.ExternalMessageID, &draft.Revision, &draft.CreatedAt, &draft.UpdatedAt)
	if err != nil {
		return Draft{}, fmt.Errorf("insert reply draft cycle: %w", err)
	}
	return draft, nil
}

func (r *DraftTaskRepo) archiveDraftTx(ctx context.Context, tx pgx.Tx, draftID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET archived_at = NOW(), updated_at = NOW(), revision = revision + 1
		WHERE id = $1::uuid`, draftID); err != nil {
		return fmt.Errorf("archive reply draft: %w", err)
	}
	return nil
}

func (r *DraftTaskRepo) insertRevisionTx(
	ctx context.Context, tx pgx.Tx, draft Draft, origin, content string, metadata []byte, actorID string,
) (Revision, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Revision{}, ErrInvalidDraftState
	}
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}
	var rev Revision
	err := tx.QueryRow(ctx, `
		INSERT INTO reply_draft_revisions (
		    draft_id, tenant_id, feedback_id, cycle_no, revision_no, origin, content,
		    content_sha256, source_fingerprint, metadata, created_by
		)
		SELECT $1::uuid, $2, $3, $4,
		       COALESCE(MAX(revision_no), 0) + 1,
		       $5, $6, $7, $8, $9::jsonb, $10
		FROM reply_draft_revisions
		WHERE draft_id = $1::uuid
		RETURNING id, draft_id, tenant_id, feedback_id, cycle_no, revision_no, origin,
		          content, source_fingerprint, metadata, created_by, created_at`,
		draft.ID, draft.TenantID, draft.FeedbackID, draft.CycleNo, origin, content,
		sha256Bytes(content), draft.SourceFingerprint, metadata, actorID,
	).Scan(&rev.ID, &rev.DraftID, &rev.TenantID, &rev.FeedbackID, &rev.CycleNo,
		&rev.RevisionNo, &rev.Origin, &rev.Content, &rev.SourceFingerprint,
		&rev.Metadata, &rev.CreatedBy, &rev.CreatedAt)
	if err != nil {
		return Revision{}, fmt.Errorf("insert reply draft revision: %w", err)
	}
	return rev, nil
}

func (r *DraftTaskRepo) markGeneratedTx(
	ctx context.Context, tx pgx.Tx, draftID, revisionID, content, actorID string, snapshot feedbackSnapshot,
) (time.Time, error) {
	var generatedAt time.Time
	err := tx.QueryRow(ctx, `
		UPDATE reply_drafts
		SET status = 'suggested',
		    active_revision_id = $2::uuid,
		    approved_revision_id = NULL,
		    sent_revision_id = NULL,
		    approved_hook_id = NULL,
		    approved_hook_fingerprint = '',
		    sent_hook_id = NULL,
		    source_fingerprint = $4,
		    source_meta = $5::jsonb,
		    last_blocker = '',
		    external_delivery_status = '',
		    external_message_id = '',
		    generated_at = NOW(),
		    generated_by = $3,
		    approved_at = NULL,
		    approved_by = '',
		    rejected_at = NULL,
		    rejected_by = '',
		    sent_at = NULL,
		    sent_by = '',
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid
		RETURNING generated_at`,
		draftID, revisionID, actorID, snapshot.Fingerprint, snapshot.Metadata,
	).Scan(&generatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("mark generated draft: %w", err)
	}
	return generatedAt, r.syncLegacyDraftTx(ctx, tx, draftID, content, generatedAt)
}

func (r *DraftTaskRepo) syncLegacyDraftTx(ctx context.Context, tx pgx.Tx, draftID, content string, generatedAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE user_feedback f
		SET reply_draft = $2, reply_draft_generated_at = $3
		FROM reply_drafts d
		WHERE d.id = $1::uuid
		  AND f.id = d.feedback_id
		  AND f.tenant_id = d.tenant_id`,
		draftID, content, generatedAt,
	)
	if err != nil {
		return fmt.Errorf("sync legacy reply draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *DraftTaskRepo) syncLegacyDraftContentTx(ctx context.Context, tx pgx.Tx, draftID, content string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE user_feedback f
		SET reply_draft = $2
		FROM reply_drafts d
		WHERE d.id = $1::uuid
		  AND f.id = d.feedback_id
		  AND f.tenant_id = d.tenant_id`,
		draftID, content,
	)
	if err != nil {
		return fmt.Errorf("sync legacy reply draft content: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *DraftTaskRepo) insertEventTx(
	ctx context.Context,
	tx pgx.Tx,
	draft Draft,
	revisionID string,
	hookID string,
	eventType string,
	actor Actor,
	blocker string,
	metadata []byte,
) error {
	if len(metadata) == 0 {
		metadata = []byte("{}")
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO reply_draft_events (
		    draft_id, tenant_id, feedback_id, revision_id, hook_id, event_type,
		    actor_type, actor_id, blocker, metadata
		)
		VALUES (
		    $1::uuid, $2, $3, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid,
		    $6, $7, $8, $9, $10::jsonb
		)`,
		draft.ID, draft.TenantID, draft.FeedbackID, revisionID, hookID, eventType,
		actor.Type, actor.ID, blocker, metadata,
	)
	if err != nil {
		return fmt.Errorf("insert reply draft event: %w", err)
	}
	return nil
}

func (r *DraftTaskRepo) loadDraftByIDTx(ctx context.Context, tx pgx.Tx, draftID string) (Draft, error) {
	return scanDraft(tx.QueryRow(ctx, draftByIDSQL(), draftID))
}

func (r *DraftTaskRepo) loadDraftByIDForUpdateTx(ctx context.Context, tx pgx.Tx, draftID string) (Draft, error) {
	return scanDraft(tx.QueryRow(ctx, draftByIDSQL()+` FOR UPDATE OF d`, draftID))
}

func draftByIDSQL() string {
	return `
		SELECT d.id, d.tenant_id, d.feedback_id, d.cycle_no, d.status,
		       d.active_revision_id, d.approved_revision_id, d.sent_revision_id,
		       d.approved_hook_id, d.approved_hook_fingerprint,
		       COALESCE(r.content, ''), d.source_fingerprint, d.last_blocker,
		       d.external_delivery_status, d.external_message_id,
		       d.generated_at, d.generated_by, d.edited_at, d.edited_by,
		       d.approved_at, d.approved_by, d.rejected_at, d.rejected_by,
		       d.sent_at, d.sent_by, d.revision, d.created_at, d.updated_at
		FROM reply_drafts d
		LEFT JOIN reply_draft_revisions r ON r.id = d.active_revision_id
		WHERE d.id = $1::uuid`
}

func fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func deliveryRequestFingerprint(parts ...string) string {
	return fingerprint(parts...)
}

func sha256Bytes(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func (r *DraftTaskRepo) loadActiveDraftForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (Draft, error) {
	return scanDraft(tx.QueryRow(ctx, activeDraftSQL()+` FOR UPDATE OF d`, tenantID, feedbackID))
}

func (r *DraftTaskRepo) GetActiveDraft(ctx context.Context, tenantID string, feedbackID int64) (Draft, error) {
	return scanDraft(r.pool.QueryRow(ctx, activeDraftSQL(), tenantID, feedbackID))
}

func (r *DraftTaskRepo) EditDraft(
	ctx context.Context, tenantID string, feedbackID int64, content string, expectedRevision int64, actor Actor,
) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("begin edit draft: %w", err)
	}
	defer rollback(ctx, tx)
	draft, err := r.editDraftTx(ctx, tx, tenantID, feedbackID, content, expectedRevision, actor)
	if err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, fmt.Errorf("commit edit draft: %w", err)
	}
	return draft, nil
}

func (r *DraftTaskRepo) editDraftTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, content string, expectedRevision int64, actor Actor,
) (Draft, error) {
	draft, err := r.loadActiveDraftForUpdateTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return Draft{}, err
	}
	if err := checkExpectedRevision(draft, expectedRevision); err != nil {
		return Draft{}, err
	}
	if !canEditDraft(draft.Status) || draft.ActiveRevisionID == "" {
		return Draft{}, ErrInvalidDraftState
	}
	rev, err := r.insertRevisionTx(ctx, tx, draft, "human", content, nil, actor.ID)
	if err != nil {
		return Draft{}, err
	}
	if err := r.markEditedTx(ctx, tx, draft.ID, rev.ID, rev.Content, actor); err != nil {
		return Draft{}, err
	}
	if err := r.insertEventTx(ctx, tx, draft, rev.ID, "", "edit", actor, "", nil); err != nil {
		return Draft{}, err
	}
	return r.loadDraftByIDTx(ctx, tx, draft.ID)
}

func canEditDraft(status string) bool {
	return status == StatusSuggested || status == StatusEdited || status == StatusApproved ||
		status == StatusSendFailed || status == StatusStale
}

func (r *DraftTaskRepo) markEditedTx(ctx context.Context, tx pgx.Tx, draftID, revisionID, content string, actor Actor) error {
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'edited',
		    active_revision_id = $2::uuid,
		    approved_revision_id = NULL,
		    sent_revision_id = NULL,
		    approved_hook_id = NULL,
		    approved_hook_fingerprint = '',
		    sent_hook_id = NULL,
		    external_delivery_status = '',
		    external_message_id = '',
		    approved_at = NULL,
		    approved_by = '',
		    edited_at = NOW(),
		    edited_by = $3,
		    sent_at = NULL,
		    sent_by = '',
		    last_blocker = '',
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid`,
		draftID, revisionID, actor.ID,
	); err != nil {
		return fmt.Errorf("mark edited draft: %w", err)
	}
	return r.syncLegacyDraftContentTx(ctx, tx, draftID, content)
}

func (r *DraftTaskRepo) ApproveDraft(
	ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor Actor,
) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("begin approve draft: %w", err)
	}
	defer rollback(ctx, tx)
	draft, err := r.approveDraftTx(ctx, tx, tenantID, feedbackID, expectedRevision, actor)
	if err != nil {
		if errors.Is(err, ErrStaleDraft) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return Draft{}, fmt.Errorf("commit stale draft: %w", commitErr)
			}
		}
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, fmt.Errorf("commit approve draft: %w", err)
	}
	return draft, nil
}

func (r *DraftTaskRepo) approveDraftTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, expectedRevision int64, actor Actor,
) (Draft, error) {
	draft, err := r.loadActiveDraftForUpdateTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return Draft{}, err
	}
	if err := checkExpectedRevision(draft, expectedRevision); err != nil {
		return Draft{}, err
	}
	if !canApproveDraft(draft.Status) || draft.ActiveRevisionID == "" {
		return Draft{}, ErrInvalidDraftState
	}
	snapshot, err := r.feedbackSnapshotTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return Draft{}, err
	}
	if draft.SourceFingerprint != snapshot.Fingerprint {
		if err := r.markStaleTx(ctx, tx, draft, snapshot, actor, "stale_source"); err != nil {
			return Draft{}, err
		}
		return Draft{}, ErrStaleDraft
	}
	hook, err := r.loadActiveHookTx(ctx, tx, tenantID)
	if err != nil {
		return Draft{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'approved',
		    approved_revision_id = active_revision_id,
		    approved_hook_id = NULLIF($3, '')::uuid,
		    approved_hook_fingerprint = $4,
		    approved_at = NOW(),
		    approved_by = $2,
		    last_blocker = '',
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid`,
		draft.ID, actor.ID, hook.ID, hook.URLFingerprint,
	); err != nil {
		return Draft{}, fmt.Errorf("approve draft: %w", err)
	}
	if err := r.insertEventTx(ctx, tx, draft, draft.ActiveRevisionID, hook.ID, "approve", actor, "", nil); err != nil {
		return Draft{}, err
	}
	return r.loadDraftByIDTx(ctx, tx, draft.ID)
}

func canApproveDraft(status string) bool {
	return status == StatusSuggested || status == StatusEdited
}

func (r *DraftTaskRepo) markStaleTx(
	ctx context.Context, tx pgx.Tx, draft Draft, snapshot feedbackSnapshot, actor Actor, blocker string,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'stale',
		    source_fingerprint = $2,
		    source_meta = $3::jsonb,
		    last_blocker = $4,
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid`,
		draft.ID, snapshot.Fingerprint, snapshot.Metadata, blocker,
	)
	if err != nil {
		return fmt.Errorf("mark stale reply draft: %w", err)
	}
	return r.insertEventTx(ctx, tx, draft, draft.ActiveRevisionID, "", "stale", actor, blocker, nil)
}

func (r *DraftTaskRepo) RejectDraft(
	ctx context.Context, tenantID string, feedbackID int64, expectedRevision int64, actor Actor,
) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("begin reject draft: %w", err)
	}
	defer rollback(ctx, tx)
	draft, err := r.rejectDraftTx(ctx, tx, tenantID, feedbackID, expectedRevision, actor)
	if err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, fmt.Errorf("commit reject draft: %w", err)
	}
	return draft, nil
}

func (r *DraftTaskRepo) rejectDraftTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, expectedRevision int64, actor Actor,
) (Draft, error) {
	draft, err := r.loadActiveDraftForUpdateTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return Draft{}, err
	}
	if err := checkExpectedRevision(draft, expectedRevision); err != nil {
		return Draft{}, err
	}
	if draft.Status == StatusSent || draft.Status == StatusRejected || draft.Status == StatusSendPending {
		return Draft{}, ErrInvalidDraftState
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'rejected',
		    rejected_at = NOW(),
		    rejected_by = $2,
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid`,
		draft.ID, actor.ID,
	); err != nil {
		return Draft{}, fmt.Errorf("reject draft: %w", err)
	}
	if err := r.insertEventTx(ctx, tx, draft, draft.ActiveRevisionID, "", "reject", actor, "", nil); err != nil {
		return Draft{}, err
	}
	return r.loadDraftByIDTx(ctx, tx, draft.ID)
}

func (r *DraftTaskRepo) ListRevisions(ctx context.Context, tenantID string, feedbackID int64) ([]Revision, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, draft_id, tenant_id, feedback_id, cycle_no, revision_no, origin,
		       content, source_fingerprint, metadata, created_by, created_at
		FROM reply_draft_revisions
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY cycle_no DESC, revision_no DESC`,
		tenantID, feedbackID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reply draft revisions: %w", err)
	}
	defer rows.Close()
	return scanRevisions(rows)
}

func scanRevisions(rows pgx.Rows) ([]Revision, error) {
	var out []Revision
	for rows.Next() {
		var rev Revision
		if err := rows.Scan(&rev.ID, &rev.DraftID, &rev.TenantID, &rev.FeedbackID,
			&rev.CycleNo, &rev.RevisionNo, &rev.Origin, &rev.Content,
			&rev.SourceFingerprint, &rev.Metadata, &rev.CreatedBy, &rev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reply draft revision: %w", err)
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func checkExpectedRevision(draft Draft, expectedRevision int64) error {
	if expectedRevision <= 0 || draft.Revision != expectedRevision {
		return ErrRevisionConflict
	}
	return nil
}

func (r *DraftTaskRepo) ListEvents(ctx context.Context, tenantID string, feedbackID int64) ([]Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, draft_id, tenant_id, feedback_id, revision_id, hook_id, event_type,
		       actor_type, actor_id, blocker, metadata, created_at
		FROM reply_draft_events
		WHERE tenant_id = $1 AND feedback_id = $2
		ORDER BY created_at DESC, id DESC`,
		tenantID, feedbackID,
	)
	if err != nil {
		return nil, fmt.Errorf("list reply draft events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		event, err := scanEventRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func scanEventRows(rows pgx.Rows) (Event, error) {
	var event Event
	var revisionID, hookID sql.NullString
	if err := rows.Scan(&event.ID, &event.DraftID, &event.TenantID, &event.FeedbackID,
		&revisionID, &hookID, &event.EventType, &event.ActorType, &event.ActorID,
		&event.Blocker, &event.Metadata, &event.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("scan reply draft event: %w", err)
	}
	event.RevisionID = nullString(revisionID)
	event.HookID = nullString(hookID)
	return event, nil
}

func (r *DraftTaskRepo) UpsertHook(ctx context.Context, in HookUpsert) (Hook, error) {
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "Default reply send hook"
	}
	hook, updated, err := r.updateActiveHook(ctx, in)
	if err != nil || updated {
		return hook, err
	}
	return r.insertHook(ctx, in)
}

func (r *DraftTaskRepo) updateActiveHook(ctx context.Context, in HookUpsert) (Hook, bool, error) {
	var hook Hook
	err := r.pool.QueryRow(ctx, `
		UPDATE reply_send_hooks
		SET name = $2,
		    url_ciphertext = $3,
		    url_key_id = $4,
		    url_fingerprint = $5,
		    url_host = $6,
		    secret_ciphertext = NULLIF($7, ''::bytea),
		    secret_key_id = $8,
		    enabled = $9,
		    updated_by = $10,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND disabled_at IS NULL
		RETURNING id, tenant_id, name, url_ciphertext, url_key_id, url_fingerprint,
		          url_host, secret_ciphertext, secret_key_id, enabled, created_by,
		          updated_by, disabled_at, created_at, updated_at`,
		in.TenantID, in.Name, in.URLCiphertext, in.URLKeyID, in.URLFingerprint,
		in.URLHost, emptyBytesIfNil(in.SecretCiphertext), in.SecretKeyID, in.Enabled, in.ActorID,
	).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, false, nil
	}
	if err != nil {
		return Hook{}, false, fmt.Errorf("update reply send hook: %w", err)
	}
	return hook, true, nil
}

func (r *DraftTaskRepo) insertHook(ctx context.Context, in HookUpsert) (Hook, error) {
	var hook Hook
	err := r.pool.QueryRow(ctx, `
		INSERT INTO reply_send_hooks (
		    tenant_id, name, url_ciphertext, url_key_id, url_fingerprint, url_host,
		    secret_ciphertext, secret_key_id, enabled, created_by, updated_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''::bytea), $8, $9, $10, $10)
		RETURNING id, tenant_id, name, url_ciphertext, url_key_id, url_fingerprint,
		          url_host, secret_ciphertext, secret_key_id, enabled, created_by,
		          updated_by, disabled_at, created_at, updated_at`,
		in.TenantID, in.Name, in.URLCiphertext, in.URLKeyID, in.URLFingerprint,
		in.URLHost, emptyBytesIfNil(in.SecretCiphertext), in.SecretKeyID, in.Enabled, in.ActorID,
	).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if err != nil {
		return Hook{}, fmt.Errorf("insert reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) GetActiveHook(ctx context.Context, tenantID string) (Hook, error) {
	var hook Hook
	err := r.pool.QueryRow(ctx, hookSelectSQL()+`
		WHERE tenant_id = $1 AND enabled = TRUE AND disabled_at IS NULL
		ORDER BY updated_at DESC
		LIMIT 1`, tenantID).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrHookNotFound
	}
	if err != nil {
		return Hook{}, fmt.Errorf("get active reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) GetLatestHook(ctx context.Context, tenantID string) (Hook, error) {
	var hook Hook
	err := r.pool.QueryRow(ctx, hookSelectSQL()+`
		WHERE tenant_id = $1
		ORDER BY (disabled_at IS NOT NULL), updated_at DESC
		LIMIT 1`, tenantID).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrHookNotFound
	}
	if err != nil {
		return Hook{}, fmt.Errorf("get latest reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) DisableHook(ctx context.Context, tenantID, actorID string) (Hook, error) {
	var hook Hook
	err := r.pool.QueryRow(ctx, `
		UPDATE reply_send_hooks
		SET enabled = FALSE, disabled_at = NOW(), updated_by = $2, updated_at = NOW()
		WHERE tenant_id = $1 AND disabled_at IS NULL
		RETURNING id, tenant_id, name, url_ciphertext, url_key_id, url_fingerprint,
		          url_host, secret_ciphertext, secret_key_id, enabled, created_by,
		          updated_by, disabled_at, created_at, updated_at`,
		tenantID, actorID,
	).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrHookNotFound
	}
	if err != nil {
		return Hook{}, fmt.Errorf("disable reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) ListDeliveryAttempts(ctx context.Context, tenantID string, limit int) ([]DeliveryAttempt, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, deliveryAttemptsSelectSQL()+`
		WHERE a.tenant_id = $1
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list reply delivery attempts: %w", err)
	}
	defer rows.Close()
	return scanDeliveryAttempts(rows)
}

func (r *DraftTaskRepo) GetDeliveryHealth(ctx context.Context, tenantID string) (DeliveryHealth, error) {
	var health DeliveryHealth
	err := r.pool.QueryRow(ctx, `
		SELECT
		    COUNT(*),
		    COUNT(*) FILTER (WHERE status = 'accepted'),
		    COUNT(*) FILTER (WHERE status = 'failed'),
		    COUNT(*) FILTER (WHERE status = 'dead'),
		    COUNT(*) FILTER (WHERE status = 'pending'),
		    COUNT(*) FILTER (WHERE status IN ('failed', 'dead'))
		FROM reply_delivery_attempts
		WHERE tenant_id = $1`, tenantID,
	).Scan(&health.Total, &health.Accepted, &health.Failed, &health.Dead, &health.Pending, &health.Retryable)
	if err != nil {
		return DeliveryHealth{}, fmt.Errorf("get reply delivery health: %w", err)
	}
	latest, err := r.latestDeliveryAttempt(ctx, tenantID, false)
	if err != nil {
		return DeliveryHealth{}, err
	}
	latestProblem, err := r.latestDeliveryAttempt(ctx, tenantID, true)
	if err != nil {
		return DeliveryHealth{}, err
	}
	health.Latest = latest
	health.LatestProblem = latestProblem
	return health, nil
}

func (r *DraftTaskRepo) latestDeliveryAttempt(ctx context.Context, tenantID string, problemOnly bool) (*DeliveryAttempt, error) {
	where := "WHERE a.tenant_id = $1"
	if problemOnly {
		where += " AND a.status IN ('failed', 'dead')"
	}
	attempt, err := scanDeliveryAttempt(r.pool.QueryRow(ctx, deliveryAttemptsSelectSQL()+`
		`+where+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT 1`, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(attempt), nil
}

func (r *DraftTaskRepo) GetDeliveryAttempt(ctx context.Context, tenantID string, attemptID string) (DeliveryAttempt, error) {
	attempt, err := scanDeliveryAttempt(r.pool.QueryRow(ctx, deliveryAttemptsSelectSQL()+`
		WHERE a.tenant_id = $1 AND a.id = $2::uuid`, tenantID, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryAttempt{}, ErrDeliveryNotFound
	}
	return attempt, err
}

func (r *DraftTaskRepo) PrepareHookTest(
	ctx context.Context, tenantID string, idempotencyKey string, actor Actor,
) (DeliveryPrepare, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DeliveryPrepare{}, fmt.Errorf("begin prepare hook test: %w", err)
	}
	defer rollback(ctx, tx)
	hook, err := r.loadActiveHookTx(ctx, tx, tenantID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	attemptID, fromCache, err := r.ensureHookTestAttemptTx(ctx, tx, hook, idempotencyKey, actor)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryPrepare{}, fmt.Errorf("commit prepare hook test: %w", err)
	}
	return DeliveryPrepare{
		AttemptID: attemptID, Hook: hook, IdempotencyKey: idempotencyKey,
		EventType: DeliveryEventReplyTest, Actor: actor, FromCache: fromCache,
	}, nil
}

func (r *DraftTaskRepo) ensureHookTestAttemptTx(
	ctx context.Context, tx pgx.Tx, hook Hook, key string, actor Actor,
) (string, bool, error) {
	requestFingerprint := deliveryRequestFingerprint(hook.TenantID, hook.ID, hook.URLFingerprint, DeliveryEventReplyTest, key)
	var attemptID string
	err := tx.QueryRow(ctx, `
		INSERT INTO reply_delivery_attempts (
		    tenant_id, hook_id, event_type, idempotency_key, attempts, max_attempts,
		    request_fingerprint, requested_by_type, requested_by
		)
		VALUES ($1, $2::uuid, $3, $4, 1, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'reply.test'
		DO NOTHING
		RETURNING id`,
		hook.TenantID, hook.ID, DeliveryEventReplyTest, key, maxReplyDeliveryAttempts,
		requestFingerprint, actor.Type, actor.ID,
	).Scan(&attemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.resolveExistingHookTestAttemptTx(ctx, tx, hook, key, requestFingerprint, actor)
	}
	if err != nil {
		return "", false, fmt.Errorf("insert reply hook test attempt: %w", err)
	}
	return attemptID, false, nil
}

func (r *DraftTaskRepo) resolveExistingHookTestAttemptTx(
	ctx context.Context, tx pgx.Tx, hook Hook, key string, requestFingerprint string, actor Actor,
) (string, bool, error) {
	var attemptID, status, existingFingerprint string
	err := tx.QueryRow(ctx, `
		SELECT id, status, request_fingerprint
		FROM reply_delivery_attempts
		WHERE tenant_id = $1 AND event_type = 'reply.test' AND idempotency_key = $2
		FOR UPDATE`,
		hook.TenantID, key,
	).Scan(&attemptID, &status, &existingFingerprint)
	if err != nil {
		return "", false, fmt.Errorf("load reply hook test attempt: %w", err)
	}
	if existingFingerprint != requestFingerprint {
		return "", false, ErrIdempotencyConflict
	}
	switch status {
	case DeliveryStatusAccepted:
		return attemptID, true, nil
	case DeliveryStatusPending:
		return "", false, ErrRequestInProgress
	case DeliveryStatusFailed, DeliveryStatusDead:
		return attemptID, false, r.resetDeliveryAttemptTx(ctx, tx, attemptID, actor)
	default:
		return "", false, fmt.Errorf("unknown reply hook test attempt status %q", status)
	}
}

func (r *DraftTaskRepo) ClaimDueDeliveries(ctx context.Context, limit int, actor Actor) ([]DeliveryPrepare, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim due deliveries: %w", err)
	}
	defer rollback(ctx, tx)
	attempts, err := r.claimableDeliveryAttemptsTx(ctx, tx, limit)
	if err != nil {
		return nil, err
	}
	preps := make([]DeliveryPrepare, 0, len(attempts))
	for _, attempt := range attempts {
		prep, claimed, err := r.claimDueDeliveryTx(ctx, tx, attempt, actor)
		if err != nil {
			return nil, err
		}
		if claimed {
			preps = append(preps, prep)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim due deliveries: %w", err)
	}
	return preps, nil
}

func (r *DraftTaskRepo) claimableDeliveryAttemptsTx(ctx context.Context, tx pgx.Tx, limit int) ([]DeliveryAttempt, error) {
	rows, err := tx.Query(ctx, deliveryAttemptsSelectSQL()+`
		WHERE a.status = 'failed'
		  AND a.event_type = 'reply.send'
		  AND a.next_retry_at <= NOW()
		  AND a.attempts < a.max_attempts
		ORDER BY a.next_retry_at ASC, a.created_at ASC
		LIMIT $1
		FOR UPDATE OF a SKIP LOCKED`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due delivery attempts: %w", err)
	}
	defer rows.Close()
	return scanDeliveryAttempts(rows)
}

func (r *DraftTaskRepo) claimDueDeliveryTx(
	ctx context.Context, tx pgx.Tx, attempt DeliveryAttempt, actor Actor,
) (DeliveryPrepare, bool, error) {
	hook, err := r.loadHookByIDTx(ctx, tx, attempt.HookID)
	if err != nil {
		if errors.Is(err, ErrHookNotFound) {
			return DeliveryPrepare{}, false, r.markDeliveryDeadTx(ctx, tx, attempt.ID, "reply send hook not configured")
		}
		return DeliveryPrepare{}, false, err
	}
	if !hook.Enabled || hook.DisabledAt.Valid {
		return DeliveryPrepare{}, false, r.markDeliveryDeadTx(ctx, tx, attempt.ID, "reply send hook disabled")
	}
	if err := r.ensureRedeliveryFreshTx(ctx, tx, attempt, hook, actor); err != nil {
		if isTerminalDeliveryClaimError(err) {
			return DeliveryPrepare{}, false, r.markDeliveryDeadTx(ctx, tx, attempt.ID, err.Error())
		}
		return DeliveryPrepare{}, false, err
	}
	if err := r.resetDeliveryAttemptTx(ctx, tx, attempt.ID, actor); err != nil {
		return DeliveryPrepare{}, false, err
	}
	prep, err := r.prepareRedeliveryFromAttemptTx(ctx, tx, attempt, hook, actor)
	return prep, err == nil, err
}

func isTerminalDeliveryClaimError(err error) bool {
	return errors.Is(err, ErrStaleDraft) ||
		errors.Is(err, ErrInvalidDraftState) ||
		errors.Is(err, ErrRevisionConflict) ||
		errors.Is(err, ErrHookNotFound)
}

func (r *DraftTaskRepo) PrepareRedelivery(
	ctx context.Context, tenantID string, attemptID string, actor Actor,
) (DeliveryPrepare, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DeliveryPrepare{}, fmt.Errorf("begin prepare redelivery: %w", err)
	}
	defer rollback(ctx, tx)
	attempt, hook, err := r.loadRedeliveryAttemptTx(ctx, tx, tenantID, attemptID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	if err := r.ensureRedeliveryFreshTx(ctx, tx, attempt, hook, actor); err != nil {
		if errors.Is(err, ErrStaleDraft) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return DeliveryPrepare{}, fmt.Errorf("commit stale redelivery block: %w", commitErr)
			}
		}
		return DeliveryPrepare{}, err
	}
	if err := r.resetDeliveryAttemptTx(ctx, tx, attemptID, actor); err != nil {
		return DeliveryPrepare{}, err
	}
	prep, err := r.prepareRedeliveryFromAttemptTx(ctx, tx, attempt, hook, actor)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryPrepare{}, fmt.Errorf("commit prepare redelivery: %w", err)
	}
	return prep, nil
}

func (r *DraftTaskRepo) loadRedeliveryAttemptTx(
	ctx context.Context, tx pgx.Tx, tenantID string, attemptID string,
) (DeliveryAttempt, Hook, error) {
	attempt, err := r.loadDeliveryAttemptByIDForUpdateTx(ctx, tx, tenantID, attemptID)
	if err != nil {
		return DeliveryAttempt{}, Hook{}, err
	}
	if attempt.Status == DeliveryStatusPending {
		return DeliveryAttempt{}, Hook{}, ErrRequestInProgress
	}
	if attempt.Status != DeliveryStatusFailed && attempt.Status != DeliveryStatusDead {
		return DeliveryAttempt{}, Hook{}, ErrInvalidDraftState
	}
	hook, err := r.loadHookByIDTx(ctx, tx, attempt.HookID)
	if err != nil {
		return DeliveryAttempt{}, Hook{}, err
	}
	if !hook.Enabled || hook.DisabledAt.Valid {
		return DeliveryAttempt{}, Hook{}, ErrHookNotFound
	}
	return attempt, hook, nil
}

func (r *DraftTaskRepo) ensureRedeliveryFreshTx(
	ctx context.Context, tx pgx.Tx, attempt DeliveryAttempt, hook Hook, actor Actor,
) error {
	if attempt.EventType == DeliveryEventReplyTest {
		return nil
	}
	return r.ensureReplySendRedeliveryFreshTx(ctx, tx, attempt, hook, actor)
}

func (r *DraftTaskRepo) ensureReplySendRedeliveryFreshTx(
	ctx context.Context, tx pgx.Tx, attempt DeliveryAttempt, hook Hook, actor Actor,
) error {
	if attempt.DraftID == "" || attempt.RevisionID == "" {
		return ErrInvalidDraftState
	}
	draft, err := r.loadDraftByIDTx(ctx, tx, attempt.DraftID)
	if err != nil {
		return err
	}
	if draft.Status != StatusSendFailed {
		return ErrInvalidDraftState
	}
	if draft.ApprovedRevisionID != attempt.RevisionID {
		return ErrRevisionConflict
	}
	if err := r.ensureFreshDeliverySourceTx(ctx, tx, draft.TenantID, draft.FeedbackID, draft, actor); err != nil {
		return err
	}
	return r.ensureFreshApprovedHookTx(ctx, tx, draft.TenantID, draft.FeedbackID, draft, hook, actor)
}

func (r *DraftTaskRepo) prepareRedeliveryFromAttemptTx(
	ctx context.Context, tx pgx.Tx, attempt DeliveryAttempt, hook Hook, actor Actor,
) (DeliveryPrepare, error) {
	if attempt.EventType == DeliveryEventReplyTest {
		return DeliveryPrepare{
			AttemptID: attempt.ID, Hook: hook, IdempotencyKey: attempt.IdempotencyKey,
			EventType: DeliveryEventReplyTest, Actor: actor,
		}, nil
	}
	return r.prepareReplySendRedeliveryTx(ctx, tx, attempt, hook, actor)
}

func (r *DraftTaskRepo) prepareReplySendRedeliveryTx(
	ctx context.Context, tx pgx.Tx, attempt DeliveryAttempt, hook Hook, actor Actor,
) (DeliveryPrepare, error) {
	if attempt.DraftID == "" || attempt.RevisionID == "" {
		return DeliveryPrepare{}, ErrInvalidDraftState
	}
	draft, err := r.loadDraftByIDTx(ctx, tx, attempt.DraftID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	rev, err := r.loadRevisionByIDTx(ctx, tx, attempt.RevisionID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	if err := r.markDeliveryPendingTx(ctx, tx, draft.ID); err != nil {
		return DeliveryPrepare{}, err
	}
	if err := r.insertEventTx(ctx, tx, draft, rev.ID, hook.ID, "send_request", actor, "", nil); err != nil {
		return DeliveryPrepare{}, err
	}
	return DeliveryPrepare{
		AttemptID: attempt.ID, Draft: draft, Hook: hook, Revision: rev,
		IdempotencyKey: attempt.IdempotencyKey, EventType: DeliveryEventReplySend,
		Actor: actor,
	}, nil
}

func hookSelectSQL() string {
	return `
		SELECT id, tenant_id, name, url_ciphertext, url_key_id, url_fingerprint,
		       url_host, secret_ciphertext, secret_key_id, enabled, created_by,
		       updated_by, disabled_at, created_at, updated_at
		FROM reply_send_hooks`
}

func scanHookDest(hook *Hook) []any {
	return []any{
		&hook.ID, &hook.TenantID, &hook.Name, &hook.URLCiphertext, &hook.URLKeyID, // ptrext:allow scan-target
		&hook.URLFingerprint, &hook.URLHost, &hook.SecretCiphertext, &hook.SecretKeyID, // ptrext:allow scan-target
		&hook.Enabled, &hook.CreatedBy, &hook.UpdatedBy, &hook.DisabledAt, // ptrext:allow scan-target
		&hook.CreatedAt, &hook.UpdatedAt, // ptrext:allow scan-target
	}
}

func emptyBytesIfNil(value []byte) []byte {
	if len(value) == 0 {
		return []byte{}
	}
	return value
}

func (r *DraftTaskRepo) PrepareDelivery(
	ctx context.Context, tenantID string, feedbackID int64, idempotencyKey string, expectedRevision int64, actor Actor,
) (DeliveryPrepare, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return DeliveryPrepare{}, fmt.Errorf("begin prepare delivery: %w", err)
	}
	defer rollback(ctx, tx)
	prep, err := r.prepareDeliveryTx(ctx, tx, tenantID, feedbackID, idempotencyKey, expectedRevision, actor)
	if err != nil {
		if errors.Is(err, ErrStaleDraft) {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return DeliveryPrepare{}, fmt.Errorf("commit stale draft: %w", commitErr)
			}
		}
		return DeliveryPrepare{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryPrepare{}, fmt.Errorf("commit prepare delivery: %w", err)
	}
	return prep, nil
}

func (r *DraftTaskRepo) prepareDeliveryTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, key string, expectedRevision int64, actor Actor,
) (DeliveryPrepare, error) {
	draft, err := r.loadActiveDraftForUpdateTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	if draft.Status == StatusSent {
		return r.loadAcceptedAttemptTx(ctx, tx, draft, key)
	}
	if draft.Status == StatusSendPending {
		return DeliveryPrepare{}, ErrRequestInProgress
	}
	if err := checkExpectedRevision(draft, expectedRevision); err != nil {
		return DeliveryPrepare{}, err
	}
	if !canPrepareDelivery(draft.Status) || draft.ApprovedRevisionID == "" {
		return DeliveryPrepare{}, ErrInvalidDraftState
	}
	if err := r.ensureFreshDeliverySourceTx(ctx, tx, tenantID, feedbackID, draft, actor); err != nil {
		return DeliveryPrepare{}, err
	}
	hook, err := r.loadFreshDeliveryHookTx(ctx, tx, tenantID, feedbackID, draft, actor)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	rev, err := r.loadRevisionByIDTx(ctx, tx, draft.ApprovedRevisionID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	attemptID, fromCache, err := r.ensureDeliveryAttemptTx(ctx, tx, draft, hook, rev, key, actor)
	if err != nil || fromCache {
		return DeliveryPrepare{
			AttemptID: attemptID, Draft: draft, Hook: hook, Revision: rev,
			IdempotencyKey: key, EventType: DeliveryEventReplySend, Actor: actor,
			FromCache: fromCache,
		}, err
	}
	if err := r.markDeliveryPendingTx(ctx, tx, draft.ID); err != nil {
		return DeliveryPrepare{}, err
	}
	if err := r.insertEventTx(ctx, tx, draft, rev.ID, hook.ID, "send_request", actor, "", nil); err != nil {
		return DeliveryPrepare{}, err
	}
	return DeliveryPrepare{
		AttemptID: attemptID, Draft: draft, Hook: hook, Revision: rev,
		IdempotencyKey: key, EventType: DeliveryEventReplySend, Actor: actor,
	}, nil
}

func canPrepareDelivery(status string) bool {
	return status == StatusApproved || status == StatusSendFailed
}

func (r *DraftTaskRepo) ensureFreshDeliverySourceTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, draft Draft, actor Actor,
) error {
	snapshot, err := r.feedbackSnapshotTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return err
	}
	if draft.SourceFingerprint == snapshot.Fingerprint {
		return nil
	}
	if err := r.markStaleTx(ctx, tx, draft, snapshot, actor, "stale_source"); err != nil {
		return err
	}
	return ErrStaleDraft
}

func (r *DraftTaskRepo) loadFreshDeliveryHookTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, draft Draft, actor Actor,
) (Hook, error) {
	hook, err := r.loadActiveHookTx(ctx, tx, tenantID)
	if err != nil {
		if errors.Is(err, ErrHookNotFound) && draft.ApprovedHookID != "" {
			return Hook{}, r.markStaleForCurrentSourceTx(ctx, tx, tenantID, feedbackID, draft, actor, "send_hook_changed")
		}
		return Hook{}, err
	}
	if err := r.ensureFreshApprovedHookTx(ctx, tx, tenantID, feedbackID, draft, hook, actor); err != nil {
		return Hook{}, err
	}
	return hook, nil
}

func (r *DraftTaskRepo) markDeliveryPendingTx(ctx context.Context, tx pgx.Tx, draftID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'send_pending',
		    external_delivery_status = '',
		    last_blocker = '',
		    updated_at = NOW(),
		    revision = revision + 1
		WHERE id = $1::uuid`,
		draftID,
	)
	if err != nil {
		return fmt.Errorf("mark reply draft send pending: %w", err)
	}
	return nil
}

func (r *DraftTaskRepo) loadActiveHookTx(ctx context.Context, tx pgx.Tx, tenantID string) (Hook, error) {
	var hook Hook
	err := tx.QueryRow(ctx, hookSelectSQL()+`
		WHERE tenant_id = $1 AND enabled = TRUE AND disabled_at IS NULL
		ORDER BY updated_at DESC
		LIMIT 1
		FOR UPDATE`, tenantID).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrHookNotFound
	}
	if err != nil {
		return Hook{}, fmt.Errorf("load active reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) ensureFreshApprovedHookTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, draft Draft, hook Hook, actor Actor,
) error {
	if draft.ApprovedHookID == "" {
		return r.markStaleForCurrentSourceTx(ctx, tx, tenantID, feedbackID, draft, actor, "send_hook_changed")
	}
	if draft.ApprovedHookID == hook.ID && draft.ApprovedHookFingerprint == hook.URLFingerprint {
		return nil
	}
	return r.markStaleForCurrentSourceTx(ctx, tx, tenantID, feedbackID, draft, actor, "send_hook_changed")
}

func (r *DraftTaskRepo) markStaleForCurrentSourceTx(
	ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, draft Draft, actor Actor, blocker string,
) error {
	snapshot, err := r.feedbackSnapshotTx(ctx, tx, tenantID, feedbackID)
	if err != nil {
		return err
	}
	if err := r.markStaleTx(ctx, tx, draft, snapshot, actor, blocker); err != nil {
		return err
	}
	return ErrStaleDraft
}

func (r *DraftTaskRepo) loadRevisionByIDTx(ctx context.Context, tx pgx.Tx, revisionID string) (Revision, error) {
	var rev Revision
	err := tx.QueryRow(ctx, `
		SELECT id, draft_id, tenant_id, feedback_id, cycle_no, revision_no, origin,
		       content, source_fingerprint, metadata, created_by, created_at
		FROM reply_draft_revisions
		WHERE id = $1::uuid`,
		revisionID,
	).Scan(&rev.ID, &rev.DraftID, &rev.TenantID, &rev.FeedbackID, &rev.CycleNo,
		&rev.RevisionNo, &rev.Origin, &rev.Content, &rev.SourceFingerprint,
		&rev.Metadata, &rev.CreatedBy, &rev.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, ErrDraftNotFound
	}
	if err != nil {
		return Revision{}, fmt.Errorf("load reply draft revision: %w", err)
	}
	return rev, nil
}

func (r *DraftTaskRepo) ensureDeliveryAttemptTx(
	ctx context.Context, tx pgx.Tx, draft Draft, hook Hook, rev Revision, key string, actor Actor,
) (string, bool, error) {
	requestFingerprint := deliveryRequestFingerprint(draft.ID, hook.ID, hook.URLFingerprint, rev.ID, key)
	attemptID, inserted, err := r.insertDeliveryAttemptTx(ctx, tx, draft, hook, rev, key, requestFingerprint, actor)
	if err != nil {
		return "", false, err
	}
	if inserted {
		return attemptID, false, nil
	}
	return r.resolveExistingAttemptTx(ctx, tx, draft, key, requestFingerprint)
}

func (r *DraftTaskRepo) insertDeliveryAttemptTx(
	ctx context.Context, tx pgx.Tx, draft Draft, hook Hook, rev Revision, key string, requestFingerprint string, actor Actor,
) (string, bool, error) {
	var attemptID string
	err := tx.QueryRow(ctx, `
		INSERT INTO reply_delivery_attempts (
		    tenant_id, draft_id, feedback_id, hook_id, revision_id, idempotency_key,
		    event_type, attempts, max_attempts, request_fingerprint,
		    requested_by_type, requested_by
		)
		VALUES ($1, $2::uuid, $3, $4::uuid, $5::uuid, $6, $7, 1, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, draft_id, idempotency_key) DO NOTHING
			RETURNING id`,
		draft.TenantID, draft.ID, draft.FeedbackID, hook.ID, rev.ID, key,
		DeliveryEventReplySend, maxReplyDeliveryAttempts, requestFingerprint,
		actor.Type, actor.ID,
	).Scan(&attemptID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("insert reply delivery attempt: %w", err)
	}
	return attemptID, true, nil
}

func (r *DraftTaskRepo) resolveExistingAttemptTx(
	ctx context.Context, tx pgx.Tx, draft Draft, key string, requestFingerprint string,
) (string, bool, error) {
	var attemptID, status, existingFingerprint string
	err := tx.QueryRow(ctx, `
		SELECT id, status, request_fingerprint
		FROM reply_delivery_attempts
		WHERE tenant_id = $1 AND draft_id = $2::uuid AND idempotency_key = $3
		FOR UPDATE`,
		draft.TenantID, draft.ID, key,
	).Scan(&attemptID, &status, &existingFingerprint)
	if err != nil {
		return "", false, fmt.Errorf("load reply delivery attempt: %w", err)
	}
	if existingFingerprint != requestFingerprint {
		return "", false, ErrIdempotencyConflict
	}
	switch status {
	case DeliveryStatusAccepted:
		return attemptID, true, nil
	case DeliveryStatusPending:
		return "", false, ErrRequestInProgress
	case DeliveryStatusFailed, DeliveryStatusDead:
		return attemptID, false, r.resetFailedAttemptTx(ctx, tx, attemptID)
	default:
		return "", false, fmt.Errorf("unknown reply delivery attempt status %q", status)
	}
}

func (r *DraftTaskRepo) resetFailedAttemptTx(ctx context.Context, tx pgx.Tx, attemptID string) error {
	return r.resetDeliveryAttemptTx(ctx, tx, attemptID, Actor{})
}

func (r *DraftTaskRepo) resetDeliveryAttemptTx(ctx context.Context, tx pgx.Tx, attemptID string, actor Actor) error {
	requestedByTypeSQL := "requested_by_type"
	requestedBySQL := "requested_by"
	args := []any{attemptID}
	if actor.ID != "" {
		requestedByTypeSQL = "$2"
		requestedBySQL = "$3"
		args = append(args, actor.Type, actor.ID)
	}
	_, err := tx.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET status = 'pending', http_status = NULL, external_message_id = '',
		    error = '', response_meta = '{}'::jsonb, attempts = attempts + 1,
		    next_retry_at = NULL, completed_at = NULL,
		    requested_by_type = `+requestedByTypeSQL+`,
		    requested_by = `+requestedBySQL+`,
		    requested_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1::uuid`, args...)
	if err != nil {
		return fmt.Errorf("reset reply delivery attempt: %w", err)
	}
	return nil
}

func (r *DraftTaskRepo) loadAcceptedAttemptTx(ctx context.Context, tx pgx.Tx, draft Draft, key string) (DeliveryPrepare, error) {
	var attemptID, hookID, revisionID string
	err := tx.QueryRow(ctx, `
		SELECT id, hook_id, revision_id
		FROM reply_delivery_attempts
		WHERE tenant_id = $1 AND draft_id = $2::uuid AND idempotency_key = $3 AND status = 'accepted'`,
		draft.TenantID, draft.ID, key,
	).Scan(&attemptID, &hookID, &revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryPrepare{}, ErrAlreadySent
	}
	if err != nil {
		return DeliveryPrepare{}, fmt.Errorf("load accepted delivery attempt: %w", err)
	}
	hook, err := r.loadHookByIDTx(ctx, tx, hookID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	rev, err := r.loadRevisionByIDTx(ctx, tx, revisionID)
	if err != nil {
		return DeliveryPrepare{}, err
	}
	return DeliveryPrepare{
		AttemptID: attemptID, Draft: draft, Hook: hook, Revision: rev,
		IdempotencyKey: key, EventType: DeliveryEventReplySend, FromCache: true,
	}, nil
}

func (r *DraftTaskRepo) loadHookByIDTx(ctx context.Context, tx pgx.Tx, hookID string) (Hook, error) {
	var hook Hook
	err := tx.QueryRow(ctx, hookSelectSQL()+` WHERE id = $1::uuid`, hookID).Scan(scanHookDest(&hook)...) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return Hook{}, ErrHookNotFound
	}
	if err != nil {
		return Hook{}, fmt.Errorf("load reply send hook: %w", err)
	}
	return hook, nil
}

func (r *DraftTaskRepo) MarkDeliveryAccepted(
	ctx context.Context, attemptID string, httpStatus int, externalMessageID string,
) (Draft, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("begin mark delivery accepted: %w", err)
	}
	defer rollback(ctx, tx)
	draft, err := r.markDeliveryAcceptedTx(ctx, tx, attemptID, httpStatus, externalMessageID)
	if err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, fmt.Errorf("commit mark delivery accepted: %w", err)
	}
	return draft, nil
}

func (r *DraftTaskRepo) markDeliveryAcceptedTx(
	ctx context.Context, tx pgx.Tx, attemptID string, httpStatus int, externalMessageID string,
) (Draft, error) {
	attempt, err := r.loadAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return Draft{}, err
	}
	if attempt.Status != DeliveryStatusPending {
		if attempt.DraftID == "" {
			return Draft{}, nil
		}
		return r.loadDraftByIDTx(ctx, tx, attempt.DraftID)
	}
	var draft Draft
	if attempt.DraftID != "" {
		draft, err = r.loadDraftByIDForUpdateTx(ctx, tx, attempt.DraftID)
		if err != nil {
			return Draft{}, err
		}
		if !canCompleteDeliveryAttempt(draft, attempt) {
			return draft, nil
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET status = 'accepted', http_status = NULLIF($2, 0), external_message_id = $3,
		    error = '', next_retry_at = NULL,
		    response_meta = jsonb_build_object('http_status', NULLIF($2, 0)),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid`,
		attemptID, httpStatus, externalMessageID,
	); err != nil {
		return Draft{}, fmt.Errorf("mark reply delivery accepted: %w", err)
	}
	if attempt.DraftID == "" {
		return Draft{}, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'sent', sent_revision_id = $2::uuid, sent_hook_id = $3::uuid,
		    external_delivery_status = 'accepted', external_message_id = $4,
		    sent_at = NOW(), sent_by = $5, last_blocker = '',
		    updated_at = NOW(), revision = revision + 1
		WHERE id = $1::uuid`,
		attempt.DraftID, attempt.RevisionID, attempt.HookID, externalMessageID, attempt.RequestedBy,
	); err != nil {
		return Draft{}, fmt.Errorf("mark reply draft sent: %w", err)
	}
	draft, err = r.loadDraftByIDTx(ctx, tx, attempt.DraftID)
	if err != nil {
		return Draft{}, err
	}
	return draft, r.insertEventTx(ctx, tx, draft, attempt.RevisionID, attempt.HookID, "send_success",
		Actor{Type: attempt.RequestedByType, ID: attempt.RequestedBy}, "", nil)
}

func (r *DraftTaskRepo) MarkDeliveryFailed(ctx context.Context, attemptID string, httpStatus int, cause error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin mark delivery failed: %w", err)
	}
	defer rollback(ctx, tx)
	if err := r.markDeliveryFailedTx(ctx, tx, attemptID, httpStatus, cause); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit mark delivery failed: %w", err)
	}
	return nil
}

func (r *DraftTaskRepo) ResetStalePendingDeliveries(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = 5 * time.Minute
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin reset stale reply deliveries: %w", err)
	}
	defer rollback(ctx, tx)
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM reply_delivery_attempts
		WHERE status = 'pending'
		  AND updated_at < NOW() - make_interval(secs => $1)
		ORDER BY updated_at ASC
		FOR UPDATE SKIP LOCKED`, olderThan.Seconds())
	if err != nil {
		return 0, fmt.Errorf("select stale reply deliveries: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan stale reply delivery id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate stale reply deliveries: %w", err)
	}
	rows.Close()
	for _, id := range ids {
		if err := r.markDeliveryFailedTx(ctx, tx, id, 0, errors.New("reply delivery claim expired")); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit reset stale reply deliveries: %w", err)
	}
	return int64(len(ids)), nil
}

func (r *DraftTaskRepo) markDeliveryFailedTx(ctx context.Context, tx pgx.Tx, attemptID string, httpStatus int, cause error) error {
	attempt, err := r.loadAttemptTx(ctx, tx, attemptID)
	if err != nil {
		return err
	}
	if attempt.Status != DeliveryStatusPending {
		return nil
	}
	msg := truncate(strings.TrimSpace(fmt.Sprint(cause)), 500)
	status := DeliveryStatusFailed
	var nextRetryAt *time.Time
	if attempt.Attempts >= attempt.MaxAttempts {
		status = DeliveryStatusDead
	} else if attempt.EventType == DeliveryEventReplySend {
		next := time.Now().UTC().Add(deliveryRetryDelay(attempt.Attempts))
		nextRetryAt = ptrext.Of(next)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET status = $2::text, http_status = NULLIF($3::integer, 0), error = $4::text,
		    next_retry_at = $5::timestamptz,
		    response_meta = jsonb_build_object('http_status', NULLIF($3::integer, 0), 'error', $4::text),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid`,
		attemptID, status, httpStatus, msg, nextRetryAt,
	); err != nil {
		return fmt.Errorf("mark reply delivery failed: %w", err)
	}
	if attempt.DraftID == "" {
		return nil
	}
	draft, err := r.loadDraftByIDForUpdateTx(ctx, tx, attempt.DraftID)
	if err != nil {
		return err
	}
	if !canCompleteDeliveryAttempt(draft, attempt) {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE reply_drafts
		SET status = 'send_failed',
		    external_delivery_status = 'failed',
		    last_blocker = $2,
		    updated_at = NOW(), revision = revision + 1
		WHERE id = $1::uuid`,
		attempt.DraftID, msg,
	); err != nil {
		return fmt.Errorf("mark reply draft send failed: %w", err)
	}
	draft, err = r.loadDraftByIDTx(ctx, tx, attempt.DraftID)
	if err != nil {
		return err
	}
	return r.insertEventTx(ctx, tx, draft, attempt.RevisionID, attempt.HookID, "send_failure",
		Actor{Type: attempt.RequestedByType, ID: attempt.RequestedBy}, msg, nil)
}

func canCompleteDeliveryAttempt(draft Draft, attempt deliveryAttemptRow) bool {
	return draft.Status == StatusSendPending &&
		draft.ApprovedRevisionID == attempt.RevisionID &&
		draft.ApprovedHookID == attempt.HookID
}

func (r *DraftTaskRepo) markDeliveryDeadTx(ctx context.Context, tx pgx.Tx, attemptID string, reason string) error {
	msg := truncate(strings.TrimSpace(reason), 500)
	if msg == "" {
		msg = "reply delivery retry stopped"
	}
	_, err := tx.Exec(ctx, `
		UPDATE reply_delivery_attempts
		SET status = 'dead', http_status = NULL, error = $2::text,
		    next_retry_at = NULL,
		    response_meta = jsonb_build_object('error', $2::text),
		    completed_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid`,
		attemptID, msg,
	)
	if err != nil {
		return fmt.Errorf("mark reply delivery dead: %w", err)
	}
	return nil
}

type deliveryAttemptRow struct {
	DraftID         string
	HookID          string
	RevisionID      string
	EventType       string
	Status          string
	Attempts        int
	MaxAttempts     int
	RequestedByType string
	RequestedBy     string
}

func (r *DraftTaskRepo) loadAttemptTx(ctx context.Context, tx pgx.Tx, attemptID string) (deliveryAttemptRow, error) {
	var attempt deliveryAttemptRow
	var draftID, revisionID sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT draft_id, hook_id, revision_id, event_type, status, attempts, max_attempts,
		       requested_by_type, requested_by
		FROM reply_delivery_attempts
		WHERE id = $1::uuid
		FOR UPDATE`, attemptID,
	).Scan(&draftID, &attempt.HookID, &revisionID, &attempt.EventType, &attempt.Status,
		&attempt.Attempts, &attempt.MaxAttempts, &attempt.RequestedByType, &attempt.RequestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return deliveryAttemptRow{}, ErrDeliveryNotFound
	}
	if err != nil {
		return deliveryAttemptRow{}, fmt.Errorf("load reply delivery attempt: %w", err)
	}
	attempt.DraftID = nullString(draftID)
	attempt.RevisionID = nullString(revisionID)
	return attempt, nil
}

func deliveryRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 30 * time.Second
	}
	delay := 30 * time.Second
	for n := 1; n < attempts; n++ {
		delay *= 2
		if delay >= 30*time.Minute {
			return 30 * time.Minute
		}
	}
	return delay
}

func deliveryAttemptsSelectSQL() string {
	return `
		SELECT a.id, a.tenant_id, COALESCE(a.draft_id::text, ''),
		       COALESCE(a.feedback_id, 0), a.hook_id,
		       COALESCE(h.url_host, ''), COALESCE(h.url_fingerprint, ''),
		       COALESCE(a.revision_id::text, ''), a.event_type, a.idempotency_key,
		       a.status, COALESCE(a.http_status, 0), a.attempts, a.max_attempts,
		       a.next_retry_at, a.external_message_id, a.error,
		       a.requested_by_type, a.requested_by, a.requested_at,
		       a.completed_at, a.created_at, a.updated_at
		FROM reply_delivery_attempts a
		LEFT JOIN reply_send_hooks h ON h.id = a.hook_id`
}

func (r *DraftTaskRepo) loadDeliveryAttemptByIDForUpdateTx(
	ctx context.Context, tx pgx.Tx, tenantID string, attemptID string,
) (DeliveryAttempt, error) {
	attempt, err := scanDeliveryAttempt(tx.QueryRow(ctx, deliveryAttemptsSelectSQL()+`
		WHERE a.tenant_id = $1 AND a.id = $2::uuid
		FOR UPDATE OF a`, tenantID, attemptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryAttempt{}, ErrDeliveryNotFound
	}
	return attempt, err
}

func scanDeliveryAttempts(rows pgx.Rows) ([]DeliveryAttempt, error) {
	var out []DeliveryAttempt
	for rows.Next() {
		attempt, err := scanDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func scanDeliveryAttempt(row pgx.Row) (DeliveryAttempt, error) {
	var attempt DeliveryAttempt
	var nextRetryAt, completedAt sql.NullTime
	if err := row.Scan(
		&attempt.ID, &attempt.TenantID, &attempt.DraftID, &attempt.FeedbackID,
		&attempt.HookID, &attempt.HookHost, &attempt.HookFingerprint,
		&attempt.RevisionID, &attempt.EventType, &attempt.IdempotencyKey,
		&attempt.Status, &attempt.HTTPStatus, &attempt.Attempts, &attempt.MaxAttempts,
		&nextRetryAt, &attempt.ExternalMessageID, &attempt.Error,
		&attempt.RequestedByType, &attempt.RequestedBy, &attempt.RequestedAt,
		&completedAt, &attempt.CreatedAt, &attempt.UpdatedAt,
	); err != nil {
		return DeliveryAttempt{}, fmt.Errorf("scan reply delivery attempt: %w", err)
	}
	if nextRetryAt.Valid {
		attempt.NextRetryAt = ptrext.Of(nextRetryAt.Time)
	}
	if completedAt.Valid {
		attempt.CompletedAt = ptrext.Of(completedAt.Time)
	}
	return attempt, nil
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func activeDraftSQL() string {
	return `
		SELECT d.id, d.tenant_id, d.feedback_id, d.cycle_no, d.status,
		       d.active_revision_id, d.approved_revision_id, d.sent_revision_id,
		       d.approved_hook_id, d.approved_hook_fingerprint,
		       COALESCE(r.content, ''), d.source_fingerprint, d.last_blocker,
		       d.external_delivery_status, d.external_message_id,
		       d.generated_at, d.generated_by, d.edited_at, d.edited_by,
		       d.approved_at, d.approved_by, d.rejected_at, d.rejected_by,
		       d.sent_at, d.sent_by, d.revision, d.created_at, d.updated_at
		FROM reply_drafts d
		LEFT JOIN reply_draft_revisions r ON r.id = d.active_revision_id
		WHERE d.tenant_id = $1 AND d.feedback_id = $2 AND d.archived_at IS NULL`
}

func scanDraft(row pgx.Row) (Draft, error) {
	var draft Draft
	var activeID, approvedID, sentID, approvedHookID sql.NullString
	var generatedAt, editedAt, approvedAt, rejectedAt, sentAt sql.NullTime
	err := row.Scan(&draft.ID, &draft.TenantID, &draft.FeedbackID, &draft.CycleNo, &draft.Status,
		&activeID, &approvedID, &sentID, &approvedHookID, &draft.ApprovedHookFingerprint,
		&draft.ActiveContent, &draft.SourceFingerprint,
		&draft.LastBlocker, &draft.ExternalDeliveryStatus, &draft.ExternalMessageID,
		&generatedAt, &draft.GeneratedBy, &editedAt, &draft.EditedBy, &approvedAt,
		&draft.ApprovedBy, &rejectedAt, &draft.RejectedBy, &sentAt, &draft.SentBy,
		&draft.Revision, &draft.CreatedAt, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrDraftNotFound
	}
	if err != nil {
		return Draft{}, fmt.Errorf("scan reply draft: %w", err)
	}
	draft.ActiveRevisionID = nullString(activeID)
	draft.ApprovedRevisionID = nullString(approvedID)
	draft.SentRevisionID = nullString(sentID)
	draft.ApprovedHookID = nullString(approvedHookID)
	draft.GeneratedAt = nullTime(generatedAt)
	draft.EditedAt = nullTime(editedAt)
	draft.ApprovedAt = nullTime(approvedAt)
	draft.RejectedAt = nullTime(rejectedAt)
	draft.SentAt = nullTime(sentAt)
	return draft, nil
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return ptrext.Of(value.Time)
}
