// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegisterMetrics_NilGuards(t *testing.T) {
	// Must not panic and must register nothing when reg or pool is nil.
	RegisterMetrics(nil, nil)
	RegisterMetrics(prometheus.NewRegistry(), nil)
}
