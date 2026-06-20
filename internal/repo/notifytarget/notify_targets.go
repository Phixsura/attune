package notifytarget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// NotifyTargetRepo owns the tenant_notify_targets table. wires
// env-defined webhook destinations into this table on startup; a follow-up
// adds a console for self-serve CRUD over the same rows.
type NotifyTargetRepo struct {
	pool *pgxpool.Pool
}

func NewNotifyTarget(pool *pgxpool.Pool) *NotifyTargetRepo {
	return ptrext.Of(NotifyTargetRepo{pool: pool})
}

// Destination types — keep in lockstep with the CHECK constraint in
// migrations/004_tenant_notify_targets.sql (and 011 for github-issue).
//
// All outbox-routed destinations share the same Transport + RetryPolicy;
// only the per-type adapter (outbound/adapter/generic, outbound/adapter/githubissue,
// …) differs. New destinations require: (1) extend the CHECK constraint
// via a new migration, (2) add the constant here, (3) implement an adapter
// in internal/outbound/adapter/, (4) include in selectOutboxTargets if
// the audience semantics apply.
const (
	DestRawWebhook  = "raw-webhook"
	DestSlackBot    = "slack-bot" // legacy — kept for migration compat
	DestSlack       = "slack"     // Slack incoming webhook (Block Kit)
	DestLark        = "lark"      // Lark/Feishu incoming webhook (interactive card)
	DestEmail       = "email"
	DestGitHubIssue = "github-issue"
)

// Audiences — keep in lockstep with the CHECK constraint.
const (
	AudiencePool  = "pool"  // every enriched row
	AudienceRadar = "radar" // P0 / P1 only
	AudienceAll   = "all"   // attune routes both PushPool + PushRadar here
	// AudienceDigest marks a raw-webhook target as the destination for the
	// daily digest (#27). It is a routing FILTER only — the per-event path
	// (service/enrich.selectOutboxTargets) has no case for it, so a digest
	// target receives no per-row traffic; the digest worker addresses it
	// directly via GetByTenantAudience(tenant, raw-webhook, digest).
	AudienceDigest = "digest"
)

// NotifyTarget is one wired destination row.
type NotifyTarget struct {
	ID               uuid.UUID
	TenantID         string
	DestinationType  string
	Audience         string
	URL              string
	Secret           string
	TimeoutSeconds   int
	Disabled         bool
	SignatureVersion string    // "v2-content-hash" (default) or "v2-bytes" (legacy)
	CreatedAt        time.Time // DB column tenant_notify_targets.created_at (UTC)
	// Current alert state. LastFailureAt is non-nil iff the most recent
	// delivery to this target failed (cleared on next success).
	LastFailureAt *time.Time
	LastError     string
}

// ErrNotifyTargetNotFound is returned when a lookup yields no row.
var ErrNotifyTargetNotFound = errors.New("notify target not found")

// ErrNotifyTargetConflict is returned by Insert when the (tenant_id,
// destination_type, audience) UNIQUE constraint trips. Mapped to 409
// by the console handler.
var ErrNotifyTargetConflict = errors.New("notify target already exists for this destination + audience")

// Upsert inserts a row or updates an existing one keyed by
// (tenant_id, destination_type, audience). Used by the env-bootstrap path.
func (r *NotifyTargetRepo) Upsert(ctx context.Context, t NotifyTarget) error {
	if t.TenantID == "" || t.DestinationType == "" || t.Audience == "" {
		return fmt.Errorf("notify target requires tenant_id, destination_type, audience")
	}
	_, err := r.pool.Exec(
		ctx, `
		INSERT INTO tenant_notify_targets
		 (tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (tenant_id, destination_type, audience)
		DO UPDATE SET
		 url = EXCLUDED.url,
		 secret = EXCLUDED.secret,
		 timeout_seconds = EXCLUDED.timeout_seconds,
		 disabled = EXCLUDED.disabled,
		 updated_at = NOW()`,
		t.TenantID, t.DestinationType, t.Audience,
		t.URL, t.Secret, t.TimeoutSeconds, t.Disabled,
	)
	if err != nil {
		return fmt.Errorf("upsert notify target: %w", err)
	}
	return nil
}

