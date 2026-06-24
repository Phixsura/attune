// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// CountedTables are the tables the row_counts check tallies on the restored
// database and compares against a live baseline. Includes the two
// secret-bearing tables so a comparison also covers managed-secret rows.
var CountedTables = []string{"tenants", "user_feedback", "inbound_sources", "llm_channels"}

const connectTimeout = 10 * time.Second

// Options configures a drill run.
type Options struct {
	BackupRef string
	// Baseline holds live per-table counts for the row_counts check; nil skips it.
	Baseline map[string]int64
	// BaselineUnvalidated is the live database's unvalidated-constraint count for
	// the constraints check; nil skips it.
	BaselineUnvalidated *int
	// BaselineExtensions is the live database's extension names for the
	// extensions check; nil skips it.
	BaselineExtensions []string
	// Deep enables the opt-in structural-integrity tier (index validity +
	// amcheck B-Tree verification).
	Deep bool
	// BackupTakenAt, when set, yields the RPO (data-loss window) = drill start −
	// backup time. RestoreDuration, when set, is the measured RTO. RPOTarget /
	// RTOTarget (0 = none) are the SLA the recovery_objectives check grades against.
	BackupTakenAt   *time.Time
	RestoreDuration *time.Duration
	RPOTarget       time.Duration
	RTOTarget       time.Duration
}

// recoveryDurations derives the RPO and RTO from the options for a drill that
// started at startedAt. Either may be nil when not supplied.
func (o Options) recoveryDurations(startedAt time.Time) (rpo, rto *time.Duration) {
	if o.BackupTakenAt != nil {
		rpo = ptrext.Of(startedAt.Sub(ptrext.Indirect(o.BackupTakenAt)))
	}
	return rpo, o.RestoreDuration
}

// Run executes the verification battery against an already-restored target
// database, decrypting sampled managed secrets with the operator's live keyset.
// It never sends production traffic and never returns decrypted plaintext.
func Run(ctx context.Context, target *pgxpool.Pool, store *secretstore.TinkStore, startedAt time.Time, opts Options) DrillReport {
	rpo, rto := opts.recoveryDurations(startedAt)
	conn := checkConnectivity(ctx, target)
	checks := []CheckResult{conn}

	schemaVersion := 0
	// RPO/RTO are only meaningful (and only reported) when the drill actually
	// connected and ran — a failed-connectivity drill verified nothing.
	var rpoSecs, rtoSecs *int64
	if conn.Status == StatusPass {
		rpoSecs, rtoSecs = secondsPtr(rpo), secondsPtr(rto)
		schemaRes, ver := gatherSchema(ctx, target)
		schemaVersion = ver
		checks = append(checks,
			schemaRes,
			checkPgvector(ctx, target),
			gatherRowCounts(ctx, target, opts.Baseline),
			checkDecryptability(ctx, target, store),
			gatherConstraints(ctx, target, opts.BaselineUnvalidated),
			gatherSequences(ctx, target),
			gatherEncoding(ctx, target),
			gatherMatviews(ctx, target),
			gatherExtensions(ctx, target, opts.BaselineExtensions),
		)
		if opts.Deep {
			checks = append(checks, checkDeep(ctx, target))
		}
		checks = append(checks, evaluateRecoveryObjectives(rpo, rto, opts.RPOTarget, opts.RTOTarget))
	}

	return DrillReport{
		Status:        Aggregate(checks),
		StartedAt:     startedAt,
		DurationMS:    time.Since(startedAt).Milliseconds(),
		BackupRef:     opts.BackupRef,
		SchemaVersion: schemaVersion,
		RPOSeconds:    rpoSecs,
		RTOSeconds:    rtoSecs,
		Checks:        checks,
		AttuneVersion: database.Version,
	}
}

func checkConnectivity(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	r := CheckResult{Name: "connectivity"}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(cctx); err != nil {
		r.Status = StatusFail
		r.Message = "cannot connect to the restored database"
		return r
	}
	r.Status = StatusPass
	r.Message = "connected to the restored database"
	return r
}

// gatherSchema reads the restored DB's migration state and classifies it,
// returning the result and the highest applied migration version.
func gatherSchema(ctx context.Context, pool *pgxpool.Pool) (CheckResult, int) {
	status, err := database.GetMigrationStatus(ctx, pool)
	if err != nil {
		return CheckResult{
			Name:    "schema",
			Status:  StatusFail,
			Message: "cannot read migration state from the restored database",
		}, 0
	}
	in := schemaInputs{
		Total:       status.Total,
		Applied:     status.Applied,
		Pending:     status.Pending,
		ChecksumErr: database.VerifyChecksums(ctx, pool),
		DirtyErr:    database.DetectDirtyMigrations(ctx, pool),
		ManifestErr: database.VerifyManifestHash(ctx, pool),
	}
	return classifySchema(in), status.Applied
}

