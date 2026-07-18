package feedback

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecorateTerminalFailureClusters_ReasonClassPresentation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	clusters := decorateTerminalFailureClusters(terminalFailureClusterReasonClass, []terminalFailureClusterRow{{
		Key:               "llm_err",
		Count:             4,
		OldestCreatedAt:   now,
		NewestCreatedAt:   now.Add(2 * time.Hour),
		SampleFeedbackIDs: []int64{101, 102, 103, 104},
	}})

	require.Len(t, clusters, 1)
	require.Equal(t, "llm_err", clusters[0].Key)
	require.Equal(t, "LLM error", clusters[0].Label)
	require.Equal(t, "Check the routed LLM channel and provider health.", clusters[0].RemediationHint)
	require.Equal(t, []int64{101, 102, 103}, clusters[0].SampleFeedbackIDs)
}

func TestDecorateTerminalFailureClusters_ModelChannelPresentation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	clusters := decorateTerminalFailureClusters(terminalFailureClusterModelChannel, []terminalFailureClusterRow{{
		Key:               "claude-3.5-sonnet @ channel-a",
		Count:             2,
		OldestCreatedAt:   now,
		NewestCreatedAt:   now,
		SampleFeedbackIDs: []int64{201},
	}})

	require.Len(t, clusters, 1)
	require.Equal(t, "claude-3.5-sonnet @ channel-a", clusters[0].Label)
	require.Equal(t, "Review the routed model, channel config, and credentials for this combination.", clusters[0].RemediationHint)
}

func TestTerminalFailureClusterQueryArgs_LengthMatchesDimension(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	require.Len(t, terminalFailureClusterQueryArgs("tenant-1", now.Add(-time.Hour), now, now, false), 4)
	require.Len(t, terminalFailureClusterQueryArgs("tenant-1", now.Add(-time.Hour), now, now, true), 5)
}

func TestTerminalFailureClusterSQLAndExpressions(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{
		terminalFailureClusterReasonClassExpr(),
		terminalFailureClusterModelChannelExpr(),
		terminalFailureClusterConfigFingerprintExpr(),
		terminalFailureClusterAgeBucketExpr(),
	} {
		require.NotEmpty(t, expr)
		sql := terminalFailureClusterSQL(expr)
		require.Contains(t, sql, expr)
		require.Contains(t, sql, "terminal_scope")
		require.Contains(t, sql, "enrichment_attempts >= $2")
		require.Contains(t, sql, "LIMIT 10")
	}

	require.Contains(t, terminalFailureClusterReasonClassExpr(), "enrichment_failure_reason_class")
	require.Contains(t, terminalFailureClusterModelChannelExpr(), "enrichment_failure_channel_name")
	require.Contains(t, terminalFailureClusterConfigFingerprintExpr(), "enrichment_failure_config_fingerprint")
	require.True(t, strings.Contains(terminalFailureClusterAgeBucketExpr(), "$5 - created_at"))
}
