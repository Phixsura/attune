// SPDX-License-Identifier: Apache-2.0

package enrichmentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	defaultPolicyKey string = "default"
	specVersion      int32  = 1

	policyLockKey int64 = 0x45525443504C414E // "ERTCPLAN"
)

var (
	ErrPolicyNotFound        = errors.New("enrichment runtime policy not found")
	ErrVersionConflict       = errors.New("enrichment runtime version conflict")
	ErrRollbackTargetMissing = errors.New("enrichment runtime rollback target not found")
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return ptrext.Of(Repo{pool: pool}) }

type Spec struct {
	QueueLen            int32
	Workers             int32
	BatchSize           int32
	BatchWindow         time.Duration
	SweepInterval       time.Duration
	LLMRateLimitEnabled bool
	LLMMaxQPS           float64
	LLMBurst            int32
}

type Policy struct {
	Spec                     Spec
	Version                  uint64
	UpdatedAt                time.Time
	UpdatedBy                string
	UpdateReason             string
	BootstrapSnapshotVersion string
	SpecVersion              uint32
	LastKnownGoodVersion     uint64
}

type HistoryEntry struct {
	Policy
	OperationType   string
	ChangedAt       time.Time
	ActorType       string
	ActorID         string
	ActorIP         string
	ActorUserAgent  string
	SessionID       string
	RiskLevel       string
	SourceVersion   uint64
	TargetVersion   uint64
	RollbackLineage string
}

type MutationMeta struct {
	ActorType       string
	ActorID         string
	ActorIP         string
	ActorUserAgent  string
	SessionID       string
	RiskLevel       string
	Operation       string
	SourceVersion   uint64
	TargetVersion   uint64
	RollbackLineage string
}

type InstanceStatus struct {
	InstanceID              string
	BootID                  string
	DesiredVersion          uint64
	ObservedDesiredVersion  uint64
	RunnerEffectiveVersion  uint64
	LimiterEffectiveVersion uint64
	AttemptedRunnerVersion  uint64
	AttemptedLimiterVersion uint64
	RunnerApplyStatus       string
	LimiterApplyStatus      string
	RunnerLastApplyError    string
	LimiterLastApplyError   string
	QueueDepth              int32
	QueueCapacityTarget     int32
	QueueCapacityEffective  int32
	QueueResizePending      bool
	InFlight                int32
	DegradedReason          string
	AppliedSpec             Spec
	HeartbeatInterval       time.Duration
	StaleAfter              time.Duration
	ExpireAfter             time.Duration
	LastAppliedAt           *time.Time
	LastReconciledAt        time.Time
	LastSeenAt              time.Time
}

func (r *Repo) GetPolicy(ctx context.Context) (Policy, error) {
	row := r.pool.QueryRow(
		ctx, `
		SELECT queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
		       llm_rate_limit_enabled, llm_max_qps, llm_burst,
		       version, updated_at, updated_by, update_reason,
		       bootstrap_snapshot_version, spec_version, last_known_good_version
		  FROM enrichment_runtime_policy
		 WHERE policy_key = $1`,
		defaultPolicyKey,
	)
	return scanPolicy(row)
}

func (r *Repo) GetHistoryVersion(ctx context.Context, version uint64) (Policy, error) {
	row := r.pool.QueryRow(
		ctx, `
		SELECT queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
		       llm_rate_limit_enabled, llm_max_qps, llm_burst,
		       version, changed_at, changed_by, change_reason,
		       bootstrap_snapshot_version, spec_version, last_known_good_version
		  FROM enrichment_runtime_policy_history
		 WHERE policy_key = $1 AND version = $2`,
		defaultPolicyKey, version,
	)
	p, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrPolicyNotFound) {
		return Policy{}, ErrRollbackTargetMissing
	}
	return p, err
}