// checkPgvector verifies the extension state and, when vector data exists,
// proves the similarity index answers a sample query.
func checkPgvector(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	r := CheckResult{Name: "pgvector"}
	if err := database.CheckPgvector(ctx, pool); err != nil {
		r.Status = StatusFail
		r.Message = "pgvector check failed on the restored database (extension missing or too old while clustering is enabled)"
		return r
	}

	var version string
	err := pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		r.Status = StatusSkip
		r.Message = "pgvector not installed (clustering disabled); nothing to verify"
		return r
	}
	if err != nil {
		r.Status = StatusFail
		r.Message = "cannot query the pgvector extension on the restored database"
		return r
	}

	ran, err := sampleKNN(ctx, pool)
	if err != nil {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("pgvector %s present but a sample similarity query failed", version)
		return r
	}
	r.Status = StatusPass
	if ran {
		r.Message = fmt.Sprintf("pgvector %s present; sample similarity query OK", version)
	} else {
		r.Message = fmt.Sprintf("pgvector %s present; no embedded rows to probe", version)
	}
	return r
}

// sampleKNN runs a nearest-neighbour query using an existing embedding as the
// probe, entirely server-side, exercising the HNSW index. ran=false means there
// were no embedded rows to probe.
func sampleKNN(ctx context.Context, pool *pgxpool.Pool) (ran bool, err error) {
	var embedded int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM user_feedback WHERE embedding IS NOT NULL`).Scan(&embedded); err != nil {
		return false, err
	}
	if embedded == 0 {
		return false, nil
	}
	var hits int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT id FROM user_feedback
			WHERE embedding IS NOT NULL
			ORDER BY embedding <=> (SELECT embedding FROM user_feedback WHERE embedding IS NOT NULL LIMIT 1)
			LIMIT 5
		) t`).Scan(&hits)
	if err != nil {
		return false, err
	}
	// The probe row is itself embedded, so a working HNSW index must return at
	// least itself; zero hits means the index answered nothing despite data.
	if hits == 0 {
		return false, fmt.Errorf("similarity query returned no neighbours despite %d embedded row(s)", embedded)
	}
	return true, nil
}

func gatherRowCounts(ctx context.Context, target *pgxpool.Pool, baseline map[string]int64) CheckResult {
	restored, err := countTables(ctx, target)
	if err != nil {
		return CheckResult{
			Name:    "row_counts",
			Status:  StatusFail,
			Message: "cannot count core tables on the restored database",
		}
	}
	return evaluateRowCounts(restored, baseline)
}

// BaselineCounts tallies CountedTables on a live pool for the row_counts
// comparison. Bounded, read-only.
func BaselineCounts(ctx context.Context, pool *pgxpool.Pool) (map[string]int64, error) {
	return countTables(ctx, pool)
}

func countTables(ctx context.Context, pool *pgxpool.Pool) (map[string]int64, error) {
	out := make(map[string]int64, len(CountedTables))
	for _, t := range CountedTables {
		var n int64
		q := "SELECT count(*) FROM " + pgx.Identifier{t}.Sanitize()
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}

// decryptDetail is the non-sensitive evidence for the decryptability check:
// population sizes and how many were decrypted. Never carries plaintext. The
// check decrypts the FULL managed-secret population (these tables are
// low-cardinality by design), so checked == total on a healthy restore.
type decryptDetail struct {
	LLMTotal       int `json:"llm_total"`
	LLMChecked     int `json:"llm_checked"`
	InboundTotal   int `json:"inbound_total"`
	InboundChecked int `json:"inbound_checked"`
	Decrypted      int `json:"decrypted"`
}

// checkDecryptability proves managed secrets in the restored DB are recoverable
// with the live keyset. It (1) runs a WHOLE-POPULATION key guard — every distinct
// llm_channels key id must be resident AND ENABLED in the live keyset — then
// (2) DECRYPTS THE FULL POPULATION of LLM credentials (AAD-bound) and inbound
// webhook/email two-level envelopes. There is no sampling: every managed secret
// is verified, so drift cannot hide in an unsampled row. A single failure fails
// loudly. It never returns or logs decrypted plaintext.
func checkDecryptability(ctx context.Context, pool *pgxpool.Pool, store *secretstore.TinkStore) CheckResult {
	r := CheckResult{Name: "decryptability"}
	if store == nil {
		r.Status = StatusFail
		r.Message = "no Tink keyset available to verify decryptability"
		return r
	}

	// (1) Whole-population key-reference guard (fast fail on obvious drift).
	referenced, err := distinctLLMKeyRefs(ctx, pool)
	if err != nil {
		r.Status = StatusFail
		r.Message = "cannot read llm_channels key references on the restored database"
		return r
	}
	if missing := missingKeyRefs(referenced, liveKeyIDs(store)); len(missing) > 0 {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d managed key id(s) referenced by llm_channels are absent or disabled in the live keyset (%s) — the restored database and keyset have drifted",
			len(missing), strings.Join(missing, ", "))
		return r
	}

	llmTotal, err := countQuery(ctx, pool, `
		SELECT count(*) FROM llm_channels
		WHERE credential_ciphertext IS NOT NULL AND octet_length(credential_ciphertext) > 0`)
	if err != nil {
		r.Status = StatusFail
		r.Message = "cannot count llm_channels on the restored database"
		return r
	}
	inboundTotal, err := countQuery(ctx, pool, `
		SELECT count(*) FROM inbound_sources
		WHERE config IS NOT NULL AND octet_length(config) > 0 AND channel IN ('webhook', 'email')`)
	if err != nil {
		r.Status = StatusFail
		r.Message = "cannot count inbound_sources on the restored database"
		return r
	}

	// (2) Decrypt the FULL population — no sampling.
	llmChecked, llmOK, llmErr, qerr := verifyDecryptAllLLM(ctx, pool, store)
	if qerr != nil {
		r.Status = StatusFail
		r.Message = "error reading llm_channels on the restored database"
		return r
	}
	inChecked, inOK, inErr, qerr := verifyDecryptAllInbound(ctx, pool, store)
	if qerr != nil {
		r.Status = StatusFail
		r.Message = "error reading inbound_sources on the restored database"
		return r
	}

	checked := llmChecked + inChecked
	decrypted := llmOK + inOK
	r.Detail = decryptDetail{
		LLMTotal: llmTotal, LLMChecked: llmChecked,
		InboundTotal: inboundTotal, InboundChecked: inChecked, Decrypted: decrypted,
	}
	firstErr := llmErr
	if firstErr == nil {
		firstErr = inErr
	}

	switch {
	case checked == 0:
		r.Status = StatusSkip
		r.Message = "no managed secrets present to verify"
	case firstErr != nil:
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d/%d managed secret(s) failed to decrypt — the restored database and the live keyset have drifted (%v)",
			checked-decrypted, checked, firstErr)
	default:
		r.Status = StatusPass
		r.Message = fmt.Sprintf("decrypted all %d managed secret(s) with the live keyset (%d llm + %d inbound)",
			checked, llmChecked, inChecked)
	}
	return r
}

