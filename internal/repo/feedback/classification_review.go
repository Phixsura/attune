package feedback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	ClassificationReviewOutcomeAccepted  = "accepted"
	ClassificationReviewOutcomeEdited    = "edited"
	ClassificationReviewOutcomeDismissed = "dismissed"

	classificationReviewDefaultLimit = 10
	classificationReviewMaxLimit     = 50
)

var ErrClassificationReviewFeedbackNotFound = errors.New("classification review feedback not found")

type ClassificationReviewRecord struct {
	TenantID       string
	FeedbackID     int64
	Outcome        string
	SignalReason   string
	CorrectionJSON string
	Note           string
	ReviewedBy     string
}

type ClassificationReviewLearningOpts struct {
	TenantID     string
	From         time.Time
	To           time.Time
	SignalReason string
	Limit        int
}

type ClassificationReviewEvent struct {
	ID                       int64
	FeedbackID               int64
	SemanticRunID            *int64
	Outcome                  string
	SignalReason             string
	CorrectionJSON           string
	Note                     string
	Source                   string
	LogicalModel             string
	ProviderModel            string
	ChannelID                string
	PromptVersion            string
	PromptVersionID          string
	ClassificationConfidence *float64
	ReviewedBy               string
	ReviewedAt               time.Time
}

type ClassificationReviewLearning struct {
	From                    time.Time
	To                      time.Time
	TotalReviews            int64
	Accepted                int64
	Edited                  int64
	Dismissed               int64
	TrainingCandidateCount  int64
	ReviewedFeedbackCount   int64
	ClassifiedFeedbackCount int64
	ReviewCoverageRate      float64
	ReasonBuckets           []ClassificationReviewReasonBucket
	RecentEvents            []ClassificationReviewEvent
}

type ClassificationReviewReasonBucket struct {
	SignalReason           string
	TotalReviews           int64
	Accepted               int64
	Edited                 int64
	Dismissed              int64
	TrainingCandidateCount int64
	LastReviewedAt         time.Time
}