func (r *Repo) ListHistory(ctx context.Context, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(
		ctx, `
		SELECT queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
		       llm_rate_limit_enabled, llm_max_qps, llm_burst,
		       version, changed_at, changed_by, change_reason,
		       bootstrap_snapshot_version, spec_version, last_known_good_version,
		       operation_type, changed_at, actor_type, actor_id, actor_ip,
		       actor_user_agent, session_id, risk_level, source_version, target_version, rollback_lineage
		  FROM enrichment_runtime_policy_history
		 WHERE policy_key = $1
		 ORDER BY version DESC
		 LIMIT $2`,
		defaultPolicyKey, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HistoryEntry
	for rows.Next() {
		var (
			item     HistoryEntry
			bwMS     int64
			sweepMS  int64
			specV    int32
			lastGood uint64
		)
		if err := rows.Scan(
			&item.Spec.QueueLen, &item.Spec.Workers, &item.Spec.BatchSize, &bwMS, &sweepMS,
			&item.Spec.LLMRateLimitEnabled, &item.Spec.LLMMaxQPS, &item.Spec.LLMBurst,
			&item.Version, &item.UpdatedAt, &item.UpdatedBy, &item.UpdateReason,
			&item.BootstrapSnapshotVersion, &specV, &lastGood,
			&item.OperationType, &item.ChangedAt, &item.ActorType, &item.ActorID, &item.ActorIP,
			&item.ActorUserAgent, &item.SessionID, &item.RiskLevel, &item.SourceVersion, &item.TargetVersion, &item.RollbackLineage,
		); err != nil {
			return nil, err
		}
		item.Spec.BatchWindow = time.Duration(bwMS) * time.Millisecond
		item.Spec.SweepInterval = time.Duration(sweepMS) * time.Millisecond
		item.SpecVersion = uint32(specV)
		item.LastKnownGoodVersion = lastGood
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) SavePolicyCAS(ctx context.Context, expectedVersion uint64, next Policy, meta MutationMeta) (Policy, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Policy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, policyLockKey); err != nil {
		return Policy{}, fmt.Errorf("policy advisory lock: %w", err)
	}

	current, found, err := getPolicyTx(ctx, tx)
	if err != nil {
		return Policy{}, err
	}
	if !found {
		if expectedVersion != 0 {
			return Policy{}, ErrVersionConflict
		}
		next.Version = 1
	} else {
		if current.Version != expectedVersion {
			return Policy{}, ErrVersionConflict
		}
		next.Version = current.Version + 1
	}
	if next.SpecVersion == 0 {
		next.SpecVersion = uint32(specVersion)
	}
	if next.UpdatedAt.IsZero() {
		next.UpdatedAt = time.Now().UTC()
	}

	err = upsertPolicyTx(ctx, tx, next, found)
	if err != nil {
		return Policy{}, err
	}

	meta.TargetVersion = next.Version
	_, err = tx.Exec(
		ctx, `
		INSERT INTO enrichment_runtime_policy_history (
			policy_key, version, operation_type,
			queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
			llm_rate_limit_enabled, llm_max_qps, llm_burst, bootstrap_snapshot_version,
			spec_version, last_known_good_version, changed_at, changed_by, change_reason,
			actor_type, actor_id, actor_ip, actor_user_agent, session_id, risk_level,
			source_version, target_version, rollback_lineage
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26
		)`,
		defaultPolicyKey, next.Version, meta.Operation,
		next.Spec.QueueLen, next.Spec.Workers, next.Spec.BatchSize,
		next.Spec.BatchWindow.Milliseconds(), next.Spec.SweepInterval.Milliseconds(),
		next.Spec.LLMRateLimitEnabled, sanitizeQPS(next.Spec.LLMMaxQPS), next.Spec.LLMBurst,
		next.BootstrapSnapshotVersion, next.SpecVersion, next.LastKnownGoodVersion,
		next.UpdatedAt, next.UpdatedBy, next.UpdateReason,
		meta.ActorType, meta.ActorID, meta.ActorIP, meta.ActorUserAgent, meta.SessionID, meta.RiskLevel,
		meta.SourceVersion, meta.TargetVersion, meta.RollbackLineage,
	)
	if err != nil {
		return Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Policy{}, err
	}
	return next, nil
}

func upsertPolicyTx(ctx context.Context, tx pgx.Tx, next Policy, found bool) error {
	if !found {
		_, err := tx.Exec(
			ctx, `
			INSERT INTO enrichment_runtime_policy (
				policy_key, queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
				llm_rate_limit_enabled, llm_max_qps, llm_burst, bootstrap_snapshot_version,
				spec_version, last_known_good_version, version, updated_at, updated_by, update_reason
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			defaultPolicyKey, next.Spec.QueueLen, next.Spec.Workers, next.Spec.BatchSize,
			next.Spec.BatchWindow.Milliseconds(), next.Spec.SweepInterval.Milliseconds(),
			next.Spec.LLMRateLimitEnabled, sanitizeQPS(next.Spec.LLMMaxQPS), next.Spec.LLMBurst,
			next.BootstrapSnapshotVersion, next.SpecVersion, next.LastKnownGoodVersion,
			next.Version, next.UpdatedAt, next.UpdatedBy, next.UpdateReason,
		)
		return err
	}

	_, err := tx.Exec(
		ctx, `
		UPDATE enrichment_runtime_policy
		   SET queue_len = $2,
		       workers = $3,
		       batch_size = $4,
		       batch_window_ms = $5,
		       sweep_interval_ms = $6,
		       llm_rate_limit_enabled = $7,
		       llm_max_qps = $8,
		       llm_burst = $9,
		       bootstrap_snapshot_version = $10,
		       spec_version = $11,
		       last_known_good_version = $12,
		       version = $13,
		       updated_at = $14,
		       updated_by = $15,
		       update_reason = $16
		 WHERE policy_key = $1`,
		defaultPolicyKey, next.Spec.QueueLen, next.Spec.Workers, next.Spec.BatchSize,
		next.Spec.BatchWindow.Milliseconds(), next.Spec.SweepInterval.Milliseconds(),
		next.Spec.LLMRateLimitEnabled, sanitizeQPS(next.Spec.LLMMaxQPS), next.Spec.LLMBurst,
		next.BootstrapSnapshotVersion, next.SpecVersion, next.LastKnownGoodVersion,
		next.Version, next.UpdatedAt, next.UpdatedBy, next.UpdateReason,
	)
	return err
}

func (r *Repo) UpsertInstanceStatus(ctx context.Context, status InstanceStatus) error {
	specJSON, err := json.Marshal(status.AppliedSpec)
	if err != nil {
		return fmt.Errorf("marshal applied spec: %w", err)
	}
	_, err = r.pool.Exec(
		ctx, `
		INSERT INTO enrichment_runtime_instance_status (
			instance_id, boot_id, desired_version, observed_desired_version,
			runner_effective_version, limiter_effective_version,
			attempted_runner_version, attempted_limiter_version,
			runner_apply_status, limiter_apply_status,
			runner_last_apply_error, limiter_last_apply_error,
			queue_depth, queue_capacity_target, queue_capacity_effective, queue_resize_pending,
			in_flight, degraded_reason, applied_spec_json,
			heartbeat_interval_ms, stale_after_ms, expire_after_ms,
			last_applied_at, last_reconciled_at, last_seen_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25
		)
		ON CONFLICT (instance_id) DO UPDATE SET
			boot_id = EXCLUDED.boot_id,
			desired_version = EXCLUDED.desired_version,
			observed_desired_version = EXCLUDED.observed_desired_version,
			runner_effective_version = EXCLUDED.runner_effective_version,
			limiter_effective_version = EXCLUDED.limiter_effective_version,
			attempted_runner_version = EXCLUDED.attempted_runner_version,
			attempted_limiter_version = EXCLUDED.attempted_limiter_version,
			runner_apply_status = EXCLUDED.runner_apply_status,
			limiter_apply_status = EXCLUDED.limiter_apply_status,
			runner_last_apply_error = EXCLUDED.runner_last_apply_error,
			limiter_last_apply_error = EXCLUDED.limiter_last_apply_error,
			queue_depth = EXCLUDED.queue_depth,
			queue_capacity_target = EXCLUDED.queue_capacity_target,
			queue_capacity_effective = EXCLUDED.queue_capacity_effective,
			queue_resize_pending = EXCLUDED.queue_resize_pending,
			in_flight = EXCLUDED.in_flight,
			degraded_reason = EXCLUDED.degraded_reason,
			applied_spec_json = EXCLUDED.applied_spec_json,
			heartbeat_interval_ms = EXCLUDED.heartbeat_interval_ms,
			stale_after_ms = EXCLUDED.stale_after_ms,
			expire_after_ms = EXCLUDED.expire_after_ms,
			last_applied_at = EXCLUDED.last_applied_at,
			last_reconciled_at = EXCLUDED.last_reconciled_at,
			last_seen_at = EXCLUDED.last_seen_at`,
		status.InstanceID, status.BootID, status.DesiredVersion, status.ObservedDesiredVersion,
		status.RunnerEffectiveVersion, status.LimiterEffectiveVersion,
		status.AttemptedRunnerVersion, status.AttemptedLimiterVersion,
		status.RunnerApplyStatus, status.LimiterApplyStatus,
		status.RunnerLastApplyError, status.LimiterLastApplyError,
		status.QueueDepth, status.QueueCapacityTarget, status.QueueCapacityEffective, status.QueueResizePending,
		status.InFlight, status.DegradedReason, specJSON,
		status.HeartbeatInterval.Milliseconds(), status.StaleAfter.Milliseconds(), status.ExpireAfter.Milliseconds(),
		status.LastAppliedAt, status.LastReconciledAt, status.LastSeenAt,
	)
	return err
}

func (r *Repo) ListInstanceStatuses(ctx context.Context) ([]InstanceStatus, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT instance_id, boot_id, desired_version, observed_desired_version,
		       runner_effective_version, limiter_effective_version,
		       attempted_runner_version, attempted_limiter_version,
		       runner_apply_status, limiter_apply_status,
		       runner_last_apply_error, limiter_last_apply_error,
		       queue_depth, queue_capacity_target, queue_capacity_effective, queue_resize_pending,
		       in_flight, degraded_reason, applied_spec_json,
		       heartbeat_interval_ms, stale_after_ms, expire_after_ms,
		       last_applied_at, last_reconciled_at, last_seen_at
		  FROM enrichment_runtime_instance_status
		 ORDER BY instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []InstanceStatus
	for rows.Next() {
		var (
			item    InstanceStatus
			specRaw []byte
			hbMS    int64
			staleMS int64
			expMS   int64
		)
		if err := rows.Scan(
			&item.InstanceID, &item.BootID, &item.DesiredVersion, &item.ObservedDesiredVersion,
			&item.RunnerEffectiveVersion, &item.LimiterEffectiveVersion,
			&item.AttemptedRunnerVersion, &item.AttemptedLimiterVersion,
			&item.RunnerApplyStatus, &item.LimiterApplyStatus,
			&item.RunnerLastApplyError, &item.LimiterLastApplyError,
			&item.QueueDepth, &item.QueueCapacityTarget, &item.QueueCapacityEffective, &item.QueueResizePending,
			&item.InFlight, &item.DegradedReason, &specRaw,
			&hbMS, &staleMS, &expMS,
			&item.LastAppliedAt, &item.LastReconciledAt, &item.LastSeenAt,
		); err != nil {
			return nil, err
		}
		item.HeartbeatInterval = time.Duration(hbMS) * time.Millisecond
		item.StaleAfter = time.Duration(staleMS) * time.Millisecond
		item.ExpireAfter = time.Duration(expMS) * time.Millisecond
		if len(specRaw) > 0 {
			if err := json.Unmarshal(specRaw, &item.AppliedSpec); err != nil {
				return nil, fmt.Errorf("decode applied spec for %s: %w", item.InstanceID, err)
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func getPolicyTx(ctx context.Context, tx pgx.Tx) (Policy, bool, error) {
	row := tx.QueryRow(
		ctx, `
		SELECT queue_len, workers, batch_size, batch_window_ms, sweep_interval_ms,
		       llm_rate_limit_enabled, llm_max_qps, llm_burst,
		       version, updated_at, updated_by, update_reason,
		       bootstrap_snapshot_version, spec_version, last_known_good_version
		  FROM enrichment_runtime_policy
		 WHERE policy_key = $1`,
		defaultPolicyKey,
	)
	p, err := scanPolicy(row)
	if errors.Is(err, ErrPolicyNotFound) {
		return Policy{}, false, nil
	}
	return p, true, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPolicy(row rowScanner) (Policy, error) {
	var (
		p        Policy
		bwMS     int64
		sweepMS  int64
		specV    int32
		lastGood uint64
	)
	err := row.Scan(
		&p.Spec.QueueLen, &p.Spec.Workers, &p.Spec.BatchSize, &bwMS, &sweepMS,
		&p.Spec.LLMRateLimitEnabled, &p.Spec.LLMMaxQPS, &p.Spec.LLMBurst,
		&p.Version, &p.UpdatedAt, &p.UpdatedBy, &p.UpdateReason,
		&p.BootstrapSnapshotVersion, &specV, &lastGood,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, ErrPolicyNotFound
	}
	if err != nil {
		return Policy{}, err
	}
	p.Spec.BatchWindow = time.Duration(bwMS) * time.Millisecond
	p.Spec.SweepInterval = time.Duration(sweepMS) * time.Millisecond
	p.SpecVersion = uint32(specV)
	p.LastKnownGoodVersion = lastGood
	return p, nil
}

func sanitizeQPS(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}
