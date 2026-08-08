// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type NPSCampaignRunEvidenceExport struct {
	ID               uuid.UUID
	TenantID         string
	CampaignID       uuid.UUID
	RunID            uuid.UUID
	ClientRequestKey uuid.UUID
	ReportVersion    string
	GeneratedAt      time.Time
	Artifact         []byte
	ArtifactSHA256   string
	CreatedByType    string
	CreatedBy        string
	CreatedAt        time.Time
	DownloadedAt     *time.Time
	ExpiresAt        time.Time
}

type NPSCampaignRunEvidenceExportSummary struct {
	ID               uuid.UUID
	TenantID         string
	CampaignID       uuid.UUID
	RunID            uuid.UUID
	ClientRequestKey uuid.UUID
	ReportVersion    string
	GeneratedAt      time.Time
	ArtifactSHA256   string
	CreatedByType    string
	CreatedAt        time.Time
	DownloadedAt     *time.Time
	ExpiresAt        time.Time
}

const npsEvidenceExportColumns = `
id, tenant_id, campaign_id, run_id, client_request_key, report_version, generated_at,
artifact, artifact_sha256, created_by_type, created_by, created_at, downloaded_at, expires_at`

const npsEvidenceExportSummaryColumns = `
id, tenant_id, campaign_id, run_id, client_request_key, report_version, generated_at,
artifact_sha256, created_by_type, created_at, downloaded_at, expires_at`

func (r *Repo) CreateNPSCampaignRunEvidenceExport(
	ctx context.Context,
	export NPSCampaignRunEvidenceExport,
) (NPSCampaignRunEvidenceExport, error) {
	if export.ID == uuid.Nil || export.ClientRequestKey == uuid.Nil || strings.TrimSpace(export.TenantID) == "" ||
		export.CampaignID == uuid.Nil || export.RunID == uuid.Nil ||
		strings.TrimSpace(export.ReportVersion) == "" || len(export.Artifact) == 0 ||
		strings.TrimSpace(export.ArtifactSHA256) == "" || strings.TrimSpace(export.CreatedByType) == "" ||
		strings.TrimSpace(export.CreatedBy) == "" || export.GeneratedAt.IsZero() || export.ExpiresAt.IsZero() {
		return NPSCampaignRunEvidenceExport{}, ErrInvalidInput
	}
	if !npsEvidenceArtifactDigestMatches(export.Artifact, export.ArtifactSHA256) {
		return NPSCampaignRunEvidenceExport{}, ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO survey_nps_run_evidence_exports (
			id, tenant_id, campaign_id, run_id, client_request_key, report_version, generated_at,
			artifact, artifact_sha256, created_by_type, created_by, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+npsEvidenceExportColumns,
		export.ID,
		strings.TrimSpace(export.TenantID),
		export.CampaignID,
		export.RunID,
		export.ClientRequestKey,
		strings.TrimSpace(export.ReportVersion),
		export.GeneratedAt.UTC(),
		export.Artifact,
		strings.TrimSpace(export.ArtifactSHA256),
		strings.TrimSpace(export.CreatedByType),
		strings.TrimSpace(export.CreatedBy),
		export.ExpiresAt.UTC(),
	)
	item, err := scanNPSCampaignRunEvidenceExport(row)
	if err != nil {
		return NPSCampaignRunEvidenceExport{}, mapWriteError(err)
	}
	return item, nil
}

func (r *Repo) FindNPSCampaignRunEvidenceExportByRequestKey(
	ctx context.Context,
	tenantID string,
	campaignID, runID, clientRequestKey uuid.UUID,
) (NPSCampaignRunEvidenceExport, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil || clientRequestKey == uuid.Nil {
		return NPSCampaignRunEvidenceExport{}, ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx, `
		SELECT `+npsEvidenceExportColumns+`
		FROM survey_nps_run_evidence_exports
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND client_request_key = $4`,
		strings.TrimSpace(tenantID), campaignID, runID, clientRequestKey)
	item, err := scanNPSCampaignRunEvidenceExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return NPSCampaignRunEvidenceExport{}, ErrNotFound
	}
	if err != nil {
		return NPSCampaignRunEvidenceExport{}, fmt.Errorf("find NPS evidence export by request key: %w", err)
	}
	if err := verifyNPSCampaignRunEvidenceExport(item); err != nil {
		return NPSCampaignRunEvidenceExport{}, err
	}
	return item, nil
}

