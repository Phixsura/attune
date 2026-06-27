<!-- markdownlint-disable MD013 -->

# Signed Compliance Evidence Packs for Audit Logs

| Field | Value |
|-------|-------|
| **Issue** | [#152](https://github.com/Phixsura/attune/issues/152) |
| **Status** | Implemented |
| **Started** | 2026-06-26 |
| **Related** | [#39](https://github.com/Phixsura/attune/issues/39) (audit log), [#43](https://github.com/Phixsura/attune/issues/43) (GDPR export), [#38](https://github.com/Phixsura/attune/issues/38) (RBAC), [#93](https://github.com/Phixsura/attune/issues/93) (MCP server) |

---

## Problem

Attune has an append-only audit log (#39), but enterprise buyers need
tamper-evident evidence exports for access reviews, incident response, and
compliance handoff (SOC 2 CC7.2, ISO 27001 A.12.4). The current CSV export
endpoint (`/fb/v1/console/audit-log/export.csv`) is an inline streaming
download with no cryptographic integrity, no manifest, and no offline
verification path. An auditor receiving this CSV has no way to prove the data
was not altered after export.

GitHub Enterprise Cloud — the most widely used developer platform — demonstrates
exactly this gap: it exports audit logs as JSON/CSV with no signing, no
manifest, and 100 MB / 10-minute hard limits. Attune can close this gap from
day one.

---

## Industry Landscape

102-agent deep research covering 20 primary sources, 85 claims extracted,
25 adversarially verified (3-vote per claim), 5 refuted, 8 confirmed findings.

### Top Systems Investigated

| System | Category | Integrity Mechanism | Export Format | Key Takeaway |
|--------|----------|-------------------|---------------|--------------|
| **AWS CloudTrail** | Cloud audit | SHA-256 hash chain + SHA256withRSA signed digest files | JSON + S3 digest | Gold standard: hourly digest files chain via `previousDigestHashValue`; signature in S3 object metadata; per-region asymmetric key pairs |
| **Cossack Labs Acra** | OSS audit (Go) | HMAC chain with per-entry key rotation: `state[n] = HMAC(k[n], LE[n] \|\| state[n-1])` | Structured log | Forward-secure integrity; separate Logger/Verifier components; Apache-2.0, Go native |
| **Google Trillian** | Transparency log | Merkle tree with gRPC inclusion/consistency proofs | gRPC API | Now in maintenance mode (→ Tessera); `SignedLogRoot` signature removed (PR #2452) — too heavy for attune's scale |
| **Sigstore Rekor** | Supply chain transparency | Merkle-backed append-only log with RESTful API | REST + OpenAPI | 99.5% SLA; v2 GA, v1 maintenance — designed for artifact signing, not audit export |
| **OCSF v1.8.0** | Schema standard | N/A (schema only) | JSON | Compliance Finding class (ID 2003) with `evidences` field; 27+ structured fields; good vocabulary reference |
| **GitHub Enterprise** | Platform audit | None | JSON / CSV | No signing, no manifest, 100 MB cap — the anti-pattern attune surpasses |
| **immudb** | Immutable DB | DualProof (Append-only Hash Tree + linear chain) | SQL / gRPC | Overkill dependency; useful pattern reference for dual integrity |
| **RFC 8785 (JCS)** | Standard | JSON Canonicalization Scheme | N/A | Deterministic JSON serialization via sorted keys + IEEE 754 normalization; essential for cross-system hash reproducibility |

### Verified Best Practices (high confidence, 3-0 unanimous votes)

1. **Hash chain linking** (CloudTrail, Acra): each digest/event references the
   previous via hash + signature, enabling chain-break detection.
2. **Asymmetric signing** (CloudTrail): RSA/Ed25519 signatures on digest files
   allow offline verification with public key only — auditors never need the
   signing secret.
3. **Deterministic serialization** (RFC 8785): canonical JSON ensures hash
   reproducibility across implementations and time.
4. **Separate creation and verification** (Acra): Logger and Verifier are
   independent components with distinct trust boundaries.
5. **Self-contained archive** (industry consensus): verification must work
   offline, without network access to the issuing system.

### Refuted Claims (0-3 votes, killed by adversarial verification)

- Three-layer architecture (event → Merkle → blockchain anchoring) from blog
  source: overcomplicated, no production precedent at this scale.
- Rekor endpoint confusion: `/api/v1/log/proof` is for tree consistency, not
  per-entry inclusion as some sources claimed.
- OCSF `raw_data_hash` for tamper-evidence: the field exists but is not a
  chain integrity mechanism.

### Research Gaps

Commercial GRC platforms (Vanta, Drata, Wiz) have proprietary export
mechanisms not publicly documented at technical depth. Their competitive
position is noted but not benchmarked.

---

## Goals / Non-goals

### Goals

- Export a tamper-evident ZIP evidence pack scoped by tenant, time range,
  action, actor, and target.
- Sign the manifest with Ed25519 so auditors can verify offline with a public
  key.
- Chain event hashes so any insertion, deletion, or modification is detectable.
- Provide a `attune audit verify-export` CLI for offline verification.
- Add Console UI for creating, polling, and downloading evidence exports.
- Audit the export creation and download events themselves (meta-auditing).
- Follow the proven GDPR async export job pattern for scalability.

### Non-goals

- Do not implement a full Merkle tree or transparency log (Trillian/Rekor
  scale). SHA-256 hash chain is sufficient and proven for our volume.
- Do not anchor hashes to an external blockchain or timestamp authority.
  Attune's own signing key is the trust root.
- Do not implement OCSF schema compliance in v1. The vocabulary is referenced
  but not mandated — attune's audit schema is already richer than OCSF
  requires.
- Do not add real-time streaming integrity (Acra-style per-write chain). The
  chain is computed at export time over the immutable audit_log rows.
- Do not replace the existing inline CSV export. The evidence pack is a
  separate, higher-assurance export path.

---

## Proposal

### 1. Archive Structure

Inspired by CloudTrail's digest file design, adapted as a self-contained ZIP:

```
audit-evidence-{tenant}-{YYYYMMDD}-{YYYYMMDD}-{jobID}.zip
├── manifest.json      # Metadata, file inventory, chain root hash, signing key ID
├── events.jsonl       # RFC 8785 canonical JSON Lines, one event per line
├── events.csv         # Human-readable CSV (same columns as existing export)
└── manifest.sig       # Detached Ed25519 signature over manifest.json bytes
```

#### manifest.json Schema

```json
{
  "version": "1.0",
  "format": "attune-audit-evidence-v1",
  "export_id": "uuid",
  "tenant_id": "...",
  "created_at": "2026-06-26T12:00:00Z",
  "created_by": {
    "type": "admin",
    "id": "...",
    "email": "..."
  },
  "filter": {
    "from": "2026-01-01T00:00:00Z",
    "to": "2026-06-26T00:00:00Z",
    "actions": [],
    "actor_type": "",
    "actor_id": "",
    "target_type": "",
    "target_id": ""
  },
  "stats": {
    "total_events": 1234,
    "first_event_at": "2026-01-01T00:12:34Z",
    "last_event_at": "2026-06-25T23:45:00Z",
    "action_counts": {
      "api_key.create": 5,
      "member.invite": 12
    }
  },
  "files": [
    {
      "name": "events.jsonl",
      "size_bytes": 524288,
      "sha256": "abc123..."
    },
    {
      "name": "events.csv",
      "size_bytes": 498000,
      "sha256": "def456..."
    }
  ],
  "integrity": {
    "algorithm": "SHA-256",
    "chain_algorithm": "SHA-256(canonical_event || previous_chain_hash)",
    "chain_hash": "final_chain_hash_hex",
    "event_count": 1234
  },
  "signing": {
    "algorithm": "Ed25519",
    "public_key_fingerprint": "SHA-256 of public key bytes, hex"
  }
}
```

### 2. Hash Chain Design

Adapted from CloudTrail's chaining and Acra's sequential formula. Each event
is canonicalized per RFC 8785 (sorted keys, compact JSON, IEEE 754 numbers),
then chained:

```
chain[0] = SHA-256(canonical_json(event[0]))
chain[n] = SHA-256(canonical_json(event[n]) || chain[n-1])
```

The final `chain[last]` value is stored in `manifest.integrity.chain_hash`.

Verification recomputes the chain from `events.jsonl` line by line and
compares. Any inserted, deleted, or modified event produces a different final
hash.

Why SHA-256 chain (not HMAC):
- The chain does not need a secret — the manifest signature provides
  authenticity. SHA-256 provides integrity within the signed manifest.
- HMAC would require sharing the secret key with verifiers, negating the
  asymmetric advantage of Ed25519 manifest signing.
- CloudTrail uses the same model: plain SHA-256 hashes for files, asymmetric
  signature for the digest.

### 3. Signing Infrastructure

New Ed25519 signing keypair, independent of the existing Tink AEAD keyset
(which is for encryption, not signing). Uses Go standard library
`crypto/ed25519`.

**Config addition** (follows the `MCPConfig → OAuth/RateLimit` sub-struct
precedent in `config.go`):

```yaml
audit:
  evidence:
    signing_key: ""             # Ed25519 private key, PEM-encoded or hex seed
    export_ttl: "72h"           # How long completed exports remain downloadable
    max_events: 500000          # Safety cap per export
    max_time_range_days: 365    # Maximum filter time range
```

Config lifecycle integration (5 touch-points in `config.go`):

1. New `AuditEvidenceConfig` struct with yaml tags.
2. `Evidence AuditEvidenceConfig` field added to existing `AuditConfig`.
3. Defaults in `applyDefaults()` or dedicated `applyAuditEvidenceDefaults()`.
4. Duration parsing in `parseAuditEvidenceFields()` called from
   `parseDerivedFields()`.
5. Validation in `validateAuditConfig()` — reject empty signing key when
   evidence export is attempted (lazy validation, not boot-time).

**Key management CLI:**

```
attune audit generate-signing-key          # Generate Ed25519 keypair, print to stdout
attune audit export-public-key             # Print the public key PEM from config
attune audit verify-export                 # Verify an exported ZIP offline
  --file ./evidence.zip                    # Path to the evidence ZIP
  --public-key ./signing-key.pub           # Public key file for signature verification
```

`generate-signing-key` outputs both private and public key in PEM format. The
operator stores the private key in config and distributes the public key to
auditors. Key rotation follows the same pattern as Tink keyset rotation — the
`public_key_fingerprint` in the manifest identifies which key signed the
export.

### 4. Async Export Job

Follows the proven GDPR `export_async.go` pattern exactly:

**Migration `082_audit_evidence_export.sql`** (job table + audit action
CHECK constraint update in one migration, following the pattern of
075 which was the last CHECK extension):

```sql
-- Job table (modeled on gdpr_export_jobs: TEXT PK, single created_by)
CREATE TABLE audit_evidence_export (
  id              TEXT         PRIMARY KEY DEFAULT gen_random_uuid()::text,
  tenant_id       TEXT         NOT NULL REFERENCES tenants(id),
  status          TEXT         NOT NULL DEFAULT 'queued',
  filter_json     JSONB        NOT NULL,
  created_by      TEXT         NOT NULL DEFAULT '',
  total_events    BIGINT,
  archive_data    BYTEA,
  archive_name    TEXT,
  error           TEXT         NOT NULL DEFAULT '',
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  started_at      TIMESTAMPTZ,
  completed_at    TIMESTAMPTZ,
  expires_at      TIMESTAMPTZ,
  downloaded_at   TIMESTAMPTZ,
  claimed_by      TEXT         NOT NULL DEFAULT '',
  claimed_at      TIMESTAMPTZ,
  last_heartbeat  TIMESTAMPTZ,
  CONSTRAINT chk_audit_evidence_export_status
    CHECK (status IN ('queued','running','completed','failed','expired'))
);

CREATE INDEX idx_audit_evidence_export_tenant
  ON audit_evidence_export (tenant_id, created_at DESC);
CREATE INDEX idx_audit_evidence_export_claim
  ON audit_evidence_export (status)
  WHERE status = 'queued';

-- Extend audit_log action CHECK with evidence actions
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value CHECK (
  action IN (
    -- ... (full cumulative action list from 075 plus:)
    'audit_evidence.create',
    'audit_evidence.download',
    'audit_evidence.expire'
  )
);
```

Schema alignment with `gdpr_export_jobs`:

| Column | GDPR pattern | This proposal |
|--------|-------------|---------------|
| PK | `id TEXT PRIMARY KEY` (UUID string) | Same |
| Actor | `created_by TEXT` (single column) | Same |
| Error | `error TEXT NOT NULL DEFAULT ''` | Same |
| Heartbeat | `last_heartbeat TIMESTAMPTZ` | Same |
| Claim time | `claimed_at TIMESTAMPTZ` | Same |

**Job lifecycle:**

```
queued → running → completed → expired
                             → downloaded (marks downloaded_at, does not change status)
       → failed
```

Worker claims with `claimed_by` + `claimed_at` + `last_heartbeat`, identical
to the GDPR worker. Stale claims (`last_heartbeat` > 5 min) are recoverable
on boot. The worker is started via `safego` in `cmd/attune/server.go`
(same pattern as GDPR worker at line ~631) and uses `workerdrain.Drainer`
for graceful shutdown.

### 5. Proto API

Add to `proto/attune/v1/audit.proto`. The existing `AuditLogService` gains
three new RPCs with `google.api.http` annotations (additive, passes
`buf breaking`). The download endpoint returns `google.api.HttpBody` for
binary streaming, same pattern as `ExportAuditLogCSV` and
`DownloadGdprExport`:

```protobuf
// --- Evidence export (added to AuditLogService) ---

service AuditLogService {
  // ... existing RPCs ...

  rpc CreateAuditEvidenceExport(CreateAuditEvidenceExportRequest)
      returns (CreateAuditEvidenceExportResponse) {
    option (google.api.http) = {
      post: "/fb/v1/console/audit-log/evidence"
      body: "*"
    };
  }
  rpc GetAuditEvidenceExport(GetAuditEvidenceExportRequest)
      returns (GetAuditEvidenceExportResponse) {
    option (google.api.http) = {
      get: "/fb/v1/console/audit-log/evidence/{job_id}"
    };
  }
  rpc DownloadAuditEvidenceExport(DownloadAuditEvidenceExportRequest)
      returns (google.api.HttpBody) {
    option (google.api.http) = {
      get: "/fb/v1/console/audit-log/evidence/{job_id}/download"
    };
  }
}

message CreateAuditEvidenceExportRequest {
  string from = 1;                  // RFC 3339
  string to = 2;                    // RFC 3339
  repeated string actions = 3;      // Filter by action types
  string actor_type = 4;
  string actor_id = 5;
  string target_type = 6;
  string target_id = 7;
}

message CreateAuditEvidenceExportResponse {
  string job_id = 1;
  string status = 2;
  int32 retry_after_seconds = 3;
}

message GetAuditEvidenceExportRequest {
  string job_id = 1;
}

message GetAuditEvidenceExportResponse {
  string job_id = 1;
  string status = 2;
  int32 total_events = 3;
  string created_at = 4;
  optional string started_at = 5;
  optional string completed_at = 6;
  optional string expires_at = 7;
  optional string downloaded_at = 8;
  optional string download_path = 9;
  optional string archive_filename = 10;
  optional string error = 11;
  int32 retry_after_seconds = 12;
}

message DownloadAuditEvidenceExportRequest {
  string job_id = 1;
}
```

HTTP mapping:

| Method | Path | RPC |
|--------|------|-----|
| POST | `/fb/v1/console/audit-log/evidence` | CreateAuditEvidenceExport |
| GET | `/fb/v1/console/audit-log/evidence/{job_id}` | GetAuditEvidenceExport |
| GET | `/fb/v1/console/audit-log/evidence/{job_id}/download` | DownloadAuditEvidenceExport |

### 6. Service Layer

New package `internal/service/auditevidence/`:

```
service/auditevidence/
├── service.go         # CreateExport, GetExport, DownloadExport
├── worker.go          # Async claim/process/heartbeat/drain loop
├── archive.go         # ZIP builder: manifest + events.jsonl + events.csv + signature
├── chain.go           # SHA-256 hash chain computation
├── canonical.go       # RFC 8785 JSON canonicalization
└── signing.go         # Ed25519 sign/verify helpers
```

The worker:
1. Claims next `queued` job via `claimed_by` + `FOR UPDATE SKIP LOCKED`.
2. Queries `audit_log` with the stored filter, streaming rows.
3. Builds `events.jsonl` in canonical JSON, computing the hash chain
   incrementally (O(1) memory for the chain, events streamed to buffer).
4. Builds `events.csv` from the same data.
5. Computes SHA-256 of both files.
6. Assembles `manifest.json` with file hashes, chain hash, and stats.
7. Signs `manifest.json` bytes with Ed25519 private key.
8. Writes everything into a ZIP buffer.
9. Calls `CompleteExportJob` with the archive bytes.

### 7. Handler Layer

New package `internal/handlers/console/auditevidence/`:

```go
type Handler struct {
    svc     evidenceService
    require middleware
}

func (h *Handler) Create(ctx context.Context, req *attunev1.CreateAuditEvidenceExportRequest) (*attunev1.CreateAuditEvidenceExportResponse, error)
func (h *Handler) Get(ctx context.Context, req *attunev1.GetAuditEvidenceExportRequest) (*attunev1.GetAuditEvidenceExportResponse, error)
func (h *Handler) Download(w http.ResponseWriter, r *http.Request)
```

Authorization: admin-only via `requireAdminStrict` (defined at `router.go`
line 451). Mount via a new `mountAuditEvidence(m chi.Router)` method on
`*Router`, called from `mountSession` (line 311), following the pattern of
`mountAuditLog` (lines 1130–1149).

### 8. CLI: `attune audit verify-export`

New file `cmd/attune/audit.go`. Register `"audit": runAudit` in the
`subcommands` map (`main.go` line 50) and add to `printUsage()` (line 97):

```
attune audit verify-export --file evidence.zip --public-key key.pub
```

Verification steps:
1. Open ZIP, read `manifest.json` and `manifest.sig`.
2. Verify Ed25519 signature of `manifest.json` bytes against public key.
3. Verify SHA-256 hashes of `events.jsonl` and `events.csv` against manifest.
4. Recompute hash chain from `events.jsonl` line by line.
5. Compare final chain hash with `manifest.integrity.chain_hash`.
6. Print structured pass/fail report.

Exit codes: 0 = all checks pass, 1 = verification failure, 2 = usage error.

Output example:
```
Audit Evidence Verification Report
===================================
Export ID:       abc123-def456
Tenant:          acme-corp
Time Range:      2026-01-01 — 2026-06-26
Total Events:    1,234

[PASS] Manifest signature verified (Ed25519, key fingerprint: sha256:abc123...)
[PASS] events.jsonl hash matches manifest (SHA-256)
[PASS] events.csv hash matches manifest (SHA-256)
[PASS] Hash chain verified (1,234 events, chain hash matches)

Result: ALL CHECKS PASSED
```

### 9. Console UI

Extend the existing Settings > Audit Log page:

- "Export Evidence Pack" button (admin-only, next to existing "Export CSV").
- Click opens a dialog with the same filter controls as the audit log list
  (time range, action, actor, target).
- Submit creates the async job, dialog switches to polling view showing
  progress.
- On completion, "Download" button fetches the ZIP.
- Expiry notice: "Available for 72 hours".
- Existing evidence exports are not listed — each export is a one-time
  download with TTL. The audit log itself records all export events.

### 10. Meta-auditing

New audit actions (added to both Go `validActions` and DB `CHECK` constraint):

| Action | When |
|--------|------|
| `audit_evidence.create` | Export job created |
| `audit_evidence.download` | Export ZIP downloaded |
| `audit_evidence.expire` | Export expired (system actor) |

These actions are themselves included in future evidence exports — the
audit_log is the single source of truth.

### 11. RFC 8785 Canonical JSON

Implement a minimal canonicalizer in `internal/pkg/canonicaljson/`:

```go
func Marshal(v any) ([]byte, error)
```

Rules per RFC 8785:
- Object keys sorted lexicographically by UTF-16 code units.
- No whitespace between tokens.
- Numbers serialized per IEEE 754: no trailing zeros, no leading zeros
  (except `0.x`), no `+` prefix on exponents.
- Strings: minimal escape sequences (`\"`, `\\`, `\b`, `\f`, `\n`, `\r`,
  `\t`), code points U+0000–U+001F use `\uXXXX`.
- No BOM, no trailing newline.

For attune's audit events, the practical impact is: sorted keys + compact
encoding. The number normalization matters because `before_json`/`after_json`
JSONB fields may contain numeric values that PostgreSQL normalizes differently
than Go's `encoding/json`.

---

## Alternatives Considered

### A. HMAC-SHA256 chain (Acra-style) instead of SHA-256 + Ed25519

Acra uses `HMAC(k[n], event || prev_state)` with per-entry key rotation.
This provides forward secrecy but requires sharing the secret key for
verification — unsuitable for third-party auditor handoff. CloudTrail's
model (plain hashes + asymmetric signature) is better for compliance
workflows where the auditor must not possess the signing key.

### B. Merkle tree (Trillian/immudb) instead of linear hash chain

Merkle trees enable O(log n) inclusion proofs and consistency proofs. However:
- Trillian is in maintenance mode (Tessera is the successor).
- immudb is a full database, not a library.
- Attune's audit log is append-only in PostgreSQL; the chain is computed at
  export time, not at write time. A linear chain is simpler, sufficient, and
  verifiable in O(n) — the same time it takes to read the export.
- If attune ever needs real-time inclusion proofs, this can be added as a
  separate concern without changing the export format.

### C. Inline signing (sign each event row) instead of manifest signing

Signing every row at write time (like Acra's Logger) would require the signing
key to be available at every audit write path. This increases the blast radius
of key compromise and adds latency to every audited operation. CloudTrail's
approach — sign a batch digest file periodically — is safer and simpler.
Export-time signing achieves the same integrity guarantee for compliance handoff.

### D. Blockchain anchoring (RFC 3161 timestamp authority / public blockchain)

Adds independent third-party verifiability but introduces an external
dependency, cost, and latency. No production system in the verified research
(CloudTrail, Acra, GitHub, immudb) uses blockchain anchoring. Defer until an
enterprise customer requires it — the manifest format has room for an
`external_anchors` field.

### E. Tink signing keyset instead of Go stdlib crypto/ed25519

Tink supports Ed25519 signatures, but the existing Tink infrastructure is
AEAD-only. Adding a second keyset type would require config schema changes,
CLI extensions, and documentation — complexity that does not pay for itself
when Go's `crypto/ed25519` is a single-function API with no dependencies.
If Tink signing becomes valuable later (e.g., for key versioning in the
signing keyset), the manifest format supports migration via the
`public_key_fingerprint` field.

---

## Risks / Tradeoffs

| Risk | Mitigation |
|------|-----------|
| Large exports (500K+ events) produce multi-MB ZIPs stored as BYTEA | Safety cap `max_events` in config; GDPR export uses the same pattern at smaller scale; file-based storage is a future optimization |
| Ed25519 private key in YAML config file | Same trust model as the Tink AEAD keyset and database credentials; documented as "never commit to VCS" in security baseline |
| RFC 8785 canonicalization edge cases (Unicode, number precision) | Implement only the subset attune's audit schema actually uses; fuzz test against Go's `encoding/json` round-trip |
| Export-time chain (not write-time) means the DB is the trust root | Acceptable: the audit_log table has an append-only trigger (#39) and is the existing trust root; the chain adds verifiable integrity *after* export |
| Worker crash mid-export leaves a stale claimed job | Heartbeat + stale recovery on boot (proven pattern from GDPR/outbox workers) |

---

## Implementation Plan

### Phase 1: Backend Core (day 1)

1. Migration `082_audit_evidence_export.sql` — job table + indexes + audit
   action CHECK constraint extension (single migration).
2. Register `audit_evidence.create`, `audit_evidence.download`,
   `audit_evidence.expire` in `service/auditlog/actions.go`.
3. `internal/pkg/canonicaljson/` — RFC 8785 canonical JSON marshaler + tests.
4. `internal/service/auditevidence/` — service, worker, archive builder,
   chain, signing.
5. `internal/repo/auditevidence/` — job CRUD, claim, heartbeat, complete,
   fail, expire (modeled on `repo/gdpr/export_jobs.go`).
6. Config additions: `AuditEvidenceConfig` struct, defaults, parsing,
   validation in `config.go`.
7. Wire worker via `safego` in `cmd/attune/server.go` (follows GDPR worker
   pattern at line ~631).

### Phase 2: API + Handler + CLI (day 2)

8. Proto additions to `audit.proto` + `make proto` + commit generated output.
9. `internal/handlers/console/auditevidence/` — Create, Get, Download
   handlers.
10. Router: `mountAuditEvidence` method + `requireAdminStrict` middleware.
11. `cmd/attune/audit.go` — `generate-signing-key`, `export-public-key`,
    `verify-export` subcommands + register in `subcommands` map.
12. Unit tests: service, handler, CLI, canonical JSON, chain, signing.

### Phase 3: Console UI + Integration Tests (day 3)

13. Console: "Export Evidence Pack" button + async dialog + download.
14. PostgreSQL integration tests: full job lifecycle, chain verification,
    permission checks, expiry.
15. End-to-end test: create export → download → CLI verify → pass.
16. Observability: metrics for export job duration, size, failure rate.
17. Documentation: update `docs/private-deploy.md` with signing key setup.

---

## Verification

- [ ] `go vet ./...` — 0 warnings.
- [ ] `go build ./...` — 0 errors.
- [ ] `go test -race ./...` — all pass.
- [ ] `golangci-lint run` — 0 findings (including new packages).
- [ ] `make proto` — generated output committed, `buf lint` + `buf breaking`
  pass.
- [ ] `lizard . -l go -C 15 -T nloc=100 --warnings_only` — 0 findings.
- [ ] `scripts/lint-rawptr.sh` — 0 findings.
- [ ] `scripts/lint-errorcode.sh` — 0 findings.
- [ ] `scripts/lint-slog.sh --strict` — 0 findings.
- [ ] `scripts/lint-artifacts.sh --strict` — 0 findings.
- [ ] Integration test: full export → download → verify lifecycle.
- [ ] CLI `attune audit verify-export` succeeds on a valid export and fails
  on a tampered one.
- [ ] Console export flow works end-to-end in browser.
- [ ] Meta-audit: `audit_evidence.create` and `audit_evidence.download` rows
  appear in audit log after export + download.
- [ ] CHANGELOG.md updated.

---

## References

- AWS CloudTrail log file validation: [docs](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-log-file-validation-intro.html), [digest file structure](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-log-file-validation-digest-file-structure.html)
- Cossack Labs Acra audit log security: [blog](https://www.cossacklabs.com/blog/audit-logs-security/), [docs](https://docs.cossacklabs.com/acra/security-controls/security-logging-and-events/audit-logging/)
- RFC 8785 JSON Canonicalization Scheme: [IETF](https://datatracker.ietf.org/doc/html/rfc8785)
- OCSF Compliance Finding (ID 2003): [schema.ocsf.io](https://schema.ocsf.io/)
- GitHub Enterprise audit log export: [docs](https://docs.github.com/en/enterprise-cloud@latest/admin/monitoring-activity-in-your-enterprise/reviewing-audit-logs-for-your-enterprise/exporting-audit-log-activity-for-your-enterprise)
- Google Trillian: [github.com/google/trillian](https://github.com/google/trillian)
- Sigstore Rekor: [github.com/sigstore/rekor](https://github.com/sigstore/rekor)
- immudb: [github.com/codenotary/immudb](https://github.com/codenotary/immudb)
- Attune audit log proposal (#39): `docs/proposals/2026/06/2026-06-16-audit-log-sensitive-console-actions.md`
- Attune GDPR export proposal (#43): `docs/proposals/2026/06/2026-06-17-gdpr-export-delete-user-data.md`
