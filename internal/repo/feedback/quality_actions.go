// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	QualityActionStatusOpen         = "open"
	QualityActionStatusAcknowledged = "acknowledged"
	QualityActionStatusResolved     = "resolved"
	QualityActionStatusDismissed    = "dismissed"

	QualityActionSeverityAlert            = "alert"
	QualityActionSeverityWatch            = "watch"
	QualityActionSeverityNormal           = "normal"
	QualityActionSeverityInsufficientData = "insufficient_data"

	qualityActionDefaultLimit = 50
	qualityActionMaxLimit     = 200
)

type QualityAction struct {
	ID                string
	TenantID          string
	ActionKey         string
	Signal            string
	Status            string
	Severity          string
	TargetPath        string
	MetricLabel       string
	MetricValue       string
	RecommendationKey string
	EvidenceJSON      string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AcknowledgedAt    *time.Time
	ResolvedAt        *time.Time
	DismissedAt       *time.Time
	UpdatedAt         time.Time
	UpdatedBy         string
}

type QualityActionListOpts struct {
	TenantID string
	Status   string
	Limit    int
}

type QualityActionUpsert struct {
	TenantID          string
	ActionKey         string
	Signal            string
	Status            string
	Severity          string
	TargetPath        string
	MetricLabel       string
	MetricValue       string
	RecommendationKey string
	EvidenceJSON      string
	ActorUserID       string
}

func (r *FeedbackRepo) ListQualityActions(ctx context.Context, opts QualityActionListOpts) ([]QualityAction, error) {
	opts = normalizeQualityActionListOpts(opts)
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, tenant_id, action_key, signal, status, severity,
		       target_path, metric_label, metric_value, recommendation_key,
		       evidence::text, created_at, last_seen_at, acknowledged_at,
		       resolved_at, dismissed_at, updated_at, updated_by
		  FROM feedback_quality_actions
		 WHERE tenant_id = $1
		   AND ($2 = '' OR status = $2)
		 ORDER BY
		       CASE status
		         WHEN 'open' THEN 0
		         WHEN 'acknowledged' THEN 1
		         WHEN 'resolved' THEN 2
		         WHEN 'dismissed' THEN 3
		         ELSE 4
		       END,
		       CASE severity
		         WHEN 'alert' THEN 0
		         WHEN 'watch' THEN 1
		         WHEN 'normal' THEN 2
		         ELSE 3
		       END,
		       last_seen_at DESC,
		       updated_at DESC
		 LIMIT $3`,
		opts.TenantID, opts.Status, opts.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list quality actions: %w", err)
	}
	defer rows.Close()
	var out []QualityAction
	for rows.Next() {
		row, err := scanQualityAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) UpsertQualityActionStatus(ctx context.Context, in QualityActionUpsert) (*QualityAction, error) {
	in = normalizeQualityActionUpsert(in)
	row, err := scanQualityAction(r.pool.QueryRow(ctx, `
		INSERT INTO feedback_quality_actions
		 (tenant_id, action_key, signal, status, severity, target_path, metric_label,
		  metric_value, recommendation_key, evidence, updated_by)
		VALUES
		 ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11)
		ON CONFLICT (tenant_id, action_key) DO UPDATE
		SET signal = EXCLUDED.signal,
		    status = EXCLUDED.status,
		    severity = EXCLUDED.severity,
		    target_path = EXCLUDED.target_path,
		    metric_label = EXCLUDED.metric_label,
		    metric_value = EXCLUDED.metric_value,
		    recommendation_key = EXCLUDED.recommendation_key,
		    evidence = EXCLUDED.evidence,
		    last_seen_at = NOW(),
		    acknowledged_at = CASE
		        WHEN EXCLUDED.status = 'acknowledged'
		             AND feedback_quality_actions.acknowledged_at IS NULL
		        THEN NOW()
		        WHEN EXCLUDED.status = 'open' THEN NULL
		        ELSE feedback_quality_actions.acknowledged_at
		    END,
		    resolved_at = CASE
		        WHEN EXCLUDED.status = 'resolved' THEN NOW()
		        WHEN EXCLUDED.status IN ('open', 'acknowledged') THEN NULL
		        ELSE feedback_quality_actions.resolved_at
		    END,
		    dismissed_at = CASE
		        WHEN EXCLUDED.status = 'dismissed' THEN NOW()
		        WHEN EXCLUDED.status IN ('open', 'acknowledged') THEN NULL
		        ELSE feedback_quality_actions.dismissed_at
		    END,
		    updated_at = NOW(),
		    updated_by = EXCLUDED.updated_by
		RETURNING id::text, tenant_id, action_key, signal, status, severity,
		          target_path, metric_label, metric_value, recommendation_key,
		          evidence::text, created_at, last_seen_at, acknowledged_at,
		          resolved_at, dismissed_at, updated_at, updated_by`,
		in.TenantID, in.ActionKey, in.Signal, in.Status, in.Severity,
		in.TargetPath, in.MetricLabel, in.MetricValue, in.RecommendationKey,
		in.EvidenceJSON, in.ActorUserID,
	))
	if err != nil {
		return nil, fmt.Errorf("upsert quality action: %w", err)
	}
	return ptrext.Of(row), nil
}

func normalizeQualityActionListOpts(opts QualityActionListOpts) QualityActionListOpts {
	if !isQualityActionStatus(opts.Status) {
		opts.Status = ""
	}
	if opts.Limit <= 0 {
		opts.Limit = qualityActionDefaultLimit
	}
	if opts.Limit > qualityActionMaxLimit {
		opts.Limit = qualityActionMaxLimit
	}
	return opts
}

func normalizeQualityActionUpsert(in QualityActionUpsert) QualityActionUpsert {
	if !isQualityActionStatus(in.Status) {
		in.Status = QualityActionStatusOpen
	}
	if !isQualityActionSeverity(in.Severity) {
		in.Severity = QualityActionSeverityWatch
	}
	if in.EvidenceJSON == "" {
		in.EvidenceJSON = "{}"
	}
	return in
}

func isQualityActionStatus(status string) bool {
	switch status {
	case QualityActionStatusOpen,
		QualityActionStatusAcknowledged,
		QualityActionStatusResolved,
		QualityActionStatusDismissed:
		return true
	default:
		return false
	}
}

func isQualityActionSeverity(severity string) bool {
	switch severity {
	case QualityActionSeverityAlert,
		QualityActionSeverityWatch,
		QualityActionSeverityNormal,
		QualityActionSeverityInsufficientData:
		return true
	default:
		return false
	}
}

func scanQualityAction(row pgx.Row) (QualityAction, error) {
	var out QualityAction
	// ptrext:allow scan-target
	err := row.Scan(
		&out.ID, &out.TenantID, &out.ActionKey, &out.Signal, &out.Status,
		&out.Severity, &out.TargetPath, &out.MetricLabel, &out.MetricValue,
		&out.RecommendationKey, &out.EvidenceJSON, &out.CreatedAt, &out.LastSeenAt,
		&out.AcknowledgedAt, &out.ResolvedAt, &out.DismissedAt, &out.UpdatedAt,
		&out.UpdatedBy,
	)
	if err != nil {
		return QualityAction{}, fmt.Errorf("scan quality action: %w", err)
	}
	return out, nil
}