func (r *Repo) GetNPSCampaignRunEvidenceExport(
	ctx context.Context,
	tenantID string,
	campaignID, runID, exportID uuid.UUID,
) (NPSCampaignRunEvidenceExport, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil || exportID == uuid.Nil {
		return NPSCampaignRunEvidenceExport{}, ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx, `
		SELECT `+npsEvidenceExportColumns+`
		FROM survey_nps_run_evidence_exports
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND id = $4`,
		strings.TrimSpace(tenantID), campaignID, runID, exportID)
	item, err := scanNPSCampaignRunEvidenceExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return NPSCampaignRunEvidenceExport{}, ErrNotFound
	}
	if err != nil {
		return NPSCampaignRunEvidenceExport{}, fmt.Errorf("get NPS evidence export: %w", err)
	}
	if err := verifyNPSCampaignRunEvidenceExport(item); err != nil {
		return NPSCampaignRunEvidenceExport{}, err
	}
	return item, nil
}

func (r *Repo) ListNPSCampaignRunEvidenceExports(
	ctx context.Context,
	tenantID string,
	campaignID, runID uuid.UUID,
	limit int,
) ([]NPSCampaignRunEvidenceExportSummary, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+npsEvidenceExportSummaryColumns+`
		FROM survey_nps_run_evidence_exports
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3
		ORDER BY generated_at DESC, id DESC
		LIMIT $4`, strings.TrimSpace(tenantID), campaignID, runID, boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list NPS evidence exports: %w", err)
	}
	defer rows.Close()
	items := make([]NPSCampaignRunEvidenceExportSummary, 0)
	for rows.Next() {
		var item NPSCampaignRunEvidenceExportSummary
		if err := rows.Scan(
			&item.ID,
			&item.TenantID,
			&item.CampaignID,
			&item.RunID,
			&item.ClientRequestKey,
			&item.ReportVersion,
			&item.GeneratedAt,
			&item.ArtifactSHA256,
			&item.CreatedByType,
			&item.CreatedAt,
			&item.DownloadedAt,
			&item.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan NPS evidence export summary: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list NPS evidence export rows: %w", err)
	}
	return items, nil
}

