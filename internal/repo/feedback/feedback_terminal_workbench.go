package feedback

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const terminalFailureWorkbenchClusterLimit = 10

type terminalFailureClusterKind string

const (
	terminalFailureClusterReasonClass       terminalFailureClusterKind = "reason_class"
	terminalFailureClusterModelChannel      terminalFailureClusterKind = "model_channel"
	terminalFailureClusterConfigFingerprint terminalFailureClusterKind = "tenant_config_fingerprint"
	terminalFailureClusterAgeBucket         terminalFailureClusterKind = "age_bucket"
)

type terminalFailureClusterRow struct {
	Key               string
	Count             int64
	OldestCreatedAt   time.Time
	NewestCreatedAt   time.Time
	SampleFeedbackIDs []int64
}

// TerminalFailureCluster is one bucket in a terminal-failure workbench
// dimension. Key is stable for grouping; Label is the human-readable
// presentation value used by the console.
type TerminalFailureCluster struct {
	Key               string
	Label             string
	Count             int64
	OldestCreatedAt   time.Time
	NewestCreatedAt   time.Time
	SampleFeedbackIDs []int64
	RemediationHint   string
}

// TerminalFailureWorkbench is the server-side summary for the terminal-failure
// console workbench.
type TerminalFailureWorkbench struct {
	PeriodStart               time.Time
	PeriodEnd                 time.Time
	TotalTerminalFailures     int64
	OldestCreatedAt           *time.Time
	ReasonClassClusters       []TerminalFailureCluster
	ModelChannelClusters      []TerminalFailureCluster
	ConfigFingerprintClusters []TerminalFailureCluster
	AgeBucketClusters         []TerminalFailureCluster
}

