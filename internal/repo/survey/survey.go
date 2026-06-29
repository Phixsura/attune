// SPDX-License-Identifier: Apache-2.0

// Package survey owns the tenant_surveys and survey_responses tables.
package survey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// SurveyType enumerates the supported survey types.
const (
	TypeNPS  = "nps"
	TypeCSAT = "csat"
	TypeCES  = "ces"
)

// Survey is a tenant-scoped survey definition.
type Survey struct {
	ID         uuid.UUID
	TenantID   string
	Name       string
	SurveyType string
	Question   string
	Enabled    bool
	Config     map[string]any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Response is a single survey response.
type Response struct {
	ID         uuid.UUID
	TenantID   string
	SurveyID   uuid.UUID
	FeedbackID *int64
	Score      int
	Comment    string
	Respondent string
	CreatedAt  time.Time
}

// ScoreStats aggregates survey scores.
type ScoreStats struct {
	Total      int
	Average    float64
	Promoters  int
	Passives   int
	Detractors int
}

// ErrNotFound is returned when a lookup yields no row.
var ErrNotFound = errors.New("survey not found")

// Repo is the data-access layer for surveys.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a survey repository.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// ListByTenant returns all surveys for a tenant.
func (r *Repo) ListByTenant(ctx context.Context, tenantID string) ([]Survey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, survey_type, question, enabled, config, created_at, updated_at
		FROM tenant_surveys
		WHERE tenant_id = $1
		ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list surveys: %w", err)
	}
	defer rows.Close()
	return scanSurveys(rows)
}

// Create inserts a new survey.
func (r *Repo) Create(ctx context.Context, s Survey) (Survey, error) {
	var out Survey
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO tenant_surveys (tenant_id, name, survey_type, question, enabled, config)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, name, survey_type, question, enabled, config, created_at, updated_at`,
		s.TenantID, s.Name, s.SurveyType, s.Question, s.Enabled, s.Config,
	).Scan( // ptrext:allow scan-out-param
		&out.ID, &out.TenantID, &out.Name, &out.SurveyType, &out.Question,
		&out.Enabled, &out.Config, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return Survey{}, fmt.Errorf("create survey: %w", err)
	}
	return out, nil
}

// Delete removes a survey.
func (r *Repo) Delete(ctx context.Context, tenantID string, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM tenant_surveys WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete survey: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordResponse inserts a survey response.
func (r *Repo) RecordResponse(ctx context.Context, resp Response) (Response, error) {
	var out Response
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO survey_responses (tenant_id, survey_id, feedback_id, score, comment, respondent)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, survey_id, feedback_id, score, comment, respondent, created_at`,
		resp.TenantID, resp.SurveyID, resp.FeedbackID, resp.Score, resp.Comment, resp.Respondent,
	).Scan( // ptrext:allow scan-out-param
		&out.ID, &out.TenantID, &out.SurveyID, &out.FeedbackID,
		&out.Score, &out.Comment, &out.Respondent, &out.CreatedAt,
	)
	if err != nil {
		return Response{}, fmt.Errorf("record survey response: %w", err)
	}
	return out, nil
}

// GetStats computes NPS-style statistics for a survey.
func (r *Repo) GetStats(ctx context.Context, tenantID string, surveyID uuid.UUID) (ScoreStats, error) {
	var stats ScoreStats
	err := r.pool.QueryRow(
		ctx, `
		SELECT
			COUNT(*),
			COALESCE(AVG(score), 0),
			COUNT(*) FILTER (WHERE score >= 9),
			COUNT(*) FILTER (WHERE score >= 7 AND score <= 8),
			COUNT(*) FILTER (WHERE score <= 6)
		FROM survey_responses
		WHERE tenant_id = $1 AND survey_id = $2`,
		tenantID, surveyID,
	).Scan( // ptrext:allow scan-out-param
		&stats.Total, &stats.Average, &stats.Promoters, &stats.Passives, &stats.Detractors,
	)
	if err != nil {
		return ScoreStats{}, fmt.Errorf("get survey stats: %w", err)
	}
	return stats, nil
}

func scanSurveys(rows pgx.Rows) ([]Survey, error) {
	var out []Survey
	for rows.Next() {
		var s Survey
		if err := rows.Scan( // ptrext:allow scan-out-param
			&s.ID, &s.TenantID, &s.Name, &s.SurveyType, &s.Question,
			&s.Enabled, &s.Config, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan survey: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
