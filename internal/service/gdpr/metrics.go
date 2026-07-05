// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
)

const (
	gdprRequestTypeExport = "export"
	gdprRequestTypeDelete = "delete"

	gdprJobResultStarted   = "started"
	gdprJobResultCompleted = "completed"
	gdprJobResultFailed    = "failed"
	gdprJobResultCancelled = "cancelled"
	gdprJobResultRevoked   = "revoked"
)

func recordGDPRJobStart(tenantID, requestType string) {
	metrics.GDPRJobTotal.WithLabelValues(normalizeGDPRMetricLabel(tenantID), normalizeGDPRMetricLabel(requestType), gdprJobResultStarted).Inc()
}

func recordGDPRJobTerminal(tenantID, requestType, result string, startedAt time.Time) {
	tenantID = normalizeGDPRMetricLabel(tenantID)
	requestType = normalizeGDPRMetricLabel(requestType)
	result = normalizeGDPRMetricLabel(result)

	metrics.GDPRJobTotal.WithLabelValues(tenantID, requestType, result).Inc()
	if startedAt.IsZero() {
		return
	}
	metrics.GDPRJobDuration.WithLabelValues(tenantID, requestType).Observe(time.Since(startedAt).Seconds())
}

func normalizeGDPRMetricLabel(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}
