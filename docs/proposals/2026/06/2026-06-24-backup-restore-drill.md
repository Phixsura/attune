# Backup and restore drill with decryptability verification

| Field | Value |
| --- | --- |
| **Issue** | [#151](https://github.com/Phixsura/attune/issues/151) |
| **Status** | Accepted |
| **Started** | 2026-06-24 |
| **Related** | [#149](https://github.com/Phixsura/attune/issues/149) (preflight — surfaces the drill), [#150](https://github.com/Phixsura/attune/issues/150) (migration checksum ledger — reused verifiers), [#66](https://github.com/Phixsura/attune/issues/66) (inbound framework — `inbound_sources` secrets), [#93](https://github.com/Phixsura/attune/issues/93) (MCP / managed LLM secrets), [2026-06-23-production-readiness-preflight.md](./2026-06-23-production-readiness-preflight.md), [2026-06-23-migration-checksum-ledger.md](./2026-06-23-migration-checksum-ledger.md) |

## Problem

`docs/private-deploy.md` already names backup/restore as a production
non-negotiable — "external PostgreSQL with backups, point-in-time recovery,
**restore drills**, and pgvector 0.5.0+" and "the Tink keyset is backed up with
the database" — but everything past "take a `pg_dump`" is left to the operator
as prose. There is no machine-checkable way to prove a backup is actually
*recoverable*, and nothing at all that proves the encrypted half of the system
survives a restore.

This is the exact failure mode the industry calls out as the #1 backup
anti-pattern. AWS Well-Architected REL09-BP04 lists verbatim: *"Restoring a
backup, but not querying or retrieving any data to check that the restoration is
usable. Assuming that a backup exists. Assuming that the backup of a system is
fully operational."* PostgreSQL's own docs warn that even `pg_verifybackup` is
insufficient — *"you should still perform test restores and verify that the
resulting databases work as expected."* ISO 27001 8.13 auditor guidance is blunter
still: *"a green dashboard showing backup completed is not evidence of
recoverability."*

attune is uniquely exposed on the encryption axis. A restored database is a pile
of bytes that *looks* fine — schema present, rows counted — yet every webhook
secret, inbound IMAP password, and managed LLM API key in it is Tink-AEAD
ciphertext (`inbound_sources.config`, `llm_channels.credential_ciphertext`).
If the operator's keyset has drifted from the backup (key retired before a
re-encrypt completed, keyset not co-restored, wrong keyset in the recovered
config), the restore is *silently* useless for everything secret-bearing, and
that only surfaces hours later when the first webhook arrives or the first
enrichment call needs a provider key.

Concretely, today:

- **Recoverability is asserted, never proven.** No command restores a backup
  somewhere safe and checks it. The "acceptance check" in the deploy docs only
  asserts `pg_dump` produced a non-empty file — the textbook anti-pattern.
- **Decryptability is never tested against real data.** The existing
  `encryption:tink_keyset` preflight check (#149) round-trips a *synthetic*
  plaintext (`"attune-preflight-test"`). It proves the keyset can encrypt and
  decrypt *something*; it never decrypts an actual persisted secret, so it
  cannot catch keyset/ciphertext drift in a restored database.
- **No audit evidence.** Nothing emits a structured, timestamped pass/fail
  report an operator can hand an ISO 27001 / SOC 2 auditor as proof a restore
  drill ran and verified data integrity.
- **No Console visibility.** The system-readiness page (#149) has no notion of
  "when did the last restore drill run and did it pass."

## Goals

- A repeatable, **tool-agnostic** drill that verifies an *already-restored*
  PostgreSQL database — regardless of how it was restored (`pg_dump`/`psql`,
  pgBackRest, a CloudNativePG recovery cluster) — without touching production or
  sending production traffic.
- The drill verifies the four things the issue names: **schema / migration
  state**, **row counts**, **pgvector extension**, and **sample Tink decryption
  of managed secrets** — and fails loudly on a missing keyset or incompatible
  schema.
- A new `attune restore-drill` CLI command that runs the battery against a
  target database URL, emits a structured JSON report suitable as audit
  evidence, and (optionally) records the run back to the production database.
- Surface the latest drill result in the Console system-readiness page via a new
  preflight check — reusing the #149 framework so it appears with zero Console
  code changes.
- A reference Kubernetes **CronJob** (Helm) that orchestrates the
  restore-to-throwaway → verify → record → tear-down loop in-cluster.
- A Prometheus metric + alert for "no successful drill in N days," and
  documentation of the drill as the recoverability acceptance gate in
  `docs/private-deploy.md` and the Helm README.
- Unit tests for every verifier (pass + fail) and an integration test that runs
  the full drill against a real restored Postgres.

## Non-goals

- **Owning backup *creation*.** attune does not become a backup engine. The
  operator keeps `pg_dump` / pgBackRest / CloudNativePG / Velero. The drill sits
  *on top* of whatever produced the restore — this is precisely the AWS pattern
  (managed restore mechanics + service-specific validation logic) and the
  CloudNativePG model (recovery always bootstraps a new cluster, so isolation is
  by construction).
- **In-place verification of the live production database.** The drill targets a
  *restored* database. Running the verifiers against production is a degenerate
  case we neither need nor want (the row-count and decryptability checks assume a
  throwaway target).
- **KMS / KEK disaster recovery.** attune's keyset lives in the runtime config
  (`secrets.tink_keyset`), not in a cloud KMS. Verifying KMS-side durability,
  cross-region KEK replication, or unwrap-availability is out of scope (it has no
  attune surface today). See Open questions.
- **Deep physical/logical corruption scanning as the default.** Full-heap
  `pg_dump` scans and `amcheck` index verification are valuable (see Prior art)
  but slow; they are an opt-in `--deep` tier, not the baseline drill.
- **Auto-remediation.** The drill reports; it does not repair a bad backup.

## Prior art

Researched the authoritative, vendor-neutral sources for automated
restore-drill + decryptability patterns (full citations in References). The
benchmark is remarkably consistent across AWS, PostgreSQL, CloudNativePG, Google
Tink/KMS, and ISO 27001 auditor practice.

| Source | Pattern adopted |
|---|---|
| **AWS Well-Architected REL09-BP04** (primary) | "A backup is not recovered until test-restored and its data queried." Automate the restore-and-validate loop on a periodic schedule. Restore into an isolated target, *without redirecting traffic*. This is the spine of the whole design. |
| **AWS Backup restore testing** (primary, GA Nov 2023) | The canonical managed pattern: *scheduled* restore into an *isolated* target → run validation → **auto-delete the target** after a retention window to avoid cost/prod impact. The split between (a) the managed restore + RTO timing and (b) **user-supplied, service-specific validation** (`PutRestoreValidationResult`) maps one-to-one onto "generic restore mechanics + attune's app-specific checks." Their validation Lambda for RDS "runs SQL with aggregate functions (SUM/AVERAGE/COUNT) compared against the original database" — our row-count check — and lists *"getting encryption key status"* as a validation activity — our decryptability check. |
| **CloudNativePG** (primary) | Recovery is *never in-place* — it bootstraps a brand-new `Cluster` from a base backup + replayed WAL, so a drill target is isolated by construction. Confirms the "restore into a fresh target, never touch the source" invariant and the five PITR target types. The strongest K8s-native restore substrate to document as a supported backend for the drill. |
| **vitabaks/pgbackrest_auto** (primary, MIT) | A concrete reference drill for self-hosted Postgres: restore to a separate directory (production untouched), then **two-tier** corruption validation — *physical* (full sequential heap read via `pg_dump`) + *logical* (`amcheck bt_index_parent_check` with `heapallindexed=true`). Justifies our optional `--deep` tier going beyond schema/row-count. Cron example runs it right after each backup and emails a report. |
| **PostgreSQL `amcheck` / `pg_verifybackup`** (primary) | `pg_verifybackup` is necessary-but-insufficient; `amcheck` catches B-Tree corruption "data checksums will fail to catch." Informs the deep tier and the honest caveat that row-count ≠ structural integrity. |
| **Google Tink + Cloud KMS** (primary) | Tink "advises against plaintext keysets — keys are often leaked when stored in plaintext"; the unencrypted path is gated behind `insecurecleartextkeyset` (which is exactly how attune loads its keyset today). "Sample decryption" is the keyset analogue of a restore drill. Drives the recommendation to keep the keyset KMS/KEK-wrappable later, and to treat decryptability as a first-class drill check now. |
| **ISO 27001 8.13 / SOC 2 backup controls** (secondary — auditor guidance) | Restore testing is mandatory and must be *documented*; "backup completed" is not evidence. Expected evidence: a backup policy, recurring restore-test **reports** capturing time-to-restore (vs RTO) and data-integrity verification, the 3-2-1 rule, and per-dataset RPO/RTO. Drives the structured JSON report + recorded history as the audit deliverable. |

Three claims surfaced in research were adversarially **refuted** and are
deliberately *not* relied on here: (1) that envelope encryption is Tink's single
"canonical" pattern (Tink supports several key-management modes — the safe,
narrower claim is "avoid plaintext, wrap with a KMS KEK"); (2) a specific
"4.5 TB recovered in two minutes" volume-snapshot benchmark (performance
superiority unconfirmed); (3) that ISO 27001 *mandates* a quarterly cadence
(quarterly is best-practice interpretation, not normative text). Cadence and
RPO/RTO targets are therefore treated as operator config, not as a number we
hard-code from a source.

## Proposal

### Architecture: three layers, reuse the bottom

The key insight from code review is that attune already owns ~70% of the
machinery. `internal/infra/database` exposes battle-tested verifiers
(`VerifyChecksums`, `VerifyManifestHash`, `GetMigrationStatus`,
`DetectDirtyMigrations`, `CheckPgvector`); `internal/preflight` (#149) is a
registry-driven readiness framework that already renders itself in the Console
with no per-check UI work. The drill is **not a new parallel stack** — it
composes these.

```
┌─ Layer 3 ── Readiness surface (light, reads last result) ───────────────┐
│  internal/preflight/checks/backup.go   "backup:restore_drill"           │
│    └─ SELECT last run FROM restore_drill_runs → pass/warn/fail          │
│       (auto-renders in Console #149; no Console code change)            │
├─ Layer 2 ── Orchestration + CLI + report (the drill itself) ───────────┤
│  cmd/attune/restore_drill.go           "attune restore-drill run|status"│
│  internal/restoredrill/                Verifier, DrillReport, runners    │
│    └─ runs the battery against a TARGET (restored) pool, emits report    │
├─ Layer 1 ── Verifiers (already exist, reused as-is) ───────────────────┤
│  internal/infra/database  VerifyChecksums / VerifyManifestHash /        │
│                           GetMigrationStatus / DetectDirtyMigrations /  │
│                           CheckPgvector                                  │
│  internal/infra/secretstore  TinkStore.DecryptValue (sample decrypt)    │
└─────────────────────────────────────────────────────────────────────────┘
```

The clean boundary — confirmed by the AWS pattern — is **the drill (heavy,
active, runs against a throwaway) is separate from the readiness surface (light,
reads the last recorded result).** Layer 2 *produces* a result; Layer 3
*consumes* it. They never share a process: the drill runs in a CronJob against an
ephemeral DB; the readiness check runs in the live server against production.

### Layer 2 — the drill: `internal/restoredrill`

A `Verifier` runs an ordered battery of `Check`s against a **target pool** (the
restored database) and returns a `DrillReport`. Deliberately mirrors the
`preflight` shapes for consistency, but is its own package because (a) it targets
an arbitrary restored pool, not the server's `Environment`, and (b) it needs the
live Tink keyset to decrypt the target's ciphertext.

```go
// internal/restoredrill/report.go
package restoredrill

type Status string

const (
    StatusPass Status = "pass"
    StatusFail Status = "fail"
    StatusSkip Status = "skipped"
)

type CheckResult struct {
    Name    string `json:"name"`     // "schema", "row_counts", "pgvector", "decryptability"
    Status  Status `json:"status"`
    Message string `json:"message"`
    Detail  any    `json:"detail,omitempty"` // e.g. per-table counts, sample count
}

// DrillReport is the audit-evidence artifact. Serialized to JSON by the CLI and
// persisted (summary) to restore_drill_runs.
type DrillReport struct {
    Status         Status        `json:"status"`
    StartedAt      time.Time     `json:"started_at"`
    DurationMS     int64         `json:"duration_ms"`     // restore-verify time (RTO signal)
    BackupRef      string        `json:"backup_ref"`      // operator-supplied label (e.g. backup filename / snapshot id)
    SchemaVersion  int           `json:"schema_version"`  // highest applied migration in target
    Checks         []CheckResult `json:"checks"`
    AttuneVersion  string        `json:"attune_version"`
}
```

The battery (each maps to an issue acceptance criterion):

| Check | What it does | Reuses | New? |
|---|---|---|---|
| `connectivity` | `pool.Ping` the restored DB within a timeout | `database.NewPool` | — |
| `schema` | No pending/dirty migrations; checksums + manifest hash verify against the embedded set | `GetMigrationStatus`, `VerifyChecksums`, `VerifyManifestHash`, `DetectDirtyMigrations` | — |
| `pgvector` | Extension present + ≥ 0.5.0; (optional) a sample KNN query against an existing HNSW/ivfflat index proves the vector index is queryable | `CheckPgvector` | partial |
| `row_counts` | Per-table counts present and within an RPO band vs. a baseline | — | **yes** |
| `decryptability` | Sample-decrypt real ciphertext from `llm_channels.credential_ciphertext` and `inbound_sources.config` with the **live** keyset, reconstructing per-row AAD | `TinkStore.DecryptValue` | **yes** |

**`decryptability` is the heart of #151** and the genuinely new security value.
It closes the gap that the synthetic-plaintext preflight check cannot:

- Sample up to N rows (default 3, configurable; bounded to avoid I/O spikes on
  large tables) from each secret-bearing table in the *restored* DB.
- For `llm_channels`, reconstruct the AAD the app uses on write —
  `secretstore.AssociatedData("llm_channel", row.ID.String(), "api_key")` — and
  call `TinkStore.DecryptValue`. For `inbound_sources.config`, decrypt with the
  nil-AAD path the inbound `SecretStore` uses.
- Cross-check every referenced `credential_key_id` against the live keyset's key
  IDs *before* decrypting, so a missing key fails with a precise message
  ("ciphertext references key_id 0x… absent from the configured keyset") rather
  than an opaque AEAD error. (`repo/llmconfig.ValidateSecretKeyReferences`
  already does exactly this reference check — see Risks for the layering call on
  whether to reuse it or query directly.)
- A single undecryptable sample → `decryptability: fail` → drill fails. This is
  the "fails loudly" half of the acceptance criterion; the missing-keyset half
  is already enforced at config load (`config.Load` rejects an empty/invalid
  `secrets.tink_keyset` at boot).

**`row_counts` baseline.** "Counts match" needs something to match against. A
backup is by definition *older* than live, so restored counts are *expected* to
be lower — the delta is the RPO. The drill captures a lightweight count manifest
from a baseline source at drill time and asserts the restored counts are present
and within a sane band:

- Default: a **floor** check — the core tables (`user_feedback`, `tenant`,
  `api_keys`, …) are non-empty and monotonic relative to a stored prior run.
- Optional `--baseline-url`: connect read-only to the live DB, capture
  `COUNT(*)` per core table, and assert `0 < restored ≤ live` within a
  configurable tolerance. This is the AWS "aggregate SQL vs original database"
  pattern. The exact RPO band is operator config (see Open questions).

### Layer 2 — CLI: `attune restore-drill`

Follows the established `migrations` sub-dispatch pattern verbatim
(stdlib `flag`, `init()`-registered into `subcommands`, per-verb `FlagSet`):

```
attune restore-drill run    --target <url> [--baseline-url <url>] [--backup-ref <s>]
                            [--record] [--deep] [--format text|json]
attune restore-drill status [--format text|json]   # read last recorded run (audit retrieval)
```

- `run` opens a pool to `--target` (the restored throwaway DB), runs the
  battery, prints the report (text table or `--format=json`), and exits non-zero
  on `fail`. `--record` additionally writes the summary to the **production**
  `restore_drill_runs` table (so Layer 3 can surface it). `--deep` adds the
  physical (`pg_dump` full read) + logical (`amcheck`) tier.
- `status` reads the last recorded run from production and prints it — the audit
  retrieval path, mirroring `attune migrations status`.

```
$ attune restore-drill run --target postgres://…/attune_restore_test --record

attune restore drill — recoverability report

  ✓ connectivity     Connected (6ms)
  ✓ schema           24/24 migrations applied, checksums + manifest verified
  ✓ pgvector         pgvector 0.7.0, sample HNSW query returned 5 rows
  ✓ row_counts       6 core tables present, within RPO band of baseline
  ✓ decryptability   3/3 llm_channels + 2/2 inbound_sources samples decrypted

Overall: PASS  (restore+verify 4.2s)  recorded as run #37
```

```
  ✗ decryptability   1/3 llm_channels samples FAILED
    → ciphertext for llm_channels.id=… references key_id 0x1a2b3c4d which is
      absent from the configured Tink keyset. The restored database and the
      operator's keyset have drifted. Restore the keyset that was active when
      this row was written, or re-run the key rotation re-encrypt.
```

### Layer 2 — result persistence: migration `072_restore_drill_runs.sql`

```sql
CREATE TABLE restore_drill_runs (
    id             BIGSERIAL PRIMARY KEY,
    ran_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    status         TEXT NOT NULL,             -- pass | fail
    backup_ref     TEXT,
    schema_version INTEGER,
    duration_ms    BIGINT,
    report         JSONB NOT NULL,            -- full DrillReport for audit
    attune_version TEXT
);
CREATE INDEX idx_restore_drill_runs_ran_at ON restore_drill_runs (ran_at DESC);
```

The drill runs *against the throwaway* but records its summary *to production*
(via `--record`, a separate connection to the live DB) — the same control-plane
split as AWS `PutRestoreValidationResult`. The `report` JSONB is the
auditor-facing artifact; the table is the drill history.

### Layer 3 — readiness surface: `backup:restore_drill` preflight check

A thin check added to the #149 framework. It does **not** run a drill — it reads
the last recorded run and grades recency:

```go
// internal/preflight/checks/backup.go
func init() {
    preflight.Register(preflight.Check{
        Name:     "backup:restore_drill",
        Category: preflight.CategoryBackup, // new category
        Run:      checkRestoreDrill,
    })
}
```

- `pass` — last run was `pass` and within the freshness window (default 7 days).
- `warn` — last run passed but is stale (older than the window), or the table is
  empty (drills configured but none recorded yet).
- `fail` — last recorded run was `fail`.
- `skipped` — restore-drill not configured (greenfield installs).

Per the #149 SOP, a new `CategoryBackup` means updating four spots
(`Category` constant, `format.go` `categoryTitle()`, Console `CATEGORY_ORDER`,
and the `system_readiness.category` i18n key) — but the check then renders in the
Console readiness page automatically. This satisfies "surface the latest drill
status in Console/system readiness" with no new endpoint or page.

### Layer 3 — observability

Per the metrics conventions and the metric-drift gate (registration + catalog +
dashboard + README, no `_count` suffix):

- `attune_restore_drill_runs_total{status}` — counter.
- `attune_restore_drill_last_success_timestamp_seconds` — gauge (drives the
  alert + the readiness freshness grade).
- `attune_restore_drill_duration_seconds` — gauge (RTO signal).

An alert `AttuneRestoreDrillStale` fires when
`time() - attune_restore_drill_last_success_timestamp_seconds` exceeds the
freshness window — wired into the existing `attune-alerts.yml` that the recent
checksum-ledger work already extended.

### Kubernetes: reference drill CronJob (Helm)

The Helm chart has **no** Job/CronJob templates today (only Deployment +
StatefulSet + a `helm test` smoke Pod). Add an *opt-in* drill, off by default:

```
deploy/helm/attune/templates/
  drill-cronjob.yaml        # schedule → ephemeral pg → restore → attune restore-drill run --record → teardown
  drill-rbac.yaml           # ServiceAccount + minimal Role/RoleBinding (own namespace)
values.yaml:
  restoreDrill:
    enabled: false
    schedule: "0 2 * * 0"       # weekly; operator-tuned
    freshnessWindowDays: 7
    backup:
      source: { type: pvc|s3|configmap, ref: "" }   # where the backup to restore lives
    deep: false
```

The CronJob reuses existing chart machinery confirmed present: the
`attune.configPath` / `attune.databaseURL` / `attune.image` helpers, the config
Secret mount, and the hardened pod security context. It spins an ephemeral
`pgvector/pgvector:pg17` (matching the embedded Postgres image), restores the
operator's backup into it, runs `attune restore-drill run --target <ephemeral>
--record`, then the pod exits and the ephemeral DB is discarded.
Operators on CloudNativePG instead point the drill at a recovery `Cluster`
(isolated by construction) and skip the ephemeral-pg container — documented as
the alternative backend.

### Package layout

```
internal/restoredrill/
  report.go         # Status, CheckResult, DrillReport
  verifier.go       # Verifier, RunAll(ctx, targetPool, opts) DrillReport
  checks.go         # connectivity, schema, pgvector, row_counts, decryptability
  deep.go           # --deep tier: pg_dump heap scan + amcheck (opt-in)
  store.go          # record run → restore_drill_runs; read last run
cmd/attune/
  restore_drill.go  # attune restore-drill run|status (init()-registered)
internal/preflight/checks/
  backup.go         # backup:restore_drill readiness check (reads last run)
internal/infra/database/migrations/
  072_restore_drill_runs.sql
internal/infra/metrics/
  metrics.go        # 3 new drill metrics
deploy/helm/attune/templates/
  drill-cronjob.yaml, drill-rbac.yaml
```

Layering: `internal/restoredrill` imports `infra/database`, `infra/secretstore`,
`infra/config` — the same infra-only dependency set as `internal/preflight`. It
sits beside `preflight` as a top-level operational package, not inside the
`handlers → service → repo` chain.

## Alternatives considered

### A. Extend the existing `encryption:tink_keyset` preflight check to sample real secrets

Rejected as the *primary* mechanism, kept as a complement. The preflight check
runs in the live server against production on every readiness poll — sampling and
decrypting real production secrets on a hot, frequently-hit path is the wrong
place, and it still wouldn't prove *recoverability* (it never restores
anything). The drill is the right home for real-secret decryption because it runs
occasionally, against a throwaway. We do, however, reuse the *idea*: the drill's
`decryptability` check is the real-data version the preflight check can't be.

### B. Make the drill a mode of `attune doctor`

Rejected. `doctor` (#149) is explicitly a *read-only, point-in-time, against-the-
running-system* diagnostic that an operator runs interactively. The drill is a
heavyweight, state-creating operation against a *different* (restored) database,
producing a persisted audit record. Overloading `doctor` would blur the "doctor
is safe to run anytime" contract. Separate command, shared verifier vocabulary.

### C. attune owns backup creation (wrap pgBackRest / shell out to `pg_dump`)

Rejected. Research is decisive that the durable division of labor is "generic,
mature restore mechanics (pgBackRest/CloudNativePG/AWS Backup) + thin,
service-specific validation." attune's value is the validation no external tool
can provide — that *its* migrations, *its* pgvector usage, and *its* Tink
ciphertext all survived. Re-implementing backup orchestration would be a large,
lower-value surface competing with far more mature tools, and would couple attune
to one backup topology. The drill stays backup-tool-agnostic: give it a restored
DB URL, it verifies it.

### D. Surface drill status only via Prometheus/Grafana, not the Console

Rejected as the *sole* surface (kept as a complement). The issue explicitly asks
for Console/system-readiness visibility, and many self-hosted operators will not
have Grafana wired. The `restore_drill_runs` table + the readiness check give a
zero-dependency surface; the metric + alert are the additional, ops-grade
surface for those who have Prometheus.

### E. Verify by replaying production traffic against the restored DB

Rejected — directly violates the "no production traffic" acceptance criterion and
the universal isolation principle. The drill is strictly read-only
introspection + sample decryption against an isolated target.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| Decrypting real secrets during a drill could log/leak plaintext | Decrypted plaintext is never logged or placed in the report — only a boolean per sample and a count. Follows the existing "hash/truncate at call site" rule; the report carries `3/3 decrypted`, never the values. |
| `decryptability` needs the AAD convention, which lives in app code — duplicating it risks drift | Prefer reusing `repo/llmconfig` helpers (`ValidateSecretKeyReferences`, the secret listing) and `secretstore.AssociatedData` rather than re-deriving AAD. Open design call: import `repo` (couples `restoredrill` to the repo layer) vs. query ciphertext directly like `preflight` does (self-contained, but re-encodes the table/column/AAD knowledge). Leaning self-contained for layering symmetry with `preflight`, with the AAD constructor shared from `secretstore`. Resolve before PR 1. |
| Row-count "match" is ambiguous for an older backup | Treat counts as an RPO-band assertion (`0 < restored ≤ live`, monotonic vs. prior run), not strict equality. The band is operator config; default is a non-empty floor so the check is meaningful with zero configuration. |
| `--deep` (`pg_dump` full scan + `amcheck`) is slow and resource-heavy | Off by default; opt-in only. Document expected runtime scales with DB size. Baseline drill stays fast (metadata + sampled rows). |
| pgvector index integrity has no external verification precedent | Research open question: `amcheck` covers B-Tree, not HNSW/ivfflat. Start with extension presence/version (existing `CheckPgvector`) + an optional sample KNN query proving the index answers; flag deeper vector-index validation as future, not blocking. |
| New `CategoryBackup` touches 4 files across Go + Console + i18n | The #149 SOP enumerates exactly these four spots; the proposal's checklist carries them. |
| The drill records to production from a CronJob — a write path from an ops job | The only write is an append to `restore_drill_runs` via `--record`; it is idempotent-safe (append-only history) and gated behind the flag. The verification itself is strictly read-only against the throwaway. |
| Migration `072` adds a table to every deploy even if drills are unused | Tiny, append-only, no hot-path impact; mirrors how `schema_migrations_manifest` (071) is always present. Greenfield installs simply have an empty table → readiness check reports `skipped`. |

## Implementation plan

### PR 1 — verifier library + CLI (`internal/restoredrill` + `attune restore-drill`)

1. `internal/restoredrill/`: `DrillReport` types, `Verifier`, the baseline
   battery (`connectivity`, `schema`, `pgvector`, `row_counts`,
   `decryptability`) composing the existing `database.*` + `secretstore`
   primitives.
2. Migration `072_restore_drill_runs.sql` + `store.go` (record / read-last).
3. `cmd/attune/restore_drill.go` with `run` / `status`, text + JSON output,
   `--target` / `--baseline-url` / `--record` / `--backup-ref` / `--deep`.
4. Resolve the AAD-reuse vs. direct-query layering call (Risks row 2).
5. Unit tests per verifier (pass + fail), table-driven, using injected
   interfaces / fixtures — no real DB for unit tests (per the coverage-gate
   convention).
6. CHANGELOG `### Added`.

### PR 2 — readiness check + observability

1. `internal/preflight/checks/backup.go` (`backup:restore_drill`) + new
   `CategoryBackup` wiring (constant, `format.go`, Console `CATEGORY_ORDER`,
   i18n key).
2. Three drill metrics + catalog + dashboard panel + alert
   (`AttuneRestoreDrillStale`) + README metric docs (metric-drift gate).
3. Unit tests for the check (pass/warn/fail/skipped) against a seeded
   `restore_drill_runs`.

### PR 3 — Kubernetes CronJob + docs

1. `drill-cronjob.yaml` + `drill-rbac.yaml` + `values.yaml` `restoreDrill`
   block (off by default), with `helm template` / `config -q` smoke coverage.
2. `docs/private-deploy.md`: a "Restore drills" runbook — the `pg_dump`/ephemeral
   path and the CloudNativePG recovery-cluster path — plus the keyset-co-restore
   reminder and a sample audit report. Update the acceptance-check section to
   call `attune restore-drill run` instead of the bare `pg_dump` smoke.
3. Helm README: the `restoreDrill` values + the CronJob.

### PR 4 — integration test

`make test-integration`: take a `pg_dump` of a migrated, secret-seeded fixture
DB, restore into a second database, run the full drill, assert `pass`; then
mutate (drop a row / swap the keyset) and assert the corresponding check fails
loudly. This is the real-restore proof the issue's acceptance criteria require.

## Verification

- `go vet ./...` + `golangci-lint` clean; `lizard` CCN/NLOC within gates;
  `jscpd` under threshold.
- Unit tests: each verifier's pass + fail path; the readiness check's four
  statuses; CLI flag parsing + JSON shape.
- Integration test (PR 4): full dump → restore → drill → assert pass, then
  fault-inject (missing row, drifted keyset) → assert the specific check fails.
- `make proto` n/a (no contract change — the readiness check rides the existing
  `/system/preflight` JSON).
- Real end-to-end: stand up the Helm `restoreDrill` CronJob in a kind cluster,
  let it restore a seeded backup into an ephemeral pg, confirm a `pass` row lands
  in `restore_drill_runs`, the metric updates, and the Console readiness page
  shows `backup:restore_drill` green. Fault-inject a drifted keyset and confirm
  the page + alert go red. (Per the real-LLM-e2e bar: acceptance includes this
  live run, not just unit tests.)
- Cite `make ci-check` (or the relevant subset) output in the PR, not an
  assertion that it is green.

## Implementation status (as built)

Landed: the verifier library (`internal/restoredrill`), migration
`072_restore_drill_runs.sql`, the `attune restore-drill run|status` CLI, the
`backup:restore_drill` preflight check under a new `backup` category (surfacing
in `attune doctor` and the Console System Readiness page), and a real-Postgres
integration test (seeded restore passes; keyset drift fails loudly).

Code-verified refinements vs. the design above:

- **Decryptability is two-level for inbound sources.** `inbound_sources.config`
  is a Tink envelope whose plaintext JSON itself carries further Tink
  ciphertexts: webhook → `secret_current_encrypted` (+ optional
  `secret_previous_encrypted`); email → `password_encrypted`. The drill decrypts
  both levels. Only the **webhook** and **email** adapters persist encrypted
  config today; MCP OAuth stores a SHA-256 `token_hash` (not reversible) and is
  out of scope for decryptability. LLM credentials are single-level, AAD-bound
  with `AssociatedData("llm_channel", id, "api_key")`.
- **Schema check is version-skew aware.** An older backup (binary embeds more
  migrations than the backup carries) yields pending migrations and a manifest
  mismatch by construction — this is **recoverable** (they apply on boot), so it
  is a `warn`, not a `fail`. A newer backup (an applied migration whose file the
  binary lacks → `ErrMissingFile`), checksum drift, a dirty migration, or a
  reorder at the same version are hard `fail`s.
- **Row counts require a live baseline** to be meaningful (an empty table is not
  corruption on its own): data loss (live has rows, restore is empty) → `fail`;
  restore exceeding live → `warn`; no baseline → `skipped`.
- **Whole-population key validation + bounded deep sampling.** Decryptability
  does two things: a cheap whole-population guard (every distinct
  `llm_channels.credential_key_id` must be resident in the live keyset — catching
  drift even in rows the sampler never touches) and a deep sample that actually
  decrypts a bounded set of LLM credentials and two-level inbound envelopes. The
  report states sampled-of-total counts; coverage is never silently truncated.
- **Opt-in `--deep` tier.** Beyond the baseline, `--deep` asserts no index is
  `indisvalid = false` and runs amcheck `bt_index_parent_check(heapallindexed)`
  over B-Tree indexes (research's structural-integrity tier), skipping cleanly
  when amcheck is absent.
- **Metrics derived server-side (not deferred).** The drill runs in a
  short-lived CronJob the scraper never sees, so the long-lived server exposes
  `attune_restore_drill_last_success_timestamp_seconds` and
  `attune_restore_drill_runs_total{status}` *derived from `restore_drill_runs`
  at scrape time*. An `AttuneRestoreDrillStale` alert (guarded so a never-run
  drill does not page) + runbook + README row accompany them. A bespoke Grafana
  panel is the only remaining follow-up; the alert + Console readiness page cover
  operational visibility.
- **CLI parity:** `--warn-exit` (matching `attune doctor`), structured `logext`
  run logging, and a richer text report (schema version, duration, backup ref).
- **Recovery objectives (RPO/RTO) + trend.** The report quantifies the two DR
  numbers that matter for audit: RPO (data-loss window, from `--backup-taken-at`)
  and RTO (restore time, from `--restore-duration`), graded against
  `--rpo-target` / `--rto-target` by a `recovery_objectives` check (a breach
  warns — the data IS recoverable, just outside SLA). Both persist as
  `rpo_seconds`/`rto_seconds`; `attune restore-drill history` shows the trend and
  `attune_restore_drill_last_rto_seconds` exposes RTO to Prometheus. This closes
  the gap to DR products that lead with RPO/RTO-vs-SLA reporting.

## Coverage (as built)

The drill battery comprehensively covers silent restore-failure modes — 12
checks: `connectivity`, `schema` (version-skew aware), `pgvector` (+ sample KNN),
`row_counts` (baseline-relative), `decryptability` (whole-population key
validation + deep sample), `constraints` (baseline-relative NOT VALID),
`sequences` (serial/identity behind column max), `encoding` (UTF8),
`materialized_views` (populated), `extensions` (baseline-relative),
`recovery_objectives` (RPO/RTO vs SLA), and the opt-in `deep` tier (index
validity + amcheck). Test coverage of `internal/restoredrill` is ~86% (unit +
real-Postgres integration, including error-branch and CLI-parsing tests).

Now built and verified: the drill *performs* the restore (`--restore-from`:
ephemeral-provision → `psql`/`pg_restore` → auto-measured RTO → battery →
teardown), and pre-restore artifact verification (`verify-backup` via
`pg_verifybackup`). Both are exercised by real-tool integration tests.

Genuinely separate efforts (deliberately out of this PR): PITR target
verification (needs a WAL-archive + recovery-target harness); KMS/KEK-wrapped
keyset backup (a `secretstore` architecture change — open question 4); and
failed-drill notification through attune's *feedback* notify pipeline (declined —
it would abuse the frozen, append-only feedback source vocabulary; operational
alerting is already delivered via the `AttuneRestoreDrillStale` Prometheus alert,
the Console readiness check, and the `attune_restore_drill_*` metrics).

## Open questions

These need an operator/product decision rather than an external benchmark
(research explicitly left them as design calls):

1. **RPO/RTO targets and drill cadence per data class** (`user_feedback`,
   `notify_outbox`, the Tink keyset). The freshness window (default 7 days) and
   weekly schedule are placeholders — what are attune's stated targets?
2. **Deep tier in v1.0 or later?** Is the `--deep` (`pg_dump` + `amcheck`) tier
   in scope for the issue, or is the baseline battery sufficient for v1.0 with
   deep as a fast follow?
3. **pgvector index integrity depth.** Is "extension present + sample KNN
   query" enough, or do we want bespoke HNSW/ivfflat structural validation (no
   external precedent to copy)?
4. **Keyset backup story.** The keyset is cleartext-in-config today
   (`insecurecleartextkeyset`). Do we, in or alongside #151, document/enforce
   KMS/KEK-wrapped keyset backup (the Tink-recommended posture), or is that a
   separate hardening issue?

## References

Primary / authoritative (the benchmark ground):

- AWS Well-Architected, Reliability Pillar REL09-BP04 (periodic recovery
  testing): https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/rel_backing_up_data_periodic_recovery_testing_data.html
- AWS Backup — automatic restore testing & validation (announcement):
  https://aws.amazon.com/blogs/aws/automatic-restore-testing-and-validation-is-now-available-in-aws-backup/
- AWS Backup — implementing restore testing for recovery validation:
  https://aws.amazon.com/blogs/storage/implementing-restore-testing-for-recovery-validation-using-aws-backup/
- AWS Backup — `PutRestoreValidationResult` API:
  https://docs.aws.amazon.com/aws-backup/latest/APIReference/API_PutRestoreValidationResult.html
- CloudNativePG — Recovery (new-cluster bootstrap, PITR targets):
  https://cloudnative-pg.io/documentation/current/recovery/
- CloudNativePG — Backup (object store vs. volume snapshot topology):
  https://cloudnative-pg.io/documentation/1.21/backup/
- vitabaks/pgbackrest_auto (two-tier physical + logical validation drill):
  https://github.com/vitabaks/pgbackrest_auto
- PostgreSQL — `amcheck`: https://www.postgresql.org/docs/current/amcheck.html
- PostgreSQL — `pg_verifybackup` (necessary but insufficient):
  https://www.postgresql.org/docs/current/app-pgverifybackup.html
- Google Tink — key management overview (plaintext-keyset warning):
  https://developers.google.com/tink/key-management-overview
- Google Cloud KMS — envelope encryption:
  https://cloud.google.com/kms/docs/envelope-encryption
- Percona — enterprise PostgreSQL backup strategy:
  https://www.percona.com/blog/postgresql-backup-strategy-enterprise-grade-environment/

Secondary (auditor/GRC guidance — cite as expectation, not normative text):

- UpGuard — ISO 27001 Annex A 8.13 (restore testing as evidence):
  https://www.upguard.com/compliance/iso-27001/8-13
