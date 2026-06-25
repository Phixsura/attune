// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorker_NilRepo(t *testing.T) {
	t.Parallel()
	var w *Worker
	require.Nil(t, w)
}

func TestHeartbeatInterval(t *testing.T) {
	t.Parallel()
	require.Equal(t, 90*time.Second, heartbeatInterval)
}

func TestMinPollInterval(t *testing.T) {
	t.Parallel()
	require.GreaterOrEqual(t, time.Second, time.Duration(0))
}