// TerminalFailureWorkbench returns the bounded terminal-failure summary for a
// tenant in [from, to). The workbench is derived from the terminal rows already
// stored in user_feedback, so it never needs to inspect live tenant config.
func (r *FeedbackRepo) TerminalFailureWorkbench(
	ctx context.Context,
	tenantID string,
	from, to time.Time,
) (*TerminalFailureWorkbench, error) {
	if to.Before(from) {
		from, to = to, from
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to
	}
	now := to.UTC()
	out := ptrext.Of(TerminalFailureWorkbench{
		PeriodStart: from.UTC(),
		PeriodEnd:   now,
	})

	var oldest sql.NullTime
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*), MIN(created_at)
		FROM user_feedback
		WHERE tenant_id = $1
		  AND enrichment_status = 'failed'
		  AND enrichment_attempts >= $2
		  AND enrichment_next_retry_at IS NULL
		  AND created_at >= $3
		  AND created_at < $4`,
		tenantID, maxEnrichmentAttempts, from, to,
	).Scan(&out.TotalTerminalFailures, &oldest)
	if err != nil {
		return nil, fmt.Errorf("terminal failure summary: %w", err)
	}
	if oldest.Valid {
		out.OldestCreatedAt = ptrext.Of(oldest.Time)
	}

	if out.TotalTerminalFailures == 0 {
		return out, nil
	}

	reasonRows, err := r.terminalFailureClusterRows(ctx, tenantID, from, to, now, terminalFailureClusterReasonClassExpr(), false)
	if err != nil {
		return nil, err
	}
	modelRows, err := r.terminalFailureClusterRows(ctx, tenantID, from, to, now, terminalFailureClusterModelChannelExpr(), false)
	if err != nil {
		return nil, err
	}
	configRows, err := r.terminalFailureClusterRows(ctx, tenantID, from, to, now, terminalFailureClusterConfigFingerprintExpr(), false)
	if err != nil {
		return nil, err
	}
	ageRows, err := r.terminalFailureClusterRows(ctx, tenantID, from, to, now, terminalFailureClusterAgeBucketExpr(), true)
	if err != nil {
		return nil, err
	}

	out.ReasonClassClusters = decorateTerminalFailureClusters(terminalFailureClusterReasonClass, reasonRows)
	out.ModelChannelClusters = decorateTerminalFailureClusters(terminalFailureClusterModelChannel, modelRows)
	out.ConfigFingerprintClusters = decorateTerminalFailureClusters(terminalFailureClusterConfigFingerprint, configRows)
	out.AgeBucketClusters = decorateTerminalFailureClusters(terminalFailureClusterAgeBucket, ageRows)
	return out, nil
}

func (r *FeedbackRepo) terminalFailureClusterRows(
	ctx context.Context,
	tenantID string,
	from, to, now time.Time,
	clusterExpr string,
	includeNow bool,
) ([]terminalFailureClusterRow, error) {
	rows, err := r.pool.Query(ctx, terminalFailureClusterSQL(clusterExpr), terminalFailureClusterQueryArgs(tenantID, from, to, now, includeNow)...)
	if err != nil {
		return nil, fmt.Errorf("terminal failure cluster rows: %w", err)
	}
	defer rows.Close()

	var out []terminalFailureClusterRow
	for rows.Next() {
		var row terminalFailureClusterRow
		if err := rows.Scan(
			&row.Key,
			&row.Count,
			&row.OldestCreatedAt,
			&row.NewestCreatedAt,
			&row.SampleFeedbackIDs,
		); err != nil {
			return nil, fmt.Errorf("scan terminal failure cluster row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func terminalFailureClusterSQL(clusterExpr string) string {
	return fmt.Sprintf(`
		WITH terminal_scope AS (
			SELECT
				id,
				created_at,
				%s AS cluster_key
			FROM user_feedback
			WHERE tenant_id = $1
			  AND enrichment_status = 'failed'
			  AND enrichment_attempts >= $2
			  AND enrichment_next_retry_at IS NULL
			  AND created_at >= $3
			  AND created_at < $4
		),
		ranked AS (
			SELECT
				id,
				created_at,
				cluster_key,
				row_number() OVER (
					PARTITION BY cluster_key
					ORDER BY created_at DESC, id DESC
				) AS rn
			FROM terminal_scope
		)
		SELECT
			cluster_key,
			COUNT(*)::bigint AS count,
			MIN(created_at) AS oldest_created_at,
			MAX(created_at) AS newest_created_at,
			COALESCE(
				array_agg(id ORDER BY created_at DESC) FILTER (WHERE rn <= 3),
				'{}'::bigint[]
			) AS sample_feedback_ids
		FROM ranked
		GROUP BY cluster_key
		ORDER BY COUNT(*) DESC, MIN(created_at) ASC, cluster_key ASC
		LIMIT %d`,
		clusterExpr, terminalFailureWorkbenchClusterLimit)
}

func terminalFailureClusterQueryArgs(
	tenantID string,
	from, to, now time.Time,
	includeNow bool,
) []any {
	args := []any{tenantID, maxEnrichmentAttempts, from, to}
	if includeNow {
		args = append(args, now)
	}
	return args
}

func terminalFailureClusterReasonClassExpr() string {
	return "COALESCE(NULLIF(enrichment_failure_reason_class, ''), 'other_err')"
}

func terminalFailureClusterModelChannelExpr() string {
	return "COALESCE(NULLIF(enrichment_failure_model, ''), '(unknown model)') || ' @ ' || COALESCE(NULLIF(enrichment_failure_channel_name, ''), NULLIF(enrichment_failure_channel_id, ''), '(unknown channel)')"
}

func terminalFailureClusterConfigFingerprintExpr() string {
	return "COALESCE(NULLIF(enrichment_failure_config_fingerprint, ''), '(unknown fingerprint)')"
}

func terminalFailureClusterAgeBucketExpr() string {
	return `CASE
		WHEN $5 - created_at < INTERVAL '1 hour' THEN '0-1h'
		WHEN $5 - created_at < INTERVAL '24 hours' THEN '1-24h'
		WHEN $5 - created_at < INTERVAL '7 days' THEN '1-7d'
		ELSE '7d+'
	END`
}

func decorateTerminalFailureClusters(kind terminalFailureClusterKind, rows []terminalFailureClusterRow) []TerminalFailureCluster {
	out := make([]TerminalFailureCluster, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			key = "(unknown)"
		}
		if len(row.SampleFeedbackIDs) > 3 {
			row.SampleFeedbackIDs = row.SampleFeedbackIDs[:3]
		}
		out = append(out, TerminalFailureCluster{
			Key:               key,
			Label:             terminalFailureClusterLabel(kind, key),
			Count:             row.Count,
			OldestCreatedAt:   row.OldestCreatedAt,
			NewestCreatedAt:   row.NewestCreatedAt,
			SampleFeedbackIDs: row.SampleFeedbackIDs,
			RemediationHint:   terminalFailureRemediationHint(kind, key),
		})
	}
	return out
}

func terminalFailureClusterLabel(kind terminalFailureClusterKind, key string) string {
	switch kind {
	case terminalFailureClusterReasonClass:
		switch key {
		case "llm_err":
			return "LLM error"
		case "parse_err":
			return "Parse error"
		case "other_err":
			return "Other error"
		default:
			return key
		}
	default:
		return key
	}
}

func terminalFailureRemediationHint(kind terminalFailureClusterKind, key string) string {
	switch kind {
	case terminalFailureClusterReasonClass:
		switch key {
		case "llm_err":
			return "Check the routed LLM channel and provider health."
		case "parse_err":
			return "Compare the prompt contract with the structured output schema."
		default:
			return "Open one sample row and compare the raw error text with nearby failures."
		}
	case terminalFailureClusterModelChannel:
		return "Review the routed model, channel config, and credentials for this combination."
	case terminalFailureClusterConfigFingerprint:
		return "Compare this fingerprint with the current tenant prompt policy before retrying."
	case terminalFailureClusterAgeBucket:
		switch key {
		case "0-1h":
			return "Triage the freshest failures first while the context is still warm."
		case "1-24h":
			return "Check recent config or provider changes before bulk retrying."
		case "1-7d":
			return "Compare against the active prompt policy and retry only after root cause review."
		default:
			return "This cluster is stale; prioritize config or provider remediation before mass retry."
		}
	default:
		return ""
	}
}