func (r *FeedbackRepo) RecordClassificationReview(
	ctx context.Context,
	in ClassificationReviewRecord,
) (ClassificationReviewEvent, error) {
	in = normalizeClassificationReviewRecord(in)
	event, err := scanClassificationReviewEvent(r.pool.QueryRow(ctx, `
		WITH feedback_row AS (
			SELECT id, source, classification_confidence
			  FROM user_feedback
			 WHERE tenant_id = $1
			   AND id = $2
			   AND deleted_at IS NULL
		),
		latest_run AS (
			SELECT ser.id,
			       ser.source,
			       ser.logical_model,
			       ser.provider_model,
			       ser.channel_id,
			       ser.prompt_version,
			       COALESCE(ser.prompt_version_id::text, '') AS prompt_version_id
			  FROM semantic_extraction_runs ser
			 WHERE ser.tenant_id = $1
			   AND ser.subject_type = $8
			   AND ser.subject_id = $2
			 ORDER BY ser.created_at DESC, ser.id DESC
			 LIMIT 1
		)
		INSERT INTO classification_review_events
		 (tenant_id, feedback_id, semantic_run_id, outcome, signal_reason, correction,
		  note, source, logical_model, provider_model, channel_id, prompt_version,
		  prompt_version_id, classification_confidence, reviewed_by)
		SELECT $1,
		       fb.id,
		       latest_run.id,
		       $3,
		       $4,
		       $5::jsonb,
		       $6,
		       COALESCE(NULLIF(latest_run.source, ''), fb.source, ''),
		       COALESCE(latest_run.logical_model, ''),
		       COALESCE(latest_run.provider_model, ''),
		       COALESCE(latest_run.channel_id, ''),
		       COALESCE(latest_run.prompt_version, ''),
		       COALESCE(latest_run.prompt_version_id, ''),
		       fb.classification_confidence,
		       $7
		  FROM feedback_row fb
		  LEFT JOIN latest_run ON TRUE
		RETURNING id, feedback_id, semantic_run_id, outcome, signal_reason, correction::text,
		          note, source, logical_model, provider_model, channel_id, prompt_version,
		          prompt_version_id, classification_confidence, reviewed_by, reviewed_at`,
		in.TenantID, in.FeedbackID, in.Outcome, in.SignalReason, in.CorrectionJSON,
		in.Note, in.ReviewedBy, SemanticSubjectFeedback,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassificationReviewEvent{}, ErrClassificationReviewFeedbackNotFound
	}
	if err != nil {
		return ClassificationReviewEvent{}, fmt.Errorf("record classification review: %w", err)
	}
	return event, nil
}

func (r *FeedbackRepo) ClassificationReviewLearning(
	ctx context.Context,
	opts ClassificationReviewLearningOpts,
) (ClassificationReviewLearning, error) {
	opts = normalizeClassificationReviewLearningOpts(opts)
	summary, err := r.classificationReviewSummary(ctx, opts)
	if err != nil {
		return ClassificationReviewLearning{}, err
	}
	reasonBuckets, err := r.classificationReviewReasonBuckets(ctx, opts)
	if err != nil {
		return ClassificationReviewLearning{}, err
	}
	recentEvents, err := r.classificationReviewRecentEvents(ctx, opts)
	if err != nil {
		return ClassificationReviewLearning{}, err
	}
	summary.ReasonBuckets = reasonBuckets
	summary.RecentEvents = recentEvents
	return summary, nil
}

func (r *FeedbackRepo) classificationReviewSummary(
	ctx context.Context,
	opts ClassificationReviewLearningOpts,
) (ClassificationReviewLearning, error) {
	out := ClassificationReviewLearning{From: opts.From, To: opts.To}
	err := r.pool.QueryRow(ctx, `
		WITH review_summary AS (
			SELECT COUNT(*) AS total_reviews,
			       COUNT(*) FILTER (WHERE outcome = 'accepted') AS accepted,
			       COUNT(*) FILTER (WHERE outcome = 'edited') AS edited,
			       COUNT(*) FILTER (WHERE outcome = 'dismissed') AS dismissed,
			       COUNT(*) FILTER (WHERE outcome IN ('edited', 'dismissed')) AS training_candidate_count,
			       COUNT(DISTINCT feedback_id) AS reviewed_feedback_count
			  FROM classification_review_events
			 WHERE tenant_id = $1
			   AND reviewed_at >= $2
			   AND reviewed_at < $3
			   AND ($4 = '' OR signal_reason = $4)
		),
		classified_summary AS (
			SELECT COUNT(DISTINCT subject_id) AS classified_feedback_count
			  FROM semantic_extraction_runs
			 WHERE tenant_id = $1
			   AND subject_type = $5
			   AND created_at >= $2
			   AND created_at < $3
		)
		SELECT review_summary.total_reviews,
		       review_summary.accepted,
		       review_summary.edited,
		       review_summary.dismissed,
		       review_summary.training_candidate_count,
		       review_summary.reviewed_feedback_count,
		       classified_summary.classified_feedback_count
		  FROM review_summary, classified_summary`,
		opts.TenantID, opts.From, opts.To, opts.SignalReason, SemanticSubjectFeedback,
	).Scan(
		&out.TotalReviews, &out.Accepted, &out.Edited, &out.Dismissed, // ptrext:allow scan-target
		&out.TrainingCandidateCount, &out.ReviewedFeedbackCount, &out.ClassifiedFeedbackCount,
	)
	if err != nil {
		return ClassificationReviewLearning{}, fmt.Errorf("classification review summary: %w", err)
	}
	if out.ClassifiedFeedbackCount > 0 {
		out.ReviewCoverageRate = float64(out.ReviewedFeedbackCount) / float64(out.ClassifiedFeedbackCount)
	}
	return out, nil
}

func (r *FeedbackRepo) classificationReviewReasonBuckets(
	ctx context.Context,
	opts ClassificationReviewLearningOpts,
) ([]ClassificationReviewReasonBucket, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT signal_reason,
		       COUNT(*) AS total_reviews,
		       COUNT(*) FILTER (WHERE outcome = 'accepted') AS accepted,
		       COUNT(*) FILTER (WHERE outcome = 'edited') AS edited,
		       COUNT(*) FILTER (WHERE outcome = 'dismissed') AS dismissed,
		       COUNT(*) FILTER (WHERE outcome IN ('edited', 'dismissed')) AS training_candidate_count,
		       MAX(reviewed_at) AS last_reviewed_at
		  FROM classification_review_events
		 WHERE tenant_id = $1
		   AND reviewed_at >= $2
		   AND reviewed_at < $3
		   AND ($4 = '' OR signal_reason = $4)
		 GROUP BY signal_reason
		 ORDER BY training_candidate_count DESC, total_reviews DESC, last_reviewed_at DESC
		 LIMIT $5`,
		opts.TenantID, opts.From, opts.To, opts.SignalReason, opts.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("classification review reason buckets: %w", err)
	}
	defer rows.Close()
	out := make([]ClassificationReviewReasonBucket, 0, opts.Limit)
	for rows.Next() {
		var bucket ClassificationReviewReasonBucket
		if err := rows.Scan(
			&bucket.SignalReason, &bucket.TotalReviews, &bucket.Accepted, // ptrext:allow scan-target
			&bucket.Edited, &bucket.Dismissed, &bucket.TrainingCandidateCount,
			&bucket.LastReviewedAt,
		); err != nil {
			return nil, fmt.Errorf("scan classification review reason bucket: %w", err)
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) classificationReviewRecentEvents(
	ctx context.Context,
	opts ClassificationReviewLearningOpts,
) ([]ClassificationReviewEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, feedback_id, semantic_run_id, outcome, signal_reason, correction::text,
		       note, source, logical_model, provider_model, channel_id, prompt_version,
		       prompt_version_id, classification_confidence, reviewed_by, reviewed_at
		  FROM classification_review_events
		 WHERE tenant_id = $1
		   AND reviewed_at >= $2
		   AND reviewed_at < $3
		   AND ($4 = '' OR signal_reason = $4)
		 ORDER BY reviewed_at DESC, id DESC
		 LIMIT $5`,
		opts.TenantID, opts.From, opts.To, opts.SignalReason, opts.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("classification review recent events: %w", err)
	}
	defer rows.Close()
	out := make([]ClassificationReviewEvent, 0, opts.Limit)
	for rows.Next() {
		event, err := scanClassificationReviewEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func normalizeClassificationReviewRecord(in ClassificationReviewRecord) ClassificationReviewRecord {
	if in.CorrectionJSON == "" {
		in.CorrectionJSON = "{}"
	}
	return in
}

func normalizeClassificationReviewLearningOpts(opts ClassificationReviewLearningOpts) ClassificationReviewLearningOpts {
	opts.From = opts.From.UTC()
	opts.To = opts.To.UTC()
	if opts.Limit <= 0 {
		opts.Limit = classificationReviewDefaultLimit
	}
	if opts.Limit > classificationReviewMaxLimit {
		opts.Limit = classificationReviewMaxLimit
	}
	return opts
}

func scanClassificationReviewEvent(row pgx.Row) (ClassificationReviewEvent, error) {
	var out ClassificationReviewEvent
	var semanticRunID sql.NullInt64
	var confidence sql.NullFloat64
	err := row.Scan(
		&out.ID, &out.FeedbackID, &semanticRunID, &out.Outcome, &out.SignalReason, // ptrext:allow scan-target
		&out.CorrectionJSON, &out.Note, &out.Source, &out.LogicalModel,
		&out.ProviderModel, &out.ChannelID, &out.PromptVersion, &out.PromptVersionID,
		&confidence, &out.ReviewedBy, &out.ReviewedAt,
	)
	if err != nil {
		return ClassificationReviewEvent{}, fmt.Errorf("scan classification review event: %w", err)
	}
	if semanticRunID.Valid {
		out.SemanticRunID = ptrext.Of(semanticRunID.Int64)
	}
	out.ClassificationConfidence = nullFloatPtr(confidence)
	return out, nil
}
