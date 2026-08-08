package feedback

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFeedbackTriageLaneArgsMatchPredicatePlaceholders(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	for _, definition := range feedbackTriageLaneDefinitions() {
		definition := definition
		t.Run(definition.Key, func(t *testing.T) {
			t.Parallel()

			args := feedbackTriageLaneArgs("tenant-a", now, definition)
			query := feedbackTriageLaneSQL(definition.Predicate)
			if definition.RequiresMaxEnrichmentAttempts {
				require.Len(t, args, 4)
				require.Contains(t, query, "$4")
				require.Equal(t, maxEnrichmentAttempts, args[3])
				return
			}
			require.Len(t, args, 3)
			require.NotContains(t, query, "$4")
		})
	}
}
