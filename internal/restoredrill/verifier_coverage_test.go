// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunReportsConnectivityFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	startedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)

	report := Run(ctx, newUnreachableVerifierPool(t), nil, startedAt, Options{BackupRef: "backup-1"})
	if report.Status != StatusFail {
		t.Fatalf("Run() status = %q, want fail", report.Status)
	}
	if len(report.Checks) != 1 {
		t.Fatalf("Run() checks = %d, want only connectivity", len(report.Checks))
	}
	if report.Checks[0].Name != "connectivity" || report.Checks[0].Status != StatusFail {
		t.Fatalf("Run() connectivity check = %+v, want failed connectivity", report.Checks[0])
	}
	if report.RPOSeconds != nil || report.RTOSeconds != nil {
		t.Fatalf("Run() recovery objective fields = (%v, %v), want nil on failed connectivity", report.RPOSeconds, report.RTOSeconds)
	}
}

func TestVerifierDatabaseFailureBranches(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableVerifierPool(t)

	if got := checkConnectivity(ctx, pool); got.Status != StatusFail {
		t.Fatalf("checkConnectivity() = %+v, want fail", got)
	}
	if got, version := gatherSchema(ctx, pool); got.Status != StatusFail || version != 0 {
		t.Fatalf("gatherSchema() = (%+v, %d), want fail and version 0", got, version)
	}
	if got := checkPgvector(ctx, pool); got.Status != StatusFail {
		t.Fatalf("checkPgvector() = %+v, want fail", got)
	}
	if got := checkDeep(ctx, pool); got.Status != StatusFail {
		t.Fatalf("checkDeep() = %+v, want fail", got)
	}
	if _, _, _, err := amcheckBTrees(ctx, pool); err == nil {
		t.Fatal("amcheckBTrees() error = nil, want query error")
	}
	if _, err := btreeIndexNames(ctx, pool); err == nil {
		t.Fatal("btreeIndexNames() error = nil, want query error")
	}
	if _, err := sampleKNN(ctx, pool); err == nil {
		t.Fatal("sampleKNN() error = nil, want query error")
	}
	if got := gatherRowCounts(ctx, pool, nil); got.Status != StatusFail {
		t.Fatalf("gatherRowCounts() = %+v, want fail", got)
	}
	if _, err := BaselineCounts(ctx, pool); err == nil {
		t.Fatal("BaselineCounts() error = nil, want query error")
	}
	if _, err := countQuery(ctx, pool, `SELECT 1`); err == nil {
		t.Fatal("countQuery() error = nil, want query error")
	}
	if _, err := distinctLLMKeyRefs(ctx, pool); err == nil {
		t.Fatal("distinctLLMKeyRefs() error = nil, want query error")
	}
	if _, _, _, qerr := verifyDecryptAllLLM(ctx, pool, nil); qerr == nil {
		t.Fatal("verifyDecryptAllLLM() queryErr = nil, want query error")
	}
	if _, _, _, qerr := verifyDecryptAllInbound(ctx, pool, nil); qerr == nil {
		t.Fatal("verifyDecryptAllInbound() queryErr = nil, want query error")
	}
}

func TestVerifierIntegrityAndFailureModeDatabaseErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableVerifierPool(t)

	if got := gatherEncoding(ctx, pool); got.Status != StatusFail {
		t.Fatalf("gatherEncoding() = %+v, want fail", got)
	}
	if got := gatherConstraints(ctx, pool, nil); got.Status != StatusFail {
		t.Fatalf("gatherConstraints() = %+v, want fail", got)
	}
	if _, err := unvalidatedConstraintCount(ctx, pool); err == nil {
		t.Fatal("unvalidatedConstraintCount() error = nil, want query error")
	}
	if _, err := BaselineUnvalidatedCount(ctx, pool); err == nil {
		t.Fatal("BaselineUnvalidatedCount() error = nil, want query error")
	}
	if got := gatherSequences(ctx, pool); got.Status != StatusFail {
		t.Fatalf("gatherSequences() = %+v, want fail", got)
	}
	if _, err := sequenceOwners(ctx, pool); err == nil {
		t.Fatal("sequenceOwners() error = nil, want query error")
	}
	ok, behind := sequenceBehind(ctx, pool, seqRef{schema: "public", seq: "feedback_id_seq", tbl: "user_feedback", col: "id"})
	if ok || behind {
		t.Fatalf("sequenceBehind() = (%t, %t), want unreadable sequence", ok, behind)
	}
	if got := gatherExtensions(ctx, pool, []string{"vector"}); got.Status != StatusFail {
		t.Fatalf("gatherExtensions() = %+v, want fail", got)
	}
	if _, err := extensionNames(ctx, pool); err == nil {
		t.Fatal("extensionNames() error = nil, want query error")
	}
	if _, err := BaselineExtensions(ctx, pool); err == nil {
		t.Fatal("BaselineExtensions() error = nil, want query error")
	}
	if got := gatherMatviews(ctx, pool); got.Status != StatusFail {
		t.Fatalf("gatherMatviews() = %+v, want fail", got)
	}
}

func TestCheckDecryptabilityRejectsMissingStoreAndReadErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableVerifierPool(t)

	if got := checkDecryptability(ctx, pool, nil); got.Status != StatusFail {
		t.Fatalf("checkDecryptability(nil store) = %+v, want fail", got)
	}
	if got := checkDecryptability(ctx, pool, newTestStore(t)); got.Status != StatusFail {
		t.Fatalf("checkDecryptability(read error) = %+v, want fail", got)
	}
}

func newUnreachableVerifierPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
