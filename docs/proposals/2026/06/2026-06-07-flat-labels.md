# Proposal — flatten kind/severity/modules into per-tenant labels

| | |
|---|---|
| **Issue** | #10 |
| **Status** | Accepted |
| **Started** | 2026-06-07 13:30 CST |
| **Related** | supersedes part of `docs/proposals/2026-06-06-enricher-per-tenant-prompt.md` (the kind/severity Non-goal) |

## Problem

`docs/proposals/2026-06-06-enricher-per-tenant-prompt.md` made `modules`
per-tenant but kept `kind` (`bug | feature | ops | question | other`) and
`severity` (`P0 | P1 | P2 | P3`) global, hardcoded in
`domain.ValidKinds` / `domain.ValidSeverities`. The browser walk-through
of the #10 demo exposed why that's the wrong shape: **GitHub gives every
repo a flat, per-repo label set. There is no global `kind` / `severity`
axis it imposes.** We've been pushing software-engineering vocabulary
(`bug vs feature`, `P0..P3`) onto customers whose real classification
needs are different:

- A customer-support SaaS wants `["售前咨询", "续费投诉", "技术支持"]`,
  not "bug vs feature".
- A game ops team wants `["匹配卡顿", "充值未到账", "外挂举报"]`, not
  "ops vs question".
- A consumer app might want a 3-level priority, not 4-level; or named
  ("紧急 / 普通 / 可延后") not "P0..P3".

Only delivering `modules` as per-tenant fulfils 1/3 of the
classification-is-per-tenant promise. The other 2/3 (`kind`, `severity`)
are still us-deciding-for-the-customer.

## Goals

- One per-tenant **label taxonomy** replaces the three axes. AI picks 0-N
  labels from this set per row.
- Per-tenant **urgent_labels** subset replaces the implicit `P0/P1 →
  雷达` routing.
- Wire shape: `enriched_labels []string` + `is_urgent bool` replace
  `enriched_kind` / `enriched_severity` / `enriched_modules` /
  `enriched_priority`.
- Existing #10 mechanics carry over unchanged:
  - prompt template + `{{labels}}` token (replaces `{{modules}}`)
  - Gate (2) post-parse whitelist on labels
  - structured output schema with labels enum
  - suggested-label signal (metric + log) for off-list emissions
  - eval `LabelSumIoU` replaces `ModuleSumIoU`

## Non-goals (this PR only)

- Color customization per label (later, single follow-up).
- Hierarchical labels (parent/child) — flat for now.
- SLA / routing-rule engine per label — only the urgent flag for now.
- Migrating pre-existing rows from kind/severity/modules → labels — the
  old columns get dropped clean (pre-1.0, the demo tenant has no
  persisted history worth keeping).

## Proposal

### Schema — migration `014_flatten_to_labels.sql`

```sql
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS enrich_label_taxonomy TEXT[],
    ADD COLUMN IF NOT EXISTS urgent_labels         TEXT[];

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS enriched_labels TEXT[],
    ADD COLUMN IF NOT EXISTS is_urgent       BOOLEAN NOT NULL DEFAULT FALSE;

-- Old columns (enriched_kind / enriched_severity / enriched_modules /
-- enriched_priority on user_feedback; enrich_modules on tenants) are
-- left in place; the application code stops reading and writing them
-- this PR. A follow-up issue drops them when downstream consumers
-- (none today) have migrated.
```

### Domain

```go
type Enriched struct {
    Title     string   `json:"title"`
    Labels    []string `json:"labels"`
    IsUrgent  bool     `json:"is_urgent"`  // derived: any label ∈ tenant.UrgentLabels
    Rationale string   `json:"rationale"`
}
```

`ValidKinds` / `ValidSeverities` / `SeverityWeight` / `Snapshot.IsHighSeverity`
all delete.

### Prompt

