// SPDX-License-Identifier: Apache-2.0

// Package restoredrill verifies that a restored attune PostgreSQL database is
// actually recoverable: its schema / migration state, row counts, pgvector
// extension, and — critically — that the managed Tink-encrypted secrets in it
// can still be decrypted with the operator's live keyset.
//
// The package targets an ALREADY-RESTORED database (a throwaway target), never
// production traffic. It composes the existing internal/infra/database
// verifiers and internal/infra/secretstore decryption rather than
// reimplementing them. The drill produces a DrillReport — the audit-evidence
// artifact — which the CLI serializes and (optionally) records to the
// production restore_drill_runs table for the readiness surface to read.
package restoredrill

import "time"

// Status is the outcome of a single check or the drill as a whole. Mirrors the
// internal/preflight status vocabulary so reports read consistently.
type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skipped"
)

// CheckResult is one verifier's verdict. Detail carries structured,
// non-sensitive context for audit evidence and MUST NOT contain decrypted
// plaintext. Detail shapes by check:
//   - row_counts:          {restored, baseline}  (per-table counts)
//   - decryptability:      {llm_total, llm_sampled, inbound_total, inbound_sampled, decrypted}
//   - schema:              {total, applied, pending}
//   - recovery_objectives: {rpo_seconds, rto_seconds, rpo_target_seconds, rto_target_seconds}
//   - deep:                {indexes_checked, indexes_total}
//   - others:              nil
type CheckResult struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

// DrillReport is the audit-evidence artifact for one restore drill. The CLI
// emits it as JSON; the summary (status, timing, backup_ref) is persisted to
// restore_drill_runs.
type DrillReport struct {
	Status        Status    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int64     `json:"duration_ms"`
	BackupRef     string    `json:"backup_ref,omitempty"`
	SchemaVersion int       `json:"schema_version"`
	// RPOSeconds is the data-loss window (backup age at drill time); RTOSeconds
	// is the measured restore duration. Both nil when not supplied by the caller.
	RPOSeconds    *int64        `json:"rpo_seconds,omitempty"`
	RTOSeconds    *int64        `json:"rto_seconds,omitempty"`
	Checks        []CheckResult `json:"checks"`
	AttuneVersion string        `json:"attune_version,omitempty"`
}

// Aggregate returns the worst status among results, ranked
// fail > warn > pass > skipped. Empty or all-skipped input yields StatusSkip.
func Aggregate(results []CheckResult) Status {
	worst := StatusSkip
	rank := map[Status]int{StatusSkip: 0, StatusPass: 1, StatusWarn: 2, StatusFail: 3}
	for _, r := range results {
		if rank[r.Status] > rank[worst] {
			worst = r.Status
		}
	}
	return worst
}