// distinctLLMKeyRefs returns every distinct, non-empty credential_key_id used by
// llm_channels rows that carry ciphertext.
func distinctLLMKeyRefs(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT credential_key_id FROM llm_channels
		WHERE credential_ciphertext IS NOT NULL AND octet_length(credential_ciphertext) > 0
		  AND credential_key_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// liveKeyIDs returns the ids of keys that can actually DECRYPT — only ENABLED
// keys. Tink decrypts solely with enabled keys, so a referenced key that is
// present-but-DISABLED/DESTROYED cannot recover its ciphertext and must count as
// drift, not as resident. Filtering by status is what makes the whole-population
// guard a real safety net rather than a membership check.
func liveKeyIDs(store *secretstore.TinkStore) []string {
	keys := store.Keys()
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k.Status == "ENABLED" {
			out = append(out, k.KeyID)
		}
	}
	return out
}

func countQuery(ctx context.Context, pool *pgxpool.Pool, sql string) (int, error) {
	var n int
	if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// verifyDecryptAllLLM decrypts EVERY LLM credential. queryErr is a fatal read
// error; firstErr is the first decryption failure (drift).
func verifyDecryptAllLLM(ctx context.Context, pool *pgxpool.Pool, store *secretstore.TinkStore) (checked, decrypted int, firstErr, queryErr error) {
	rows, err := pool.Query(ctx, `
		SELECT id, credential_key_id, credential_ciphertext FROM llm_channels
		WHERE credential_ciphertext IS NOT NULL AND octet_length(credential_ciphertext) > 0`)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, keyID string
		var ct []byte
		if err := rows.Scan(&id, &keyID, &ct); err != nil {
			return checked, decrypted, firstErr, err
		}
		checked++
		if e := decryptLLMChannel(store, id, keyID, ct); e != nil {
			if firstErr == nil {
				firstErr = e
			}
		} else {
			decrypted++
		}
	}
	return checked, decrypted, firstErr, rows.Err()
}

// verifyDecryptAllInbound decrypts EVERY webhook/email two-level inbound config
// envelope.
func verifyDecryptAllInbound(ctx context.Context, pool *pgxpool.Pool, store *secretstore.TinkStore) (checked, decrypted int, firstErr, queryErr error) {
	rows, err := pool.Query(ctx, `
		SELECT channel, config FROM inbound_sources
		WHERE config IS NOT NULL AND octet_length(config) > 0 AND channel IN ('webhook', 'email')`)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var channel string
		var cfg []byte
		if err := rows.Scan(&channel, &cfg); err != nil {
			return checked, decrypted, firstErr, err
		}
		var e error
		switch channel {
		case "webhook":
			e = decryptInboundWebhook(store, cfg)
		case "email":
			e = decryptInboundEmail(store, cfg)
		default:
			continue
		}
		checked++
		if e != nil {
			if firstErr == nil {
				firstErr = e
			}
		} else {
			decrypted++
		}
	}
	return checked, decrypted, firstErr, rows.Err()
}
