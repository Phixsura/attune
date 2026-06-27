// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func Test_reArm(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	created := now.Add(-time.Hour)
	delivered := now.Add(-30 * time.Minute)
	manualRetryAt := ptrext.Of(now)

	before := OutboxRow{
		ID:                42,
		FeedbackID:        100,
		TenantID:          "tenant-1",
		DestinationType:   "raw_webhook",
		DestinationTarget: "https://example.com/hook",
		Audience:          "ops",
		Payload:           []byte(`{"msg":"hello"}`),
		Status:            OutboxStatusDead,
		Attempts:          5,
		TraceID:           "trace-abc",
		LastError:         "connection refused",
		DeadReason:        "max retries exceeded",
		FailureKind:       "network",
		HTTPStatus:        503,
		NextRetryAt:       now.Add(-10 * time.Minute),
		CreatedAt:         created,
		DeliveredAt:       ptrext.Of(delivered),
		ClaimedAt:         ptrext.Of(now.Add(-5 * time.Minute)),
		LastManualRetryAt: nil,
		RetriedBy:         "",
		ManualRetryCount:  2,
	}

	t.Run("resets terminal fields", func(t *testing.T) {
		t.Parallel()
		after := reArm(before, "admin@example.com", now, manualRetryAt)

		require.Equal(t, OutboxStatusPending, after.Status)
		require.Equal(t, 0, after.Attempts)
		require.Empty(t, after.LastError)
		require.Empty(t, after.DeadReason)
		require.Empty(t, after.FailureKind)
		require.Equal(t, 0, after.HTTPStatus)
		require.Nil(t, after.ClaimedAt)
	})

	t.Run("stamps retry bookkeeping", func(t *testing.T) {
		t.Parallel()
		after := reArm(before, "admin@example.com", now, manualRetryAt)

		require.Equal(t, now, after.NextRetryAt)
		require.Equal(t, manualRetryAt, after.LastManualRetryAt)
		require.Equal(t, "admin@example.com", after.RetriedBy)
		require.Equal(t, 3, after.ManualRetryCount) // before.ManualRetryCount + 1
	})

	t.Run("preserves identity and envelope fields", func(t *testing.T) {
		t.Parallel()
		after := reArm(before, "admin@example.com", now, manualRetryAt)

		require.Equal(t, before.ID, after.ID)
		require.Equal(t, before.FeedbackID, after.FeedbackID)
		require.Equal(t, before.TenantID, after.TenantID)
		require.Equal(t, before.DestinationType, after.DestinationType)
		require.Equal(t, before.DestinationTarget, after.DestinationTarget)
		require.Equal(t, before.Audience, after.Audience)
		require.Equal(t, before.Payload, after.Payload)
		require.Equal(t, before.TraceID, after.TraceID)
		require.Equal(t, before.CreatedAt, after.CreatedAt)
		require.Equal(t, before.DeliveredAt, after.DeliveredAt)
	})
}

func Test_nullStr(t *testing.T) {
	t.Parallel()

	t.Run("empty string returns nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, nullStr(""))
	})

	t.Run("non-empty string returns itself", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "hello", nullStr("hello"))
	})
}

func Test_nullInt(t *testing.T) {
	t.Parallel()

	t.Run("zero returns nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, nullInt(0))
	})

	t.Run("positive returns itself", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, 42, nullInt(42))
	})

	t.Run("negative returns itself", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, -1, nullInt(-1))
	})
}