// ListAllActive returns every active row across all tenants. Used by
// startup wiring to build the in-memory routing table; attune picks up
// any DB-side change on restart.
func (r *NotifyTargetRepo) ListAllActive(ctx context.Context) ([]NotifyTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 signature_version, created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE disabled = FALSE`)
	if err != nil {
		return nil, fmt.Errorf("list all active notify targets: %w", err)
	}
	defer rows.Close()
	return scanNotifyTargets(rows)
}

// ListActiveByTenant returns all enabled destinations for a tenant.
// Empty slice (not error) when the tenant has no rows or all are disabled.
func (r *NotifyTargetRepo) ListActiveByTenant(ctx context.Context, tenantID string) ([]NotifyTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 signature_version, created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE tenant_id = $1
		 AND disabled = FALSE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list notify targets: %w", err)
	}
	defer rows.Close()
	return scanNotifyTargets(rows)
}

// scanNotifyTargets drains a *pgx.Rows into []NotifyTarget. Callers
// must defer rows.Close() themselves.
func scanNotifyTargets(rows pgx.Rows) ([]NotifyTarget, error) {
	var out []NotifyTarget
	for rows.Next() {
		var t NotifyTarget
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.DestinationType, &t.Audience,
			&t.URL, &t.Secret, &t.TimeoutSeconds, &t.Disabled,
			&t.SignatureVersion, &t.CreatedAt, &t.LastFailureAt, &t.LastError,
		); err != nil {
			return nil, fmt.Errorf("scan notify target: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListByTenant returns every row for the tenant (including disabled).
// Used by the console UI which shows disabled rows greyed out.
func (r *NotifyTargetRepo) ListByTenant(ctx context.Context, tenantID string) ([]NotifyTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 signature_version, created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE tenant_id = $1
		 ORDER BY destination_type, audience`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list notify targets by tenant: %w", err)
	}
	defer rows.Close()
	return scanNotifyTargets(rows)
}

// ListActiveByTenantAudience returns all enabled targets for a tenant with
// the specified audience. Used by the digest worker for multi-channel fan-out.
func (r *NotifyTargetRepo) ListActiveByTenantAudience(ctx context.Context, tenantID, audience string) ([]NotifyTarget, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled,
		 signature_version, created_at, last_failure_at, last_error
		 FROM tenant_notify_targets
		 WHERE tenant_id = $1
		 AND audience = $2
		 AND disabled = FALSE
		 ORDER BY destination_type`, tenantID, audience)
	if err != nil {
		return nil, fmt.Errorf("list notify targets by tenant audience: %w", err)
	}
	defer rows.Close()
	return scanNotifyTargets(rows)
}

// Insert creates a fresh row. Distinct from Upsert (which the boot path
// uses) so the console can surface "already exists" as 409 instead of
// silently overwriting a manually-tuned secret.
func (r *NotifyTargetRepo) Insert(ctx context.Context, t NotifyTarget) (uuid.UUID, time.Time, error) {
	const where = "repo.NotifyTargetRepo.Insert"
	if t.TenantID == "" || t.DestinationType == "" || t.Audience == "" {
		return uuid.Nil, time.Time{}, fmt.Errorf("notify target requires tenant_id, destination_type, audience")
	}
	var id uuid.UUID
	var createdAt time.Time
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO tenant_notify_targets
		 (tenant_id, destination_type, audience, url, secret, timeout_seconds, disabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		t.TenantID, t.DestinationType, t.Audience,
		t.URL, t.Secret, t.TimeoutSeconds, t.Disabled,
	).Scan(&id, &createdAt)
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return uuid.Nil, time.Time{}, ErrNotifyTargetConflict
		}
		logext.Errorf(ctx, "[%s] insert failed,tenant:%s,dest_type:%s,err:%+v",
			where, t.TenantID, t.DestinationType, err.Error())
		return uuid.Nil, time.Time{}, fmt.Errorf("insert notify target: %w", err)
	}
	return id, createdAt, nil
}