func (r *Repo) MarkNPSCampaignRunEvidenceExportDownloaded(
	ctx context.Context,
	tenantID string,
	campaignID, runID, exportID uuid.UUID,
) (NPSCampaignRunEvidenceExport, error) {
	if strings.TrimSpace(tenantID) == "" || campaignID == uuid.Nil || runID == uuid.Nil || exportID == uuid.Nil {
		return NPSCampaignRunEvidenceExport{}, ErrInvalidInput
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE survey_nps_run_evidence_exports
		SET downloaded_at = COALESCE(downloaded_at, NOW())
		WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND id = $4
		  AND expires_at > NOW()
		RETURNING `+npsEvidenceExportColumns,
		strings.TrimSpace(tenantID), campaignID, runID, exportID)
	item, err := scanNPSCampaignRunEvidenceExport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		var expired bool
		err := r.pool.QueryRow(ctx, `
			SELECT expires_at <= NOW()
			FROM survey_nps_run_evidence_exports
			WHERE tenant_id = $1 AND campaign_id = $2 AND run_id = $3 AND id = $4`,
			strings.TrimSpace(tenantID), campaignID, runID, exportID).Scan(&expired)
		if errors.Is(err, pgx.ErrNoRows) {
			return NPSCampaignRunEvidenceExport{}, ErrNotFound
		}
		if err != nil {
			return NPSCampaignRunEvidenceExport{}, fmt.Errorf("check NPS evidence export expiry: %w", err)
		}
		if expired {
			return NPSCampaignRunEvidenceExport{}, ErrNPSArtifactExpired
		}
		return NPSCampaignRunEvidenceExport{}, ErrNotFound
	}
	if err != nil {
		return NPSCampaignRunEvidenceExport{}, fmt.Errorf("mark NPS evidence export downloaded: %w", err)
	}
	if err := verifyNPSCampaignRunEvidenceExport(item); err != nil {
		return NPSCampaignRunEvidenceExport{}, err
	}
	return item, nil
}

// PurgeExpiredNPSCampaignRunEvidenceExports removes expired artifact rows in
// bounded batches. Audit rows remain intact because they are the durable
// record that an export was created or downloaded.
func (r *Repo) PurgeExpiredNPSCampaignRunEvidenceExports(
	ctx context.Context,
	now time.Time,
	limit int,
) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `
		WITH expired AS (
			SELECT id
			FROM survey_nps_run_evidence_exports
			WHERE expires_at <= $1
			ORDER BY expires_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM survey_nps_run_evidence_exports export
		USING expired
		WHERE export.id = expired.id
		RETURNING export.tenant_id`, now.UTC(), boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("purge expired NPS evidence exports: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int64)
	for rows.Next() {
		var tenantID string
		if err := rows.Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("scan purged NPS evidence export tenant: %w", err)
		}
		counts[tenantID]++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read purged NPS evidence exports: %w", err)
	}
	return counts, nil
}

func verifyNPSCampaignRunEvidenceExport(item NPSCampaignRunEvidenceExport) error {
	if !npsEvidenceArtifactDigestMatches(item.Artifact, item.ArtifactSHA256) {
		return fmt.Errorf("%w: export_id=%s", ErrNPSArtifactIntegrity, item.ID)
	}
	return nil
}

func npsEvidenceArtifactDigestMatches(artifact []byte, declared string) bool {
	digest := sha256.Sum256(artifact)
	return strings.TrimSpace(declared) == "sha256:"+hex.EncodeToString(digest[:])
}

func scanNPSCampaignRunEvidenceExport(row pgx.Row) (NPSCampaignRunEvidenceExport, error) {
	var item NPSCampaignRunEvidenceExport
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.CampaignID,
		&item.RunID,
		&item.ClientRequestKey,
		&item.ReportVersion,
		&item.GeneratedAt,
		&item.Artifact,
		&item.ArtifactSHA256,
		&item.CreatedByType,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.DownloadedAt,
		&item.ExpiresAt,
	)
	return item, err
}

// NPSCampaignRunEvidenceExportRepo is the optional repository surface used by
// evidence snapshots. Keeping it separate preserves lightweight survey fakes.
type NPSCampaignRunEvidenceExportRepo interface {
	CreateNPSCampaignRunEvidenceExport(context.Context, NPSCampaignRunEvidenceExport) (NPSCampaignRunEvidenceExport, error)
	FindNPSCampaignRunEvidenceExportByRequestKey(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID) (NPSCampaignRunEvidenceExport, error)
	GetNPSCampaignRunEvidenceExport(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID) (NPSCampaignRunEvidenceExport, error)
	ListNPSCampaignRunEvidenceExports(context.Context, string, uuid.UUID, uuid.UUID, int) ([]NPSCampaignRunEvidenceExportSummary, error)
	MarkNPSCampaignRunEvidenceExportDownloaded(context.Context, string, uuid.UUID, uuid.UUID, uuid.UUID) (NPSCampaignRunEvidenceExport, error)
}

type NPSCampaignRunEvidenceExportPurger interface {
	PurgeExpiredNPSCampaignRunEvidenceExports(context.Context, time.Time, int) (map[string]int64, error)
}

var (
	_ NPSCampaignRunEvidenceExportRepo   = (*Repo)(nil)
	_ NPSCampaignRunEvidenceExportPurger = (*Repo)(nil)
)
