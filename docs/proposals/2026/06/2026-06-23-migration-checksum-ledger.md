# Migration checksum ledger, dry-run, and duplicate-prefix guard

| Field | Value |
| --- | --- |
| **Issue** | [#150](https://github.com/Phixsura/attune/issues/150) |
| **Status** | Proposed |
| **Started** | 2026-06-23 |
| **Related** | [#149](https://github.com/Phixsura/attune/issues/149) (preflight system), [2026-06-23-production-readiness-preflight.md](./2026-06-23-production-readiness-preflight.md) |

## Problem

Migrations are embedded and applied in lexicographic order, but the tracker
(`schema_migrations_feedback`) primarily records version order. Several issues
undermine v1.0 upgrade confidence:

### P1. Duplicate numeric prefixes (critical)

Recent parallel development created 10 migrations with repeated prefixes
058–062:

| Prefix | File A (enrich/discord track) | File B (MCP track) |
|--------|-------------------------------|-------------------|
| 058 | `058_discord_dest_type.sql` | `058_mcp_oauth.sql` |
| 059 | `059_enrich_prompt_versions.sql` | `059_mcp_audit_actions.sql` |
| 060 | `060_enrich_prompt_activate_audit_action.sql` | `060_mcp_refresh_token_session.sql` |
| 061 | `061_enrich_prompt_versions_repair.sql` | `061_mcp_sessions_fk_index.sql` |
| 062 | `062_enrich_prompt_policy_config.sql` | `062_mcp_codes_hash.sql` |

Current lexicographic sort produces (69 files total):
```
...
057_eval_promote_audit_action.sql   → version 57
058_discord_dest_type.sql           → version 58
058_mcp_oauth.sql                   → version 59  ← filename prefix != version
059_enrich_prompt_versions.sql      → version 60
059_mcp_audit_actions.sql           → version 61
...
064_retry_enrichment_audit_action.sql → version 69
```

**Impact:** The tracker's `version` column no longer matches the filename
prefix. A database migrated by binary A may have different version→filename
mapping than binary B if files were added in different order. This is a **silent
correctness bug** that breaks upgrade assumptions.

**Current state:** No production deployments exist yet (pre-v1.0), so we can
renumber without upgrade migration.

### P2. No checksum tracking

If an already-applied migration file is modified (accidentally or maliciously),
attune cannot detect the drift. The database state diverges silently from the
embedded migrations.

**Industry context:** Only 4 of 15 surveyed tools track checksums (Flyway,
Liquibase, Prisma, Atlas). Those 4 are all enterprise-grade; checksum tracking
is a differentiator for production-critical systems.

### P3. No operator inspection tools

Operators cannot:
- See which migrations are pending without applying them.
- Verify that the embedded migrations match what was applied.
- Perform a dry-run to preview SQL before a production deploy.

### P4. Missing metadata

The tracker lacks:
- `duration_ms` — how long each migration took (useful for diagnosing slow
  deploys)
- `applied_by` — which binary version applied the migration (audit trail)
- `success` — whether migration completed (dirty state detection)

These are useful for post-incident diagnosis and compliance audits (SOC2 CC8.1
requires who/what/when/where; PCI-DSS requires tamper-evident audit trails).

## Goals

1. **Fix existing duplicates.** Renumber migrations 058–064 to eliminate
   collisions before any production deployment.

2. **Extend tracker table.** Add `checksum`, `duration_ms`, `applied_by`, and
   `success` columns.

3. **Fail on drift.** Startup fails with clear message on checksum mismatch or
   duplicate numeric prefixes.

4. **CLI commands.** Add `attune migrations status`, `verify`, and `dry-run`.

5. **CI lint.** Add `scripts/lint-migrations.sh` for naming and no-transaction
   directive validation.

6. **Preflight integration.** Extend `migration:pending` check to include
   checksum verification and duplicate detection.

7. **Documentation.** Add migration management section to private-deploy guide
   with recovery procedures.

8. **Test coverage.** Fresh DB, upgraded DB, drift injection, duplicate
   injection, no-tx behavior.

## Non-goals

- **Down migrations / rollback.** Industry consensus (GitLab, Stripe, Netflix)
  favors fix-forward; down migrations are error-prone and rarely tested. We
  document expand-contract as the safe rollback pattern.

- **Shadow database drift detection** (Prisma pattern). Adds operational
  complexity; checksum comparison is sufficient for our scale.

- **Per-tenant migration orchestration.** attune uses shared-schema multi-
  tenancy; a single migration run affects all tenants.

- **GUI migration management** (Bytebase pattern). Out of scope for CLI-first
  v1.0.

- **Automatic repair.** `attune migrations repair` is not implemented in v1.
  Operators must manually update checksums if drift is intentional.

- **Console UI for migrations.** The existing system-readiness page shows
  preflight results including migration status; no separate migration UI.

## Prior art

Surveyed 25+ migration tools across Go, Rails, Django, Node.js, and enterprise
ecosystems. Full research notes in session transcript.

### Checksum tracking comparison

| Tool | Checksum | Algorithm | Storage | On drift |
|------|:--------:|-----------|---------|----------|
| **Flyway** | ✅ | CRC32 | DB `checksum` column | Validation fails → `repair` |
| **Liquibase** | ✅ | MD5 (`9:hash` format) | DB `MD5SUM` column | ValidationFailed → `clear-checksums` |
| **Prisma** | ✅ | SHA-256 | DB `checksum` column | Fails → `migrate resolve` |
| **Atlas** | ✅ | SHA-256 Merkle tree | `atlas.sum` file | Fails, detects reorder/insert |
| golang-migrate | ❌ | — | — | `dirty` flag only |
| goose | ❌ | — | — | panic on duplicate version |
| Rails | ❌ | — | — | No drift detection |
| Django | ❌ | — | — | Dependency graph only |
| Grafana | ❌ | — | Stores SQL text | No drift detection |
| Mattermost | ❌ | — | — | `morph:nontransactional` directive |

**Decision:** Adopt SHA-256 (Prisma/Atlas pattern) for stronger integrity than
CRC32. Store in DB tracker table (simpler than file-based `atlas.sum`).

### Tracker table fields comparison

**Flyway `flyway_schema_history`** (most complete):
```sql
installed_rank  INT PRIMARY KEY,
version         VARCHAR(50),
description     VARCHAR(200) NOT NULL,
type            VARCHAR(20) NOT NULL,     -- SQL, JDBC, etc.
script          VARCHAR(1000) NOT NULL,   -- filename
checksum        INT,                      -- CRC32
installed_by    VARCHAR(100) NOT NULL,    -- DB user
installed_on    TIMESTAMP NOT NULL,
execution_time  INT NOT NULL,             -- milliseconds
success         BOOLEAN NOT NULL
```

**Prisma `_prisma_migrations`** (modern design):
```sql
id                  VARCHAR(36) PRIMARY KEY,
checksum            VARCHAR(64) NOT NULL,  -- SHA-256
migration_name      VARCHAR(255) NOT NULL,
started_at          TIMESTAMPTZ NOT NULL,
finished_at         TIMESTAMPTZ,           -- NULL = in-progress/failed
applied_steps_count INT NOT NULL,
rolled_back_at      TIMESTAMPTZ,
logs                TEXT
```

**Decision:** Hybrid approach:
- Keep existing: `version`, `filename`, `applied_at`
- Add Flyway-style: `duration_ms`, `success`
- Add Prisma-style: `checksum` (SHA-256)
- Add attune-specific: `applied_by` (binary version, not DB user)

### Duplicate detection comparison

| Tool | Detection timing | Behavior |
|------|------------------|----------|
| golang-migrate | Source load | `ErrDuplicateMigration` |
| goose | Sort (Less()) | **panic** with both filenames |
| Flyway | validate/migrate | Validation fails |
| Atlas | `atlas.sum` integrity | Merkle tree detects insert |

**Decision:** Detect at startup before any apply; return error (not panic) with
clear message listing all duplicates.

### CLI command comparison

| Function | Flyway | Prisma | Atlas | Django | goose |
|----------|--------|--------|-------|--------|-------|
| Status | `info` | `migrate status` | `schema inspect` | `showmigrations` | `status` |
| Dry-run | `check -dryrun` ⚡ | `migrate diff` | `schema diff` | `sqlmigrate` | ❌ |
| Verify | `validate` | — | `migrate lint` | `--check` | `validate` |
| Repair | `repair` | `migrate resolve` | — | — | `force` |

**Decision:** Adopt `attune migrations {status,verify,dry-run}`. Reserve
`repair` for potential future implementation.

### Compliance requirements

| Framework | Key requirements | Relevance |
|-----------|------------------|-----------|
| **SOC2 CC8.1** | who/what/when/where audit trail; approval workflows | `applied_by` + `applied_at` + `checksum` |
| **PCI-DSS 4.0** | Tamper-evident logs; 12-month retention | Checksum verification |
| **HIPAA** | Immutable audit trails; 6-year retention | Append-only tracker |

**Decision:** `applied_by` (binary version) + `applied_at` + `checksum` provide
audit-grade metadata. True who/approval belongs in PR/issue workflow, not
migration tooling.

## Proposal

### Phase 0: Fix existing duplicate prefixes

**Before any other work**, renumber the 10 conflicting migrations. Since no
production deployments exist, this is a one-time source change with no upgrade
migration needed.

**Renumbering plan:**

| Current filename | New filename | Rationale |
|------------------|--------------|-----------|
| `058_discord_dest_type.sql` | `058_discord_dest_type.sql` | Keep (first in sort) |
| `058_mcp_oauth.sql` | `065_mcp_oauth.sql` | Move to end |
| `059_enrich_prompt_versions.sql` | `059_enrich_prompt_versions.sql` | Keep |
| `059_mcp_audit_actions.sql` | `066_mcp_audit_actions.sql` | Move to end |
| `060_enrich_prompt_activate_audit_action.sql` | `060_enrich_prompt_activate_audit_action.sql` | Keep |
| `060_mcp_refresh_token_session.sql` | `067_mcp_refresh_token_session.sql` | Move to end |
| `061_enrich_prompt_versions_repair.sql` | `061_enrich_prompt_versions_repair.sql` | Keep |
| `061_mcp_sessions_fk_index.sql` | `068_mcp_sessions_fk_index.sql` | Move to end |
| `062_enrich_prompt_policy_config.sql` | `062_enrich_prompt_policy_config.sql` | Keep |
| `062_mcp_codes_hash.sql` | `069_mcp_codes_hash.sql` | Move to end |
| `063_enrich_prompt_active_version_tenant_fk.sql` | `063_enrich_prompt_active_version_tenant_fk.sql` | Keep |
| `064_retry_enrichment_audit_action.sql` | `064_retry_enrichment_audit_action.sql` | Keep |

**Result:** 69 migrations with unique prefixes 001–069. MCP migrations move to
065–069 (they have no dependencies on enrich migrations, so order is safe).

**Verification:**
```bash
# After renumbering
ls internal/infra/database/migrations/*.sql | xargs -I{} basename {} | cut -d'_' -f1 | sort | uniq -d
# Should output nothing
```

### Phase 1: Tracker table extension

Add migration `070_migration_checksum_ledger.sql`:

```sql
-- Extend tracker with checksum, duration, binary version, success flag.
-- Existing rows get empty checksum (legacy marker) and success=true.

ALTER TABLE schema_migrations_feedback
    ADD COLUMN IF NOT EXISTS checksum    VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS duration_ms INT,
    ADD COLUMN IF NOT EXISTS applied_by  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS success     BOOLEAN NOT NULL DEFAULT TRUE;

COMMENT ON COLUMN schema_migrations_feedback.checksum IS
    'SHA-256 hex of migration file content; empty string for legacy rows (pre-070)';
COMMENT ON COLUMN schema_migrations_feedback.duration_ms IS
    'Execution time in milliseconds; NULL for legacy rows';
COMMENT ON COLUMN schema_migrations_feedback.applied_by IS
    'Binary version that applied this migration (e.g. "attune v0.9.0"); empty for legacy rows';
COMMENT ON COLUMN schema_migrations_feedback.success IS
    'FALSE if migration started but crashed before recording completion (dirty state)';
```

**Backward compatibility:** Existing rows keep `checksum=''`, which is treated
as "legacy, skip verification". New rows get computed checksums.

### Phase 2: Core data structures

```go
// internal/infra/database/migration.go

// MigrationRecord represents a row in schema_migrations_feedback.
type MigrationRecord struct {
    Version    int
    Filename   string
    AppliedAt  time.Time
    Checksum   string        // SHA-256 hex, empty for legacy
    DurationMs sql.NullInt32 // NULL for legacy
    AppliedBy  string        // Binary version, empty for legacy
    Success    bool
}

// MigrationFile represents an embedded migration file.
type MigrationFile struct {
    Name     string
    Prefix   int    // Numeric prefix extracted from filename
    Body     []byte
    Checksum string // Computed SHA-256 hex
    NoTx     bool   // Has migrate:no-transaction directive
}

// MigrationStatus represents the combined state for CLI/API output.
type MigrationStatus struct {
    Total       int                `json:"total"`
    Applied     int                `json:"applied"`
    Pending     int                `json:"pending"`
    Migrations  []MigrationDetail  `json:"migrations"`
    Checksums   ChecksumStatus     `json:"checksums"`
    Duplicates  []DuplicatePrefix  `json:"duplicates"`
    Elapsed     string             `json:"elapsed"`
}

type MigrationDetail struct {
    Version    int       `json:"version"`
    Filename   string    `json:"filename"`
    Status     string    `json:"status"` // "applied", "pending", "missing"
    AppliedAt  *string   `json:"applied_at,omitempty"`
    DurationMs *int      `json:"duration_ms,omitempty"`
    AppliedBy  *string   `json:"applied_by,omitempty"`
    Checksum   *string   `json:"checksum,omitempty"`
}

type ChecksumStatus struct {
    Verified int `json:"verified"`
    Total    int `json:"total"`
    Drifted  []ChecksumDrift `json:"drifted,omitempty"`
}

type ChecksumDrift struct {
    Version  int    `json:"version"`
    Filename string `json:"filename"`
    Stored   string `json:"stored"`   // First 12 chars + "..."
    Computed string `json:"computed"` // First 12 chars + "..."
}

type DuplicatePrefix struct {
    Prefix int      `json:"prefix"`
    Files  []string `json:"files"`
}
```

### Phase 3: Checksum calculation

```go
// internal/infra/database/checksum.go

package database

import (
    "crypto/sha256"
    "fmt"
)

// Checksum returns the SHA-256 hex digest of migration file content.
// The result is a 64-character lowercase hex string.
func Checksum(body []byte) string {
    return fmt.Sprintf("%x", sha256.Sum256(body))
}

// ChecksumShort returns truncated checksum for display (12 chars + "...").
func ChecksumShort(checksum string) string {
    if len(checksum) <= 12 {
        return checksum
    }
    return checksum[:12] + "..."
}
```

### Phase 4: Duplicate prefix detection

```go
// internal/infra/database/validate.go

package database

import (
    "fmt"
    "sort"
    "strconv"
    "strings"
)

// ErrDuplicatePrefix is returned when multiple migrations share a numeric prefix.
type ErrDuplicatePrefix struct {
    Duplicates []DuplicatePrefix
}

func (e ErrDuplicatePrefix) Error() string {
    var sb strings.Builder
    sb.WriteString("duplicate migration prefixes detected:\n")
    for _, d := range e.Duplicates {
        sb.WriteString(fmt.Sprintf("  %03d: %s\n", d.Prefix, strings.Join(d.Files, ", ")))
    }
    sb.WriteString("\nRenumber migrations to ensure unique prefixes.")
    return sb.String()
}

// DetectDuplicatePrefixes checks for numeric prefix collisions.
// Returns nil if no duplicates found.
func DetectDuplicatePrefixes(names []string) error {
    prefixes := make(map[int][]string) // prefix -> filenames
    for _, name := range names {
        parts := strings.SplitN(name, "_", 2)
        if len(parts) < 2 {
            continue // Skip malformed names
        }
        prefix, err := strconv.Atoi(parts[0])
        if err != nil {
            continue // Skip non-numeric prefixes
        }
        prefixes[prefix] = append(prefixes[prefix], name)
    }

    var dups []DuplicatePrefix
    for prefix, files := range prefixes {
        if len(files) > 1 {
            sort.Strings(files)
            dups = append(dups, DuplicatePrefix{Prefix: prefix, Files: files})
        }
    }

    if len(dups) > 0 {
        sort.Slice(dups, func(i, j int) bool { return dups[i].Prefix < dups[j].Prefix })
        return ErrDuplicatePrefix{Duplicates: dups}
    }
    return nil
}
```

### Phase 5: Checksum drift detection

```go
// internal/infra/database/verify.go

package database

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5/pgxpool"
)

// ErrChecksumDrift is returned when an applied migration's checksum doesn't match.
type ErrChecksumDrift struct {
    Drifted []ChecksumDrift
}

func (e ErrChecksumDrift) Error() string {
    var sb strings.Builder
    sb.WriteString("migration checksum drift detected:\n")
    for _, d := range e.Drifted {
        sb.WriteString(fmt.Sprintf("  %03d %s\n      stored:   %s\n      computed: %s\n",
            d.Version, d.Filename, d.Stored, d.Computed))
    }
    sb.WriteString("\nPossible causes:\n")
    sb.WriteString("  - Migration file was edited after being applied\n")
    sb.WriteString("  - Binary was built from different source than what's in the database\n")
    sb.WriteString("\nRecovery options:\n")
    sb.WriteString("  - Restore original migration file from git history\n")
    sb.WriteString("  - If change was intentional, update stored checksum (see docs/private-deploy.md)\n")
    return sb.String()
}

// ErrMissingFile is returned when an applied migration's file is missing from the binary.
type ErrMissingFile struct {
    Version  int
    Filename string
}

func (e ErrMissingFile) Error() string {
    return fmt.Sprintf("migration %03d (%s) was applied but file is missing from binary\n\n"+
        "Possible causes:\n"+
        "  - Migration file was deleted from source\n"+
        "  - Binary was built from incompatible branch\n\n"+
        "Recovery: restore the migration file or rebuild from correct source.",
        e.Version, e.Filename)
}

// VerifyChecksums compares embedded files against stored checksums.
// Only verifies rows with non-empty checksum (skips legacy rows).
func VerifyChecksums(ctx context.Context, conn *pgxpool.Conn) error {
    rows, err := conn.Query(ctx,
        `SELECT version, filename, checksum FROM schema_migrations_feedback 
         WHERE checksum != '' ORDER BY version`)
    if err != nil {
        return fmt.Errorf("query applied migrations: %w", err)
    }
    defer rows.Close()

    var drifted []ChecksumDrift
    for rows.Next() {
        var version int
        var filename, storedChecksum string
        if err := rows.Scan(&version, &filename, &storedChecksum); err != nil {
            return fmt.Errorf("scan migration row: %w", err)
        }

        body, err := migrationFS.ReadFile("migrations/" + filename)
        if err != nil {
            return ErrMissingFile{Version: version, Filename: filename}
        }

        computed := Checksum(body)
        if computed != storedChecksum {
            drifted = append(drifted, ChecksumDrift{
                Version:  version,
                Filename: filename,
                Stored:   ChecksumShort(storedChecksum),
                Computed: ChecksumShort(computed),
            })
        }
    }

    if len(drifted) > 0 {
        return ErrChecksumDrift{Drifted: drifted}
    }
    return nil
}
```

### Phase 6: Updated migration apply flow

```go
// internal/infra/database/migrate.go (modifications)

// Version is set via ldflags: -X 'github.com/.../database.Version=v0.9.0'
var Version = "unknown"

func runMigrationsLocked(ctx context.Context, conn *pgxpool.Conn) error {
    const where = "database.RunMigrations"
    logext.Infof(ctx, "[%s] start", where)

    // 1. Ensure tracker table exists with new columns
    if err := ensureTrackerTable(ctx, conn); err != nil {
        return err
    }

    // 2. Load embedded migrations
    names, err := loadMigrationNames()
    if err != nil {
        return err
    }

    // 3. Detect duplicate prefixes (fail before any apply)
    if err := DetectDuplicatePrefixes(names); err != nil {
        logext.Errorf(ctx, "[%s] duplicate prefixes,err:%+v", where, err.Error())
        return err
    }

    // 4. Verify checksums of already-applied migrations
    if err := VerifyChecksums(ctx, conn); err != nil {
        logext.Errorf(ctx, "[%s] checksum verification failed,err:%+v", where, err.Error())
        return err
    }

    // 5. Apply pending migrations with new metadata
    for i, name := range names {
        version := i + 1
        if applied, _ := isApplied(ctx, conn, version); applied {
            continue
        }

        body, err := migrationFS.ReadFile("migrations/" + name)
        if err != nil {
            return fmt.Errorf("read %s: %w", name, err)
        }

        checksum := Checksum(body)
        start := time.Now()

        // Mark as started (success=false until complete)
        if err := markMigrationStarted(ctx, conn, version, name, checksum); err != nil {
            return err
        }

        // Apply migration
        if isNoTxMigration(body) {
            if err := applyMigrationNoTx(ctx, conn, version, name, body); err != nil {
                return err
            }
        } else if err := applyMigrationTx(ctx, conn, version, name, body); err != nil {
            return err
        }

        // Mark as complete
        duration := time.Since(start)
        if err := markMigrationComplete(ctx, conn, version, duration); err != nil {
            return err
        }

        logext.Infof(ctx, "[%s] applied,version:%d,file:%s,duration:%v", where, version, name, duration)
    }

    logext.Infof(ctx, "[%s] OK", where)
    return nil
}

func markMigrationStarted(ctx context.Context, conn *pgxpool.Conn, version int, filename, checksum string) error {
    _, err := conn.Exec(ctx, fmt.Sprintf(`
        INSERT INTO %s (version, filename, checksum, applied_by, success)
        VALUES ($1, $2, $3, $4, FALSE)
        ON CONFLICT (version) DO UPDATE SET success = FALSE`, trackerTable),
        version, filename, checksum, Version)
    return err
}

func markMigrationComplete(ctx context.Context, conn *pgxpool.Conn, version int, duration time.Duration) error {
    _, err := conn.Exec(ctx, fmt.Sprintf(`
        UPDATE %s SET success = TRUE, duration_ms = $2 WHERE version = $1`, trackerTable),
        version, int(duration.Milliseconds()))
    return err
}
```

### Phase 7: CLI commands

#### `cmd/attune/migrations.go`

```go
package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "time"

    "github.com/Phixsura/attune/internal/infra/config"
    "github.com/Phixsura/attune/internal/infra/database"
)

func init() {
    subcommands["migrations"] = runMigrations
}

func runMigrations(args []string) error {
    if len(args) == 0 {
        return fmt.Errorf("usage: attune migrations {status|verify|dry-run}")
    }

    switch args[0] {
    case "status":
        return runMigrationsStatus(args[1:])
    case "verify":
        return runMigrationsVerify(args[1:])
    case "dry-run":
        return runMigrationsDryRun(args[1:])
    default:
        return fmt.Errorf("unknown subcommand: %s", args[0])
    }
}

func runMigrationsStatus(args []string) error {
    fs := flag.NewFlagSet("migrations status", flag.ContinueOnError)
    format := fs.String("format", "text", "Output format: text or json")
    pending := fs.Bool("pending", false, "Show only pending migrations")
    if err := fs.Parse(args); err != nil {
        return err
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    pool, err := database.NewPool(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("connect to database: %w", err)
    }
    defer pool.Close()

    status, err := database.GetMigrationStatus(ctx, pool)
    if err != nil {
        return err
    }

    switch *format {
    case "json":
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(status)
    default:
        return printMigrationStatusText(status, *pending)
    }
}

func runMigrationsVerify(args []string) error {
    fs := flag.NewFlagSet("migrations verify", flag.ContinueOnError)
    format := fs.String("format", "text", "Output format: text or json")
    if err := fs.Parse(args); err != nil {
        return err
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    pool, err := database.NewPool(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("connect to database: %w", err)
    }
    defer pool.Close()

    conn, err := pool.Acquire(ctx)
    if err != nil {
        return err
    }
    defer conn.Release()

    // Check duplicates
    names, _ := database.LoadMigrationNames()
    dupErr := database.DetectDuplicatePrefixes(names)

    // Check checksums
    checksumErr := database.VerifyChecksums(ctx, conn)

    // Check no-tx directives
    noTxErr := database.VerifyNoTxDirectives(names)

    if *format == "json" {
        result := map[string]any{
            "duplicates": dupErr == nil,
            "checksums":  checksumErr == nil,
            "no_tx":      noTxErr == nil,
            "passed":     dupErr == nil && checksumErr == nil && noTxErr == nil,
        }
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(result)
    }

    fmt.Println("Verifying migrations...")
    passed := true

    if dupErr != nil {
        fmt.Printf("  Duplicates: FAIL\n%s\n", dupErr)
        passed = false
    } else {
        fmt.Println("  Duplicates: OK")
    }

    if checksumErr != nil {
        fmt.Printf("  Checksums: FAIL\n%s\n", checksumErr)
        passed = false
    } else {
        fmt.Println("  Checksums: OK")
    }

    if noTxErr != nil {
        fmt.Printf("  No-tx directives: FAIL\n%s\n", noTxErr)
        passed = false
    } else {
        fmt.Println("  No-tx directives: OK")
    }

    if !passed {
        os.Exit(1)
    }
    fmt.Println("\nAll checks passed.")
    return nil
}

func runMigrationsDryRun(args []string) error {
    names, err := database.LoadMigrationNames()
    if err != nil {
        return err
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    cfg, err := config.Load()
    if err != nil {
        return fmt.Errorf("load config: %w", err)
    }

    pool, err := database.NewPool(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("connect to database: %w", err)
    }
    defer pool.Close()

    pending, err := database.GetPendingMigrations(ctx, pool, names)
    if err != nil {
        return err
    }

    if len(pending) == 0 {
        fmt.Println("No pending migrations.")
        return nil
    }

    fmt.Printf("Pending migrations (%d):\n\n", len(pending))
    for _, m := range pending {
        fmt.Printf("-- %s\n", m.Name)
        fmt.Println(string(m.Body))
        fmt.Println()
    }
    fmt.Println("No changes applied. Run 'attune server' to apply.")
    return nil
}
```

### Phase 8: CI lint script

`scripts/lint-migrations.sh`:

```bash
#!/usr/bin/env bash
# lint-migrations.sh — CI guard for migration file quality
#
# Checks:
#   A. No duplicate numeric prefixes
#   B. no-transaction migrations are single-statement and idempotent
#   C. Files are named NNN_description.sql with 3-digit prefix
#
# Usage: scripts/lint-migrations.sh [--strict]
#   --strict: exit 1 on warnings (default: exit 1 only on errors)

set -euo pipefail

DIR="internal/infra/database/migrations"
STRICT=false
ERRORS=0
WARNINGS=0

[[ "${1:-}" == "--strict" ]] && STRICT=true

# ─── Check A: Duplicate numeric prefixes ───────────────────────────────────

prefixes=$(ls "$DIR"/*.sql 2>/dev/null | xargs -I{} basename {} | cut -d'_' -f1 | sort)
dups=$(echo "$prefixes" | uniq -d || true)

if [[ -n "$dups" ]]; then
    echo "ERROR: Duplicate migration prefixes detected:"
    for p in $dups; do
        echo "  $p:"
        ls "$DIR"/${p}_*.sql 2>/dev/null | xargs -I{} basename {} | sed 's/^/    /'
    done
    ((ERRORS++))
fi

# ─── Check B: no-transaction directive validation ──────────────────────────

for f in "$DIR"/*.sql; do
    name=$(basename "$f")
    if head -1 "$f" | grep -q "migrate:no-transaction"; then
        # B1: Must be single statement (allow multiple semicolons only in strings)
        # Simplified: count semicolons outside single quotes
        semicolons=$(grep -v "^--" "$f" | grep -o ";" | wc -l | tr -d ' ')
        if [[ "$semicolons" -gt 1 ]]; then
            echo "ERROR: $name has no-transaction but multiple statements ($semicolons semicolons)"
            ((ERRORS++))
        fi

        # B2: Must have idempotency guard
        if ! grep -qiE "IF (NOT )?EXISTS|CONCURRENTLY" "$f"; then
            echo "ERROR: $name has no-transaction but no idempotency guard (IF NOT EXISTS / IF EXISTS / CONCURRENTLY)"
            ((ERRORS++))
        fi
    fi
done

# ─── Check C: Naming convention ────────────────────────────────────────────

for f in "$DIR"/*.sql; do
    name=$(basename "$f")
    if ! [[ "$name" =~ ^[0-9]{3}_[a-z0-9_]+\.sql$ ]]; then
        echo "WARNING: $name doesn't match NNN_description.sql pattern"
        ((WARNINGS++))
    fi
done

# ─── Summary ───────────────────────────────────────────────────────────────

echo ""
if [[ $ERRORS -gt 0 ]]; then
    echo "Migration lint FAILED: $ERRORS error(s), $WARNINGS warning(s)"
    exit 1
elif [[ $WARNINGS -gt 0 && "$STRICT" == "true" ]]; then
    echo "Migration lint FAILED (strict mode): $WARNINGS warning(s)"
    exit 1
elif [[ $WARNINGS -gt 0 ]]; then
    echo "Migration lint passed with $WARNINGS warning(s)"
else
    echo "Migration lint passed."
fi
```

Add to CI:

```yaml
# .github/workflows/ci.yml
- name: Lint migrations
  run: scripts/lint-migrations.sh
```

### Phase 9: Preflight integration

Extend `internal/preflight/checks/migration.go`:

```go
func init() {
    preflight.Register(preflight.Check{
        Name:     "migration:pending",
        Category: preflight.CategoryMigration,
        Run:      checkMigrationPending,
    })
    preflight.Register(preflight.Check{
        Name:     "migration:integrity",
        Category: preflight.CategoryMigration,
        Run:      checkMigrationIntegrity,
    })
}

func checkMigrationIntegrity(ctx context.Context, env *preflight.Environment) preflight.Result {
    r := preflight.Result{
        Name:     "migration:integrity",
        Category: preflight.CategoryMigration,
    }

    if env.Pool == nil {
        r.Status = preflight.StatusSkipped
        r.Message = "Database pool not available"
        return r
    }

    conn, err := env.Pool.Acquire(ctx)
    if err != nil {
        r.Status = preflight.StatusFail
        r.Message = "Cannot acquire database connection"
        return r
    }
    defer conn.Release()

    // Check for duplicate prefixes
    names, _ := database.LoadMigrationNames()
    if err := database.DetectDuplicatePrefixes(names); err != nil {
        r.Status = preflight.StatusFail
        r.Message = "Duplicate migration prefixes detected"
        r.Remediation = "Renumber migrations to ensure unique prefixes. See docs/private-deploy.md."
        return r
    }

    // Check for checksum drift
    if err := database.VerifyChecksums(ctx, conn); err != nil {
        r.Status = preflight.StatusFail
        r.Message = "Migration checksum drift detected"
        r.Remediation = "Restore original migration files or update stored checksums. See docs/private-deploy.md."
        return r
    }

    r.Status = preflight.StatusPass
    r.Message = "All migration checksums verified"
    return r
}
```

### Phase 10: Documentation

Add to `docs/private-deploy.md`:

```markdown
## Migration management

### Pre-deploy verification

Before deploying, verify migration integrity:

```bash
attune migrations verify
```

This checks:
- No duplicate numeric prefixes
- All applied migrations match embedded files (checksum)
- No-transaction migrations are correctly formatted

### Status inspection

View current migration state:

```bash
attune migrations status
attune migrations status --format json
attune migrations status --pending
```

### Dry-run

Preview pending migrations without applying:

```bash
attune migrations dry-run
```

### Recovery procedures

#### Checksum drift

If startup fails with "migration checksum drift":

1. **Investigate.** Determine why the file changed:
   ```bash
   git log -p -- internal/infra/database/migrations/NNN_*.sql
   ```

2. **If the change was intentional and safe** (whitespace, comment):
   ```sql
   -- Calculate new checksum
   -- On Linux: sha256sum internal/infra/database/migrations/NNN_name.sql
   -- On macOS: shasum -a 256 internal/infra/database/migrations/NNN_name.sql

   UPDATE schema_migrations_feedback
   SET checksum = '<new-sha256-hex>'
   WHERE filename = 'NNN_name.sql';
   ```

3. **If the database state is correct but the file is wrong,** restore from git:
   ```bash
   git checkout <commit> -- internal/infra/database/migrations/NNN_name.sql
   ```

4. **If both are inconsistent,** restore from backup and reapply migrations.

#### Missing migration file

If startup fails with "migration applied but file missing":

1. The binary was built from a different source than what's in the database.
2. Restore the migration file from the branch that was deployed, or
3. Rebuild the binary from the correct source.

#### Duplicate prefixes (development only)

If lint fails with "duplicate migration prefixes":

1. Renumber one of the conflicting files:
   ```bash
   git mv 058_foo.sql 070_foo.sql
   ```

2. Update the tracker (if already applied):
   ```sql
   UPDATE schema_migrations_feedback
   SET version = 70, filename = '070_foo.sql'
   WHERE filename = '058_foo.sql';
   ```

### Expand-contract pattern

For breaking schema changes, use the expand-contract pattern:

1. **Expand** (migration N): Add new column/table, nullable or with default
2. **Deploy code** that writes to both old and new
3. **Backfill** (migration N+1 or background job): Populate new from old
4. **Deploy code** that reads only from new
5. **Contract** (migration N+2): Remove old column/table

This ensures zero-downtime deploys with rollback capability at each step.
```

## Alternatives considered

### A. Atlas-style `atlas.sum` file

Store checksums in a version-controlled Merkle tree file instead of DB.

**Pros:**
- Detects reordering/insertion (stronger than per-file checksum)
- Git-diffable for review
- No database migration for checksum column

**Cons:**
- Two sources of truth (DB tracker + sum file)
- Requires regenerating file on every migration add (developer friction)
- More complex tooling

**Decision:** Rejected. DB-stored checksums are simpler and sufficient. If
reorder detection becomes critical, we can add Merkle verification later.

### B. Per-file checksum in filename

Encode checksum in filename: `001_init_a1b2c3d4.sql`.

**Pros:**
- Immutable by design
- No tracker schema change

**Cons:**
- Verbose filenames
- Requires tooling to generate
- Breaking change for existing migrations

**Decision:** Rejected. Too invasive.

### C. Goose-style panic on duplicate

Panic immediately when duplicate prefixes are detected.

**Pros:**
- Matches goose behavior
- Impossible to proceed with bad state

**Cons:**
- Panic is harsh for recoverable error
- Poor user experience

**Decision:** Rejected. Return error with clear remediation guidance.

### D. Warn-only on checksum drift

Log warning but continue startup.

**Pros:**
- Less disruptive to operations
- Allows emergency deploys

**Cons:**
- Defeats purpose of drift detection
- Silent correctness bugs can propagate

**Decision:** Rejected. Drift is a serious integrity issue; fail fast. Future
work could add `--skip-checksum-verify` flag for emergencies.

### E. Store full SQL in tracker (Grafana pattern)

Store the executed SQL text for audit purposes.

**Pros:**
- Complete audit trail
- No checksum calculation needed

**Cons:**
- Table grows large
- Redundant with embedded files
- Doesn't detect drift (just records what ran)

**Decision:** Rejected. Checksum is more space-efficient and detects drift.

## Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Breaking existing dev databases | Medium | Low | Default empty checksum for legacy; only verify non-empty |
| Renumbering causes merge conflicts | Medium | Low | Do renumbering in dedicated PR before other migration work |
| CI lint false positives | Low | Medium | Test script against current migrations before enabling |
| Performance overhead | Low | Low | SHA-256 of 70 files (~2KB avg) < 1ms total |
| Operator confusion on recovery | Medium | Medium | Clear error messages + documented procedures |

## Implementation plan

| Phase | Scope | Files | Estimate |
|-------|-------|-------|----------|
| **0** | Renumber duplicate prefixes | 5 `git mv` commands | 30min |
| **1** | Tracker table migration | `070_*.sql` | 30min |
| **2** | Core data structures | `migration.go` | 1h |
| **3** | Checksum calculation | `checksum.go` | 30min |
| **4** | Duplicate detection | `validate.go` | 1h |
| **5** | Checksum verification | `verify.go` | 1h |
| **6** | Updated apply flow | `migrate.go` | 2h |
| **7** | CLI commands | `cmd/attune/migrations.go` | 3h |
| **8** | CI lint script | `scripts/lint-migrations.sh` | 1h |
| **9** | Preflight integration | `internal/preflight/checks/migration.go` | 1h |
| **10** | Documentation | `docs/private-deploy.md` | 1h |
| **11** | Tests | Unit + integration | 4h |

**Total:** ~2-3 days

## Verification

### Unit tests

- [ ] `TestChecksum` — SHA-256 produces 64-char hex
- [ ] `TestDetectDuplicatePrefixes` — finds duplicates, returns nil on clean
- [ ] `TestVerifyChecksums` — detects drift, allows legacy empty checksum
- [ ] `TestVerifyNoTxDirectives` — validates single-statement, idempotent

### Integration tests

- [ ] `TestMigrations_FreshDB` — applies all migrations with checksums
- [ ] `TestMigrations_UpgradeFromLegacy` — handles empty checksum rows
- [ ] `TestMigrations_DriftDetection` — fails on modified file
- [ ] `TestMigrations_MissingFile` — fails on deleted file
- [ ] `TestMigrations_DuplicatePrefix` — fails before any apply
- [ ] `TestMigrations_DirtyState` — handles crashed migration (success=false)

### CLI tests

- [ ] `attune migrations status` — correct counts and formatting
- [ ] `attune migrations status --format json` — valid JSON output
- [ ] `attune migrations verify` — passes clean, fails dirty
- [ ] `attune migrations dry-run` — shows SQL without applying

### CI verification

- [ ] `scripts/lint-migrations.sh` passes on current migrations
- [ ] CI workflow includes migration lint step
- [ ] Pre-commit hook catches duplicates locally

### Documentation verification

- [ ] Recovery procedures work as documented
- [ ] `attune migrations --help` matches docs

## References

- [Flyway Schema History Table](https://documentation.red-gate.com/fd/flyway-schema-history-table-273973417.html)
- [Prisma Migration Histories](https://www.prisma.io/docs/orm/prisma-migrate/understanding-prisma-migrate/migration-histories)
- [Atlas Migration Directory Integrity](https://atlasgo.io/concepts/migration-directory-integrity)
- [GitLab Batched Background Migrations](https://docs.gitlab.com/development/database/batched_background_migrations/)
- [Stripe Online Migrations at Scale](https://stripe.com/blog/online-migrations)
- [SOC2 Change Management Controls](https://soc2auditors.org/insights/soc-2-change-management-controls/)
- [golang-migrate Dirty Database FAQ](https://github.com/golang-migrate/migrate/blob/master/FAQ.md)
- [goose Duplicate Version Detection](https://github.com/pressly/goose)
