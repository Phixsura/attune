// SPDX-License-Identifier: Apache-2.0

// Package system implements Console system administration endpoints.
package system

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// RecoveryHandler serves the latest restore-drill recoverability state.
type RecoveryHandler struct {
	pool lastRunReader
}

// RecoveryInfo describes the latest restore-drill state for operators.
type RecoveryInfo struct {
	Status                 string                `json:"status"`
	Message                string                `json:"message"`
	Remediation            string                `json:"remediation,omitempty"`
	FreshnessWindowSeconds int64                 `json:"freshnessWindowSeconds"`
	AgeSeconds             *int64                `json:"ageSeconds,omitempty"`
	LastRun                *restoredrill.LastRun `json:"lastRun,omitempty"`
}

// NewRecoveryHandler creates a handler that serves recovery metadata.
func NewRecoveryHandler(pool lastRunReader) *RecoveryHandler {
	return ptrext.Of(RecoveryHandler{pool: pool})
}

// ServeHTTP returns the latest restore-drill state as JSON.
func (h *RecoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info := RecoveryInfo{
		Status:                 string(restoredrill.StatusWarn),
		Message:                "Recovery history unavailable",
		Remediation:            "Ensure database.url is reachable so restore history can be inspected.",
		FreshnessWindowSeconds: int64(restoredrill.DefaultFreshnessWindow / time.Second),
	}
	if h == nil || h.pool == nil {
		writeRecoveryJSON(w, info)
		return
	}
	last, ok, err := restoredrill.ReadLast(r.Context(), h.pool)
	if err != nil {
		writeRecoveryJSON(w, info)
		return
	}
	age := time.Since(last.RanAt)
	assessment := restoredrill.AssessLastRun(ok, last, age, restoredrill.DefaultFreshnessWindow)
	info.Status = string(assessment.Status)
	info.Message = assessment.Message
	info.Remediation = assessment.Remediation
	if ok {
		info.LastRun = ptrext.Of(last)
		ageSeconds := int64(age.Seconds())
		info.AgeSeconds = ptrext.Of(ageSeconds)
	}
	writeRecoveryJSON(w, info)
}

type lastRunReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func writeRecoveryJSON(w http.ResponseWriter, info RecoveryInfo) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(info)
}