Default prompt is rewritten to ask the model to pick 0-N labels from a
list. `{{labels}}` token replaces `{{modules}}`. JSON schema collapses
to:
```json
{"title": "...", "labels": [...], "rationale": "..."}
```
No `kind` / `severity` keys.

### Gate (2)

`filterLabels(produced, allowed []string)` — same case-insensitive
canonical-spelling + suggested-label-signal mechanics as `filterModules`.

### Urgency routing

`IsUrgent` is derived in the enricher: `for any label in
parsed.Labels: if tenant.UrgentLabels contains it canonically, then
IsUrgent = true`. Snapshot carries `IsUrgent`. Lark webhook's "雷达"
push fires iff `IsUrgent`. Default `urgent_labels = nil` means no row
ever routes to 雷达 — explicit opt-in, which is correct for tenants
who haven't decided yet.

### Console

- Settings page gains: **label taxonomy** tag input + **urgent labels**
  tag input (the latter must be a subset of the former, validated
  client-side).
- Feedback list: kind + severity columns collapse into a `labels` chip
  column; an urgent dot/badge appears when `is_urgent`.
- Detail sheet: `enriched_kind` / `enriched_severity` badges become a
  flat label-chip row.
- Donut chart "本月分布" by kind is replaced with a **top-5 labels bar**
  (clearer at a glance, no fake "其他" bucket).

### Webhook contract — breaking

Raw-webhook / outbox envelope's `enriched` block goes from
`{title, kind, severity, modules, priority, rationale, enriched_at}` to
`{title, labels, is_urgent, rationale, enriched_at}`. Pre-1.0; any
consumer must adapt. CHANGELOG flags it loudly. `buf breaking` will
also flag.

## Alternatives considered

- **Per-tenant kind + severity AS SEPARATE AXES** (Option A from the
  walk-through). Cleaner backward compat, but it ratifies the
  "axes" model — exactly what GitHub's flat-label success shows is
  wrong for general classification. Costs ~the same to build; product
  fit strictly worse.
- **Keep axes, expose them as well-known labels (label "kind:bug",
  "severity:P0")**. Compromise that avoids breaking changes, but every
  tooling integration would have to special-case the prefix, and we
  carry a permanent "two systems" complexity. Rejected as cargo.

## Risks / tradeoffs

- **Breaking webhook contract** — pre-1.0, demo tenant only, acceptable.
  CHANGELOG `### Changed` + bold warning.
- **Old SQL columns left as garbage** — accepted; drop in a clean
  follow-up.
- **Default urgent_labels empty** ⇒ Lark 雷达 channel goes silent for
  tenants that don't configure it. This is correct (silent > noisy), but
  document in the migration release note.

## Implementation plan

Single PR (this one, continuing #10) in 6 commits, by layer:

1. Foundation: proposal, migration 014, `domain.Enriched`,
   `tenant.EnrichConfig`, tenant repo.
2. Enricher: prompt, parse, filter, schema, urgency derivation,
   `ConfigService`.
3. Wire: `proto` regen, console handler, notify adapters
   (lark-card, github-issue, outbox, raw-webhook), triage cleanup.
4. Read path: `feedback` repo, console feedback handler, eval, weekly
   digest.
5. Console UI: feedback list / detail / chart / settings + i18n.
6. Tests / CHANGELOG `### Changed` (breaking) / cleanup.

## Verification

- Empty taxonomy on a fresh tenant → `enriched_labels = []`, `is_urgent =
  false`, no console crash. Same "free-form" vibe as #10's modules.
- Configured `["售前咨询","续费投诉"]` + urgent `["续费投诉"]` → feedback
  with content matching second label sets `is_urgent = true`, routes to
  lark-radar in the demo.
- `buf breaking` will flag the envelope changes; ack in CHANGELOG.

## References

- GitHub Issues labels (the model we're aligning with).
- Linear labels, Sentry tags, Notion select.
- `docs/proposals/2026-06-06-enricher-per-tenant-prompt.md` (#10 main
  proposal, Non-goals item 2 — this addendum supersedes that one).
