package feedback

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const triageDueSoonWindow = 12 * time.Hour

type FeedbackTriageCommandCenter struct {
	GeneratedAt          time.Time
	OpenCount            int64
	ActiveCount          int64
	ClosedCount          int64
	UrgentOpenCount      int64
	TerminalFailureCount int64
	IdentityDebtCount    int64
	OverdueCount         int64
	DueSoonCount         int64
	Lanes                []FeedbackTriageLane
}

type FeedbackTriageLane struct {
	Key               string
	Label             string
	OwnerLane         string
	Severity          string
	SLAHours          int
	Count             int64
	OverdueCount      int64
	DueSoonCount      int64
	OldestCreatedAt   *time.Time
	NextDeadlineAt    *time.Time
	RecommendedAction string
	FilterQuery       string
	SampleFeedbackIDs []int64
}

type feedbackTriageLaneDefinition struct {
	Key               string
	Label             string
	OwnerLane         string
	Severity          string
	SLAHours          int
	Predicate         string
	RecommendedAction string
	FilterQuery       string
}

func (r *FeedbackRepo) FeedbackTriageCommandCenter(
	ctx context.Context,
	tenantID string,
	now time.Time,
) (FeedbackTriageCommandCenter, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	summary, err := r.feedbackTriageSummary(ctx, tenantID)
	if err != nil {
		return FeedbackTriageCommandCenter{}, err
	}
	summary.GeneratedAt = now
	for _, def := range feedbackTriageLaneDefinitions() {
		lane, err := r.feedbackTriageLane(ctx, tenantID, now, def)
		if err != nil {
			return FeedbackTriageCommandCenter{}, err
		}
		summary.Lanes = append(summary.Lanes, lane)
		summary.OverdueCount += lane.OverdueCount
		summary.DueSoonCount += lane.DueSoonCount
		if def.Key == "urgent_open" {
			summary.UrgentOpenCount = lane.Count
		}
		if def.Key == "terminal_failures" {
			summary.TerminalFailureCount = lane.Count
		}
		if def.Key == "identity_debt" {
			summary.IdentityDebtCount = lane.Count
		}
	}
	return summary, nil
}

func (r *FeedbackRepo) feedbackTriageSummary(
	ctx context.Context,
	tenantID string,
) (FeedbackTriageCommandCenter, error) {
	const q = `
		SELECT
			COUNT(*) FILTER (WHERE COALESCE(ws.category, 'open') = 'open'),
			COUNT(*) FILTER (WHERE ws.category = 'active'),
			COUNT(*) FILTER (WHERE ws.category = 'closed')
		FROM user_feedback uf
		LEFT JOIN tenant_workflow_states ws
		  ON ws.tenant_id = uf.tenant_id AND ws.id = uf.workflow_state_id
		WHERE uf.tenant_id = $1`
	var out FeedbackTriageCommandCenter
	if err := r.pool.QueryRow(ctx, q, tenantID).Scan(&out.OpenCount, &out.ActiveCount, &out.ClosedCount); err != nil {
		return FeedbackTriageCommandCenter{}, fmt.Errorf("feedback triage summary: %w", err)
	}
	return out, nil
}

func (r *FeedbackRepo) feedbackTriageLane(
	ctx context.Context,
	tenantID string,
	now time.Time,
	def feedbackTriageLaneDefinition,
) (FeedbackTriageLane, error) {
	row := r.pool.QueryRow(ctx, feedbackTriageLaneSQL(def.Predicate), tenantID, now, def.SLAHours, maxEnrichmentAttempts)
	lane := FeedbackTriageLane{
		Key:               def.Key,
		Label:             def.Label,
		OwnerLane:         def.OwnerLane,
		Severity:          def.Severity,
		SLAHours:          def.SLAHours,
		RecommendedAction: def.RecommendedAction,
		FilterQuery:       def.FilterQuery,
	}
	var oldest sql.NullTime
	var deadline sql.NullTime
	if err := row.Scan(&lane.Count, &lane.OverdueCount, &lane.DueSoonCount, &oldest, &deadline, &lane.SampleFeedbackIDs); err != nil {
		return FeedbackTriageLane{}, fmt.Errorf("feedback triage lane %s: %w", def.Key, err)
	}
	if oldest.Valid {
		lane.OldestCreatedAt = ptrext.Of(oldest.Time)
	}
	if deadline.Valid {
		lane.NextDeadlineAt = ptrext.Of(deadline.Time)
	}
	return lane, nil
}

func feedbackTriageLaneSQL(predicate string) string {
	return fmt.Sprintf(`
		WITH lane_scope AS (
			SELECT
				uf.id,
				uf.created_at,
				uf.created_at + ($3::int * INTERVAL '1 hour') AS deadline_at
			FROM user_feedback uf
			LEFT JOIN tenant_workflow_states ws
			  ON ws.tenant_id = uf.tenant_id AND ws.id = uf.workflow_state_id
			WHERE uf.tenant_id = $1 AND (%s)
		),
		ranked AS (
			SELECT id, created_at, deadline_at,
			       row_number() OVER (ORDER BY deadline_at ASC, created_at ASC, id ASC) AS rn
			FROM lane_scope
		)
		SELECT
			COUNT(*)::bigint,
			COUNT(*) FILTER (WHERE deadline_at < $2),
			COUNT(*) FILTER (WHERE deadline_at >= $2 AND deadline_at < $2 + INTERVAL '%d hours'),
			MIN(created_at),
			MIN(deadline_at),
			COALESCE(array_agg(id ORDER BY deadline_at ASC, id ASC) FILTER (WHERE rn <= 3), '{}'::bigint[])
		FROM ranked`, predicate, int(triageDueSoonWindow.Hours()))
}

func feedbackTriageLaneDefinitions() []feedbackTriageLaneDefinition {
	return []feedbackTriageLaneDefinition{
		{
			Key:               "urgent_open",
			Label:             "Urgent open feedback",
			OwnerLane:         "support_triage",
			Severity:          "critical",
			SLAHours:          24,
			Predicate:         "uf.is_urgent = TRUE AND COALESCE(ws.category, 'open') <> 'closed'",
			RecommendedAction: "Open the oldest urgent samples, confirm impact, and move each row into an active workflow state or promoted request.",
			FilterQuery:       "urgent=true",
		},
		{
			Key:               "untriaged",
			Label:             "Untriaged intake",
			OwnerLane:         "triage_dri",
			Severity:          "high",
			SLAHours:          72,
			Predicate:         "uf.workflow_state_id IS NULL OR COALESCE(ws.category, 'open') = 'open'",
			RecommendedAction: "Classify the oldest intake, apply the right workflow state, and promote decision-ready demand to Customer Requests.",
			FilterQuery:       "workflow_category=open",
		},
		{
			Key:               "stalled_active",
			Label:             "Active work at risk",
			OwnerLane:         "product_owner",
			Severity:          "high",
			SLAHours:          168,
			Predicate:         "ws.category = 'active'",
			RecommendedAction: "Review active rows that are approaching their deadline and either close, update, or link them to execution work.",
			FilterQuery:       "workflow_category=active",
		},
		{
			Key:               "terminal_failures",
			Label:             "Terminal AI failures",
			OwnerLane:         "ai_ops",
			Severity:          "high",
			SLAHours:          48,
			Predicate:         "uf.enrichment_status = 'failed' AND uf.enrichment_attempts >= $4 AND uf.enrichment_next_retry_at IS NULL",
			RecommendedAction: "Inspect the oldest terminal failures, repair the model or prompt route, then retry only after root cause review.",
			FilterQuery:       "terminal_failed_only=true",
		},
		{
			Key:               "identity_debt",
			Label:             "Identity evidence debt",
			OwnerLane:         "data_quality",
			Severity:          "medium",
			SLAHours:          96,
			Predicate:         "COALESCE(ws.category, 'open') <> 'closed' AND NOT (" + feedbackStableIdentityPredicate() + ")",
			RecommendedAction: "Open samples without stable identity keys and capture email, external ID, CRM ID, support ID, or source contact ID before merging demand.",
			FilterQuery:       "",
		},
	}
}

func feedbackStableIdentityPredicate() string {
	return `uf.source_meta ? 'email'
		OR uf.source_meta ? 'externalId'
		OR uf.source_meta ? 'external_id'
		OR uf.source_meta ? 'crmId'
		OR uf.source_meta ? 'crm_id'
		OR uf.source_meta ? 'supportId'
		OR uf.source_meta ? 'support_id'
		OR COALESCE((uf.source_meta -> 'contact') ? 'sourceContactId', FALSE)
		OR COALESCE((uf.source_meta -> 'contact') ? 'source_contact_id', FALSE)
		OR COALESCE((uf.source_meta -> 'customer') ? 'crmId', FALSE)
		OR COALESCE((uf.source_meta -> 'customer') ? 'crm_id', FALSE)
		OR COALESCE((uf.source_meta -> 'support') ? 'zendesk_user_id', FALSE)`
}
