# Proposal — Metadata-driven Enrich Dimensions (with first-class i18n)

| | |
|---|---|
| **Issue** | #10 |
| **Status** | Implemented |
| **Started** | 2026-06-07 13:30 CST |
| **Updated** | 2026-06-07 — pivoted from "flat labels + urgent boolean" (B) to **metadata-driven Dimensions with per-display i18n** (E3) after reviewer directions: "borrow the industry's principles wholesale, take it all the way, keep it extensible" and "display_name supports i18n". Landed on PR #86. |
| **Related** | supersedes part of `docs/proposals/2026/06/2026-06-06-enricher-per-tenant-prompt.md` (the kind/severity Non-goal) |

> attune is Apache-2.0 OSS and this proposal is the canonical English
> version so external contributors can read it cold. Where Chinese strings
> appear, they are concrete i18n examples for the design itself
> (`DisplayName` maps demonstrating how a tenant could configure
> bilingual labels) — not commentary.

---

## TL;DR (30-second expectation for reviewers)

- **Decision**: not two more hard-coded columns (`type` + `severity`). **Introduce a metadata-driven `Dimension` abstraction** — the schema stops changing with the business. Adding any new dimension is a settings edit, not a code change.
- **Industry evidence**: Jira (Custom Fields), Linear (Custom Properties, 2024), Sentry (Tags), Datadog (universal tagging), GitHub Projects v2 (Fields metadata) — **every SaaS-grade ticketing / feedback / observability system converges on metadata-driven storage in v2**. attune lands there in v1 and skips their detour.
- **Core abstraction**: `Dimension { Name, DisplayName: I18nString, Kind ∈ {single, multi}, Taxonomy []Taxonomy, UrgentSet []string, Required bool }`. Each tenant owns a list of `Dimension`. The LLM emits values keyed by `Name`; `is_urgent` is derived by `OR` over each dim's `UrgentSet`.
- **First-class i18n**: `DisplayName` is an `I18nString = map[locale]string` for both `Dimension` and each `Taxonomy` entry. The LLM and the wire format always use the stable `Value` (machine identifier); the console resolves the display by the current user's locale (preference → `"default"` → first entry). **This is more i18n than Linear / Jira / GitHub offer** — appropriate for a service that serves bilingual ops teams natively.
- **Default OSS bundle**: 3 dimensions — `type` (single, 4 values) + `severity` (single, 3 values, `urgent_set=["critical"]`) + `labels` (multi, freeform). All three are deletable / fully editable in Settings (GitHub-style freedom).
- **Identifier discipline**: `Name` is a stable machine key (`^[a-z][a-z0-9_]{0,30}$`, immutable after creation). `DisplayName` is i18n display, freely editable. `Taxonomy.Value` is a stable machine string (any Unicode, immutable after creation); `Taxonomy.DisplayName` is i18n display, freely editable.
- **Breaking change**: `Enriched` domain shape, `user_feedback` schema, and webhook envelope all change. Pre-1.0, the demo tenant has no production history; old SQL columns drop outright (no 015 cleanup tail).
- **Effort**: the current worktree (B direction — `labels TEXT[] + is_urgent BOOL`, ~60% done) gets refactored to metadata-driven Dimensions. Estimate 8-10 commits.

---

## Problem

`docs/proposals/2026/06/2026-06-06-enricher-per-tenant-prompt.md` made `modules`
per-tenant but kept `kind` (`bug | feature | ops | question | other`) and
`severity` (`P0 | P1 | P2 | P3`) globally hard-coded. The #10 demo browser walkthrough
exposed the shape as wrong: **GitHub gives every repo a flat per-repo label
set; there is no globally imposed axis.** Real customer classification
needs are not software-engineering vocabulary:

- A customer-support SaaS needs values along the lines of "pre-sales inquiry / renewal complaint / technical support", not "bug vs feature".
- A game ops team needs values along the lines of "matchmaking lag / payment-not-credited / cheating report", not "ops vs question".
- A consumer app may want a 3-level priority instead of 4, with locally-named values.

And more critically — **the number of axes is itself per-tenant**. Some customers
need one multi-select labels dim; others need type + severity + area + impact + one
custom field. **Any hard-coded N-axis model is wrong because N is wrong.**

On top of that, attune serves bilingual ops teams (China-first vendors with
overseas users, English-first SaaS with translated UI). A dim called "severity"
should render as `Severity` for an English colleague and `严重程度` for a Chinese
colleague viewing the same row — **without two parallel configurations**.

---

## Top-repo benchmarking

15 first-hand sources (top-tier GitHub repos / Linear / Sentry / PagerDuty / incident.io /
Kubernetes / Rust Forge / Jira Custom Fields / Datadog Universal Tagging / Notion
case study / Postgres JSONB docs / BCP 47).

### A · Each product's real model

| Source | Real model | One-line observation |
|---|---|---|
| **GitHub Issues (2025-04+)** | **Type (single-select enum, org-level) + Labels (multi-select free-form, repo-level)** | [2025-04 Issue Types ship](https://github.blog/changelog/2025-04-09-evolving-github-issues-and-projects/) — the official 13-year correction to "labels express everything". |
| **GitHub Projects v2** | **Fully metadata-driven Fields** (text / number / date / single-select / iteration) | v1 was hard-coded fields, v2 is Custom Fields. GitHub's own dogfooding confirms metadata is the answer. |
| **microsoft/vscode** | **Prefix-pseudo-axis flat labels** (697) | Status uses `*as-designed/*duplicate`, area uses `agent-*` — admits axes exist, just hopes labels can hold them. |
| **kubernetes/kubernetes** | **Strong-prefix multi-axis flat labels** (214) | `kind/bug`, `priority/critical-urgent`, `area/apiserver` — `prefix/value` simulates axes on flat labels. |
| **rust-lang/rust** | **Strong-prefix multi-axis flat labels** (959) | `P-*` + `A-*` + `T-*` + `I-*` + `C-*` — multi-prefix is a low-fi metadata system. |
| **Linear** | **Fixed 5-level Priority enum + Label Groups + free labels** + [Custom Properties (2024)](https://linear.app/changelog) | 2024 added Custom Properties — Linear too is walking toward metadata-driven. |
| **Jira** | **Priority enum + Components + Labels + Custom Fields** (text/select/multi-select/date/user/...) | Custom Fields are the v2 extension point — Jira's evolution proves hard-coded fields don't last. |
| **Sentry** | **Level + Priority + Tags (free key:value, indexed)** | Tags have been metadata from day one — any dimension, any slice. |
| **PagerDuty** | **Severity ≠ Urgency ≠ Priority** | The three concepts are officially independent: severity is content, urgency is routing. |
| **incident.io** | **3-5 reading-friendly severities** | "Severity is subjective — its purpose is to mobilize quickly." |
| **Datadog** | **Universal tagging** (`team:foo, env:prod, severity:p1`) | Every resource is sliced by tag — metadata is the de-facto standard in observability. |
| **Atlassian Statuspage** | 4-level impact enum | Auto-calculated from components, with operator override. |
| **Notion (Enterpret case study)** | >700 tags, fully flat, NLP-derived second-level "reason" semantics | Massive tag oceans work, but Notion explicitly splits "product area" and "user reason" — implicit dimensions. |

### B · Core trend

**The industry does not converge on flat labels. It converges on metadata-driven
Custom Fields / Tags / Dimensions.** Four independent pieces of evidence:

1. **GitHub 2025-04** adds Issue Types after 13 years. **GitHub Projects v2** is fully Custom Fields.
2. **Linear 2024** introduces Custom Properties — they refused hard-coded axes beyond priority on day one, but still ended up adding a metadata entry point in 2024.
3. **Jira / Sentry / Datadog** have been metadata-first since day one — these three cover ticketing + error monitoring + observability and **all agree**.
4. **The Notion case** shows even with humans + NLP, >700 tags still need an implicit two-layer "area" / "reason" split to be usable.

> **Conclusion**: hard-coded dimension counts (whether 1, 2, or 4) get overturned
> in v2 of every SaaS-grade feedback/ticketing system. Landing on metadata-driven
> Dimensions in v1 means attune skips their detour.

### C · Industrial definitions of urgent / severity / priority

| Word | Describes | Who decides | LLM reliability |
|---|---|---|---|
| **Severity** | Objective impact scope (users? traffic? feature wholly down?) | Data/monitoring-derived OR ops rubric | **Medium** — given a clear report the LLM can estimate, but lacks statistical context |
| **Priority** | Business backlog ordering (what should we do first?) | Ops / team decide — roadmap, politics, SLA | **Unreliable** — no backlog overview |
| **Urgency** | Notification intensity / routing signal | Ops / SRE-configured routing rules | **Unsuitable** — this is policy, not judgment |

**Key insight for attune**:
- The LLM **should output**: dimension values (picked from taxonomy).
- The LLM **should not output**: is_urgent — that's a boolean derived from the operator-configured `UrgentSet` per dim.

This maps cleanly onto metadata-driven design: **LLM emits `attrs`; `is_urgent` is
derived by `OR over dims: attrs[dim.name] ∩ dim.UrgentSet ≠ ∅` and persisted at
write time.** This combines PagerDuty + Sentry + Linear best practice —
content judgment goes to the LLM, routing policy stays with the operator.

---

## Proposal

### Core abstractions

```go
// internal/domain/i18n.go

// I18nString is a per-locale display map. The key "default" is the
// universal fallback when the requested locale is missing. At least one
// non-empty entry is required; persisted exactly as authored.
//
// Locale keys follow BCP 47 (e.g. "zh", "en", "ja", "zh-Hant"). The
// resolver walks the user's preferred locale chain, then "default",
// then any remaining entry.
type I18nString map[string]string

// Resolve picks the best available display for the caller's locale
// preference list. Falls back to the "default" key, then to any
// non-empty entry — never returns "" unless the map is fully empty.
func (i I18nString) Resolve(preferred []string) string { /* ... */ }
```

```go
// internal/domain/dimension.go

type DimensionKind string

const (
    DimSingle DimensionKind = "single" // LLM picks one (or none if !Required)
    DimMulti  DimensionKind = "multi"  // LLM picks 0..N
)

// Taxonomy is one allowed value for a Dimension. The Value is the
// stable machine identifier (LLM emits it; SQL filters on it; webhook
// envelopes carry it). The DisplayName is i18n surface only —
// editable without affecting persisted data or wire contracts.
type Taxonomy struct {
    Value       string     // any non-empty Unicode, immutable after creation
    DisplayName I18nString // per-locale display surface
}

// Dimension is one enrichment axis the LLM populates for each row. The
// set per tenant is open (any number of dims); the kind is closed
// ({single, multi}). All wire shapes — schema, prompt, SQL, proto,
// webhook envelope — derive from this single shape.
type Dimension struct {
    Name        string         // stable machine key: ^[a-z][a-z0-9_]{0,30}$; immutable after creation
    DisplayName I18nString     // per-locale display surface for the dim itself
    Kind        DimensionKind  // single | multi
    Taxonomy    []Taxonomy     // allowed values; empty + Kind=multi = freeform (GitHub-labels style)
    UrgentSet   []string       // subset of Taxonomy.Value whose presence flips is_urgent
    Required    bool           // if true, the LLM JSON schema marks the property required
}

type DimensionSet []Dimension
```

### Identifier discipline: Name / Value (stable) vs DisplayName (i18n)

| Field | Example | Constraint | Mutability |
|---|---|---|---|
| `Dimension.Name` | `"severity"` | `^[a-z][a-z0-9_]{0,30}$`, unique within tenant | **Immutable** — renaming = delete + recreate (operator owns migration) |
| `Dimension.DisplayName` | `{zh:"严重程度", en:"Severity"}` | At least one non-empty entry | Freely editable; UI-only |
| `Taxonomy.Value` | `"critical"` | Any non-empty Unicode, unique within Dimension | **Immutable** — renaming = delete + recreate |
| `Taxonomy.DisplayName` | `{zh:"严重", en:"Critical"}` | At least one non-empty entry | Freely editable; UI-only |
| `UrgentSet` items | `"critical"` | Must reference an existing `Taxonomy.Value` | Freely editable |

**Why Name / Value are immutable**:
- `enriched_attrs JSONB` keys are `Name`; values include `Taxonomy.Value`. Renaming would silently drift historical data.
- Webhook envelopes carry `attrs.<name>: <value>` as the customer-facing contract.
- Filter URLs use `?severity=critical` — wire-level stable.
- Renaming via "delete + recreate" forces the operator to consciously own the migration of any consumers / dashboards.

**Why DisplayName is i18n**:
- Bilingual teams should see labels in their own language without parallel configurations.
- DisplayName never enters a wire contract — it's pure UI surface.
- Resolution chain (`user locale → "default" → first entry`) means a tenant who only fills `default` gets the same UX as today.

### I18n design (first-class)

```json
// API contract returned to console:
{
  "name": "severity",
  "display_name": {
    "default": "Severity",
    "zh": "严重程度",
    "en": "Severity",
    "ja": "重大度"
  },
  "kind": "single",
  "taxonomy": [
    {"value": "critical", "display_name": {"default": "Critical", "zh": "严重", "en": "Critical"}},
    {"value": "major",    "display_name": {"default": "Major",    "zh": "重要", "en": "Major"}},
    {"value": "minor",    "display_name": {"default": "Minor",    "zh": "次要", "en": "Minor"}}
  ],
  "urgent_set": ["critical"],
  "required": false
}
```

Resolution path:
1. **Console SPA** reads the user's locale (cookie / `Accept-Language` / explicit `lang=zh` query param).
2. SPA calls `I18nString.resolve(["zh", "en", "default"])` for every `DisplayName` it renders. Helper hook `useDisplayName(i18n)` does it transparently.
3. **Backend** never resolves — it returns the full `I18nString` map. Locale logic lives in one place (the SPA).
4. **LLM prompt** uses `Value` as the canonical token; the prompt template renders i18n display as parenthetical hints to help the model disambiguate:
   ```
   severity (single, pick one or none):
     - "critical" (Critical | 严重)
     - "major"    (Major    | 重要)
     - "minor"    (Minor    | 次要)
   ```
5. **Webhook envelope** carries the `Value` only — the customer's consumer decides how to render it. (We can add an optional `attrs_display` mirror later if customers need a one-stop payload; deferred.)

**Editor UX**:
- The Settings editor shows DisplayName as a row of `<locale>: <input>` entries.
- A "+" button adds a new locale (defaults to the user's current locale).
- The `default` key is auto-created when none is set, mirroring the first authored locale.
- LLM-fronting `Value` and dimension `Name` are read-only after creation — visible, but explicitly marked immutable.

**Why not auto-translate**:
- We never auto-fill missing locales with machine translation. The operator owns the wording; partial maps are valid (resolver falls back).
- This keeps the system predictable: what the operator typed is what the user sees.

### Default OSS dimensions

A fresh tenant seeds with three dimensions. All are deletable and fully
editable — there is no "locked" dimension (the question of `labels` lock-in
was explicitly answered "free, like GitHub" in design review).

```json
[
  {
    "name": "type",
    "display_name": {"default": "Type", "zh": "类型", "en": "Type"},
    "kind": "single",
    "taxonomy": [
      {"value": "bug",      "display_name": {"default": "Bug",      "zh": "缺陷", "en": "Bug"}},
      {"value": "feature",  "display_name": {"default": "Feature",  "zh": "特性", "en": "Feature"}},
      {"value": "question", "display_name": {"default": "Question", "zh": "咨询", "en": "Question"}},
      {"value": "other",    "display_name": {"default": "Other",    "zh": "其他", "en": "Other"}}
    ],
    "urgent_set": [],
    "required": false
  },
  {
    "name": "severity",
    "display_name": {"default": "Severity", "zh": "严重程度", "en": "Severity"},
    "kind": "single",
    "taxonomy": [
      {"value": "critical", "display_name": {"default": "Critical", "zh": "严重", "en": "Critical"}},
      {"value": "major",    "display_name": {"default": "Major",    "zh": "重要", "en": "Major"}},
      {"value": "minor",    "display_name": {"default": "Minor",    "zh": "次要", "en": "Minor"}}
    ],
    "urgent_set": ["critical"],
    "required": false
  },
  {
    "name": "labels",
    "display_name": {"default": "Labels", "zh": "标签", "en": "Labels"},
    "kind": "multi",
    "taxonomy": [],
    "urgent_set": [],
    "required": false
  }
]
```

Notes:
- `severity.urgent_set = ["critical"]` ensures the Lark "radar" routing is non-empty on day one — otherwise a fresh deployment shows an empty radar channel and customers think the integration is broken.
- `labels` has an empty taxonomy → freeform multi-select, the GitHub-style escape valve. The LLM may emit any string; whitelist filter is a no-op when taxonomy is empty.
- Every dimension is deletable. The operator owns the configuration completely.

### Schema — migration `014_enrich_dimensions.sql` (rewritten)

```sql
-- tenants: per-tenant dimension definitions (the metadata layer)
ALTER TABLE tenants
    DROP COLUMN IF EXISTS enrich_modules,
    ADD COLUMN IF NOT EXISTS enrich_dimensions JSONB NOT NULL DEFAULT '[]'::jsonb;

-- user_feedback: LLM-populated attribute values + derived urgent flag
ALTER TABLE user_feedback
    DROP COLUMN IF EXISTS enriched_kind,
    DROP COLUMN IF EXISTS enriched_severity,
    DROP COLUMN IF EXISTS enriched_modules,
    DROP COLUMN IF EXISTS enriched_priority,
    ADD COLUMN IF NOT EXISTS enriched_attrs JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS is_urgent BOOLEAN NOT NULL DEFAULT FALSE;

-- GIN with jsonb_path_ops supports efficient `@>` containment for
-- per-dim filter queries: enriched_attrs @> '{"severity":"critical"}'.
-- jsonb_path_ops is smaller and faster than the default jsonb_ops for
-- the pure-containment access pattern we use.
CREATE INDEX IF NOT EXISTS idx_user_feedback_enriched_attrs_gin
    ON user_feedback USING GIN (enriched_attrs jsonb_path_ops);

-- Seed default dimensions for the demo tenant and any fresh tenant
-- (the backend re-seeds if enrich_dimensions = []).
UPDATE tenants
   SET enrich_dimensions = '[ ...as shown above... ]'::jsonb
 WHERE enrich_dimensions = '[]'::jsonb;
```

Pre-1.0, old columns DROP outright — the demo tenant has no production history,
and no 015 cleanup tail is needed.

**Schema choice rationale**:
- **JSONB + GIN `jsonb_path_ops`** is the Postgres-canonical pattern used by Datadog tags, Sentry tags, and Jira custom fields. `@>` containment is idiomatic and performs within a constant factor of column GINs.
- **Not EAV** (entity-attribute-value tables): joins are complex, query plans suffer, ORM expression is awkward — Jira's early EAV is the cautionary tale.
- **Not `TEXT[]`**: single-kind dims store strings, multi-kind dims store arrays. JSONB carries both in one column without per-dim normalization.

### Domain

```go
// internal/domain/feedback.go
type Enriched struct {
    Title     string         `json:"title"`
    Attrs     map[string]any `json:"attrs"`     // map<dim.Name, string|[]string>
    IsUrgent  bool           `json:"is_urgent"` // derived; persisted at write time
    Rationale string         `json:"rationale"`
}
```

`ValidKinds` / `ValidSeverities` / `SeverityWeight` / `Snapshot.IsHighSeverity` are deleted entirely.

### LLM prompt + JSON schema (generated)

```go
// internal/service/enrich/schema.go
//
// buildEnrichSchema turns the tenant's DimensionSet into the JSON schema
// the LLM must satisfy. Single dims become string + enum; multi dims
// become array of string + enum. Required dims go into the schema's
// "required" list — others may be omitted by the model.
func buildEnrichSchema(dims domain.DimensionSet) map[string]any {
    props := map[string]any{
        "title":     map[string]any{"type": "string"},
        "rationale": map[string]any{"type": "string"},
    }
    required := []string{"title", "rationale"}
    for _, d := range dims {
        var p map[string]any
        switch d.Kind {
        case domain.DimSingle:
            p = map[string]any{"type": "string"}
            if vals := taxonomyValues(d); len(vals) > 0 {
                p["enum"] = vals
            }
        case domain.DimMulti:
            items := map[string]any{"type": "string"}
            if vals := taxonomyValues(d); len(vals) > 0 {
                items["enum"] = vals
            }
            p = map[string]any{"type": "array", "items": items}
        }
        props[d.Name] = p
        if d.Required {
            required = append(required, d.Name)
        }
    }
    return map[string]any{
        "type":                 "object",
        "properties":           props,
        "required":             required,
        "additionalProperties": false,
    }
}
```

The prompt template uses a new `{{dimensions}}` token. The renderer prints each
dimension with `Name`, kind, and a `Value (i18n hints)` list — letting the
LLM disambiguate `bug` (the value) from `缺陷` (a Chinese display hint) when the operator
configured a Chinese display.

### Gate (2): per-dim post-parse whitelist

```go
// filterAttrs trims the LLM payload to the allowed shape:
//   - drops dims not in the dim set
//   - for each dim, drops values not in the taxonomy (unless freeform)
//   - emits per-dim "suggested" metric when off-list values appear
//
// Returns the canonical kept payload.
func filterAttrs(produced map[string]any, dims domain.DimensionSet) map[string]any {
    out := make(map[string]any, len(dims))
    for _, d := range dims {
        v, ok := produced[d.Name]
        if !ok {
            continue
        }
        if len(d.Taxonomy) == 0 {
            out[d.Name] = v // freeform — pass through
            continue
        }
        // ... per-Kind whitelist filter ...
    }
    return out
}
```

Per-dim metrics:
- `attune_enrich_attrs_dropped_total{tenant, dim}` — values that failed whitelist
- `attune_enrich_suggested_attrs_total{tenant, dim}` — rows where the model proposed at least one off-list value

### Urgency derivation (persisted at write time)

```go
// ComputeIsUrgent ORs every dim's UrgentSet against the row's attrs.
// Persisted to user_feedback.is_urgent so the row's urgent status is
// a snapshot at enrichment time — operator edits to UrgentSet apply to
// new rows only, never retroactively flip historical urgent/not-urgent.
func ComputeIsUrgent(attrs map[string]any, dims domain.DimensionSet) bool {
    for _, d := range dims {
        if len(d.UrgentSet) == 0 {
            continue
        }
        v, ok := attrs[d.Name]
        if !ok {
            continue
        }
        switch d.Kind {
        case domain.DimSingle:
            if s, ok := v.(string); ok && contains(d.UrgentSet, s) {
                return true
            }
        case domain.DimMulti:
            if arr, ok := v.([]string); ok && intersects(arr, d.UrgentSet) {
                return true
            }
        }
    }
    return false
}
```

**Why persist (not view / generated column / function index)**:
- **Historical snapshot semantics**: the moment a row was classified, its `is_urgent` reflects the operator's policy *then*. Demoting `severity=critical` from the urgent_set later doesn't retroactively make past urgent rows non-urgent. View mode would silently flip them — breaking the historical consistency of radar pages already sent.
- An explicit `attune recompute` CLI (future) is a known, audited operation. Silent retroactive flips are not.

Lark webhook "radar" pushes are conditional on `is_urgent`. The seeded `severity`
dim's `urgent_set=["critical"]` ensures the operator gets a sensible non-empty
radar without configuration. The trade-off is documented in Settings UI.

### Console UI (one generic renderer, all dimensions)

```tsx
// console/src/components/dim/DimensionChips.tsx
function DimensionChips({ dim, value }: { dim: Dimension; value: unknown }) {
  const displayOf = useDisplayResolver();
  if (dim.kind === "single") {
    if (!value) return null;
    const tx = dim.taxonomy.find(t => t.value === value);
    return <Badge>{tx ? displayOf(tx.display_name) : (value as string)}</Badge>;
  }
  const values = (value as string[]) ?? [];
  return (
    <>
      {values.map(v => {
        const tx = dim.taxonomy.find(t => t.value === v);
        return <Chip key={v}>{tx ? displayOf(tx.display_name) : v}</Chip>;
      })}
    </>
  );
}
```

- **List**: iterate `tenant.dimensions`; render one column per dim using `<DimensionChips>`.
- **Detail**: same component, rendered as a badge / chip row.
- **Filters**: `single → <Select>`, `multi → <MultiSelect>`. URL params use stable `Value` (`?severity=critical&labels=payment`); backend issues `enriched_attrs @> '{...}'`.
- **Stats**: for each dim, a "top values bar" — backend `jsonb_array_elements_text` or `jsonb_path_query` per kind.
- **Settings**: `<DimensionsEditor>` — add / remove dims, edit DisplayName i18n, edit Taxonomy values + i18n, edit UrgentSet, toggle Required. `Name` and `Taxonomy.Value` are read-only after creation (visible but locked).

Adding a new dim = configuration in Settings, **zero code changes**.

### Proto / wire shape

```proto
// proto/attune/v1/common.proto (or extend existing)
message I18nString {
  // locale (BCP-47, or "default") -> display text
  map<string, string> entries = 1;
}

message Taxonomy {
  string value = 1;              // stable machine identifier
  I18nString display_name = 2;
}

message Dimension {
  string name = 1;               // stable key (^[a-z][a-z0-9_]{0,30}$)
  I18nString display_name = 2;
  string kind = 3;               // "single" | "multi"
  repeated Taxonomy taxonomy = 4;
  repeated string urgent_set = 5;
  bool required = 6;
}

// proto/attune/v1/enrich_config.proto
message EnrichConfig {
  optional string prompt_template = 1;
  string default_prompt_template = 2;
  repeated Dimension dimensions = 3;
}

// proto/attune/v1/ingest.proto (Feedback / FeedbackDetail / Envelope)
message Feedback {
  int64 id = 1;
  string content = 2;
  string source = 3;
  string type = 4;
  string user_id = 5;
  string page_url = 6;
  optional string enriched_title = 7;
  // enriched_attrs is a free-form map<dim.name, value> where value is
  // either a string (single-kind dim) or a repeated string (multi-kind).
  google.protobuf.Struct enriched_attrs = 8;
  bool is_urgent = 9;
  string enrichment_status = 10;
  string created_at = 11;
}
```

Webhook envelope (v2):
```json
{
  "version": "2",
  "event_type": "feedback.enriched",
  "feedback": {
    "enriched": {
      "title": "Payment page crashed",
      "attrs": {
        "type": "bug",
        "severity": "critical",
        "labels": ["payment", "ux"]
      },
      "is_urgent": true,
      "rationale": "...",
      "enriched_at": "..."
    }
  }
}
```

Customers consume stable `Value`s — they render whatever DisplayName they need
on their side.

### `attune eval` subcommand updates

- Remove `KindAccuracy` / `SeverityAccuracy` / `ModuleSumIoU`.
- Add `AttrAccuracy{dim}` for single-kind dims and `AttrSumIoU{dim}` for multi-kind dims.
- The metric matrix is data-driven — iterate the tenant's dim set, emit one row per dim.

---

## Alternatives weighed

Scored against industry evidence (✓ strong / ~ weak / ✗ unsupported).

| Option | Description | Industry evidence | Effort | Extensibility | Score |
|---|---|---|---|---|---|
| **A. All flat labels (urgent is also a label)** | LLM picks labels freely; urgent is a specific string | vscode, Notion (both have NLP postprocessing) ~ | Low | ✗ new axes need label prefix conventions | **4/10** |
| **B. Flat labels + standalone urgent boolean** | `is_urgent = labels ∩ urgent_labels ≠ ∅` | PagerDuty / Sentry / Linear separation principle ✓✓ | Medium | ~ new axis still needs schema change | **6/10** |
| **C. labels + type + severity + is_urgent (hard-coded E2)** | Four columns | GitHub 2025 / Sentry / PagerDuty all support ✓✓✓ | Medium+ | ~ adding a fifth axis still requires code changes | **7/10** |
| **D. Metadata-driven Dimensions (E3)** ⭐ | `Dimension` abstraction, N dims via config | Jira / Linear 2024 / Sentry tags / Datadog / GitHub Projects v2 ✓✓✓✓ | Medium (one generic renderer) | ✓ adding any axis = zero code changes | **10/10** |
| **E. Fully free schema (any JSON)** | LLM emits any JSON, no schema | No top-tier repo does this ✗ | Low | ✓ but uncontrolled | **2/10** |

**Selected: D (E3 — metadata-driven Dimensions with first-class i18n)**. The
strongest evidence is the convergence of Jira / Linear (2024) / Sentry / Datadog /
GitHub Projects v2 — five projects across ticketing / error monitoring / observability
all running metadata-driven storage. The i18n layer adds a property none of them
provide first-class, which is appropriate for attune's bilingual customer base.

---

## Goals

- A `Dimension` abstraction expresses every LLM-populated enrichment axis.
- Per-tenant `[]Dimension` configuration; LLM schema, Gate (2) filter, urgency derivation, console rendering all driven by the metadata.
- First-class i18n for `DisplayName` (both on Dimension and per Taxonomy value) — bilingual teams without parallel config.
- Default OSS bundle of 3 dims (`type`, `severity`, `labels`) with `severity.urgent_set=["critical"]` so radar routing is non-empty out of the box.
- Adding a new dimension = configuration in Settings, zero code changes.
- Snapshot semantics: `is_urgent` is persisted at write time; UrgentSet edits apply prospectively.

## Non-goals (this PR)

- Renaming `Name` or `Taxonomy.Value` after creation — GitHub-style: delete + recreate, operator owns migration.
- Per-dim color / icon — follow-up issue.
- Per-dim description / help text in i18n — follow-up issue (DisplayName covers the headline; description is cosmetic).
- Hierarchical Dimensions (Linear Label Groups) — follow-up.
- Non-string typed dims (number / date / user) — follow-up; v1 is string-only (single or multi).
- Cross-tenant Dimension templates ("import from another tenant") — follow-up.
- Auto-translation of DisplayName via the LLM — follow-up; operator-authored only for v1.
- SLA / per-dim routing rules — only the urgent boolean is wired.

---

## Risks / tradeoffs

| Risk | Evidence | Mitigation |
|---|---|---|
| Go loses type safety (`Attrs map[string]any`) | Abstraction cost | Validated against DimensionSet at parse; repo layer exposes typed `GetSingle(name)` / `GetMulti(name)` accessors |
| JSONB queries less idiomatic than columns | Real | GIN `jsonb_path_ops` + `@>` is the Postgres canonical pattern; query verbosity ~10-20% higher, accepted |
| Front-end dynamic form complexity | Real | One `<DimensionsEditor>` written once; new dims need 0 front-end code |
| TypeScript narrows to `Record<string, string \| string[]>` | Real | `useDimensions()` hook returns typed dim list; dim-aware components use `dim.kind` to refine |
| UrgentSet drifts into modern alert fatigue (operator marks every dim's every value urgent) | PagerDuty warning | Settings UI shows a warning when `urgent_share > 50%`; `attune_urgent_ratio` metric tracks |
| Snapshot vs realtime semantics confusion | Design self-risk | Persist at write time + Settings UI calls out "does not affect historical rows"; future `attune recompute` CLI |
| Large tenants with >20 dims overwhelm UI | Speculative | Future: dim grouping / collapse; Jira lives with hundreds of custom fields |
| Operator regrets the chosen Name (immutable) | Real (first-time naming is hard) | Settings clearly states "Name is immutable" at creation; delete + recreate path exists |
| Operator builds an i18n map with only one locale | Expected | Resolver falls back to `default` then first entry — partial maps are valid |
| Dimension Name collides with reserved attune payload keys (`title`, `rationale`, etc.) | Real | Reject list at validation: `["title","rationale","content","attrs","is_urgent","id","tenant_id","source","user_id","_attune"]` plus a `_attune_*` prefix reservation |

**Industry counter-example honestly noted**: Jira Custom Fields scale to hundreds
per tenant, but when a tenant grows past several thousand fields the dropdown
oceans become unusable. attune is not at that scale; future dim grouping is the
graceful upgrade path.

---

## Implementation plan

### Worktree status (2026-06-07)

The current worktree is on the B direction (`labels TEXT[] + is_urgent BOOL`)
about 60% complete. **B → E3 refactor steps**:

1. **Schema**: `014_flatten_to_labels.sql` rewritten as `014_enrich_dimensions.sql` (JSONB) + old columns DROP + seed default 3 dims (with i18n display names).
2. **Domain**: add `i18n.go` + `dimension.go`; `Enriched.Labels []string + IsUrgent bool` → `Enriched.Attrs map[string]any + IsUrgent bool`.
3. **Tenant repo**: `LabelTaxonomy + UrgentLabels` → `Dimensions []Dimension`.
4. **Enricher**: prompt / schema / filter / urgency derivation, all DimensionSet-driven.
5. **Feedback repo + console handler**: `enriched_labels / is_urgent` → `enriched_attrs JSONB / is_urgent`; queries via `@>` containment.
6. **Proto**: `Dimension` + `Taxonomy` + `I18nString` messages; `EnrichConfig.dimensions`; `Feedback.enriched_attrs` (Struct).
7. **Notify adapters**: envelope `attrs` replaces `labels`.
8. **Console UI**: generic `<DimensionChips>` + `<DimensionsEditor>` + i18n resolver hook; switch list / detail / settings / stats to metadata-driven.
9. **`attune eval`**: `AttrAccuracy / AttrSumIoU` per dim.
10. **Tests + CHANGELOG `### Changed` (breaking) + browser end-to-end re-run**.

**Estimate**: 8-10 commits to land. About 2-3 commits more than B (generic renderer + dim editor + i18n editor), but 2-3 fewer than C (no per-dim hard-coded UI components). i18n surface adds maybe 1 commit on top.

---

## Verification

- Fresh tenant (3 default dims seeded) → first ingest renders 3 columns; LLM emits `attrs={type:"bug", severity:"critical", labels:["payment"]}`; `is_urgent=true` fires radar. ✓
- Settings: add a 4th dim `area` (multi, freeform) → next ingest the LLM emits `area`, console gains a column, **zero code changes**. ✓
- Settings: empty `severity.urgent_set` → new rows non-urgent; historical rows' `is_urgent` unchanged (snapshot semantics). ✓
- Settings: delete the `labels` dim → historical `attrs.labels` values stay in the JSONB; console stops rendering the labels column. ✓
- Settings: edit a Taxonomy.DisplayName from `{zh:"严重"}` to `{zh:"严重",en:"Critical"}` → switching console language updates the badge text instantly; `Value` and stored `enriched_attrs.severity` unchanged. ✓
- `buf breaking` flags envelope changes; CHANGELOG `### Changed` documents the v1 → v2 hop in bold.
- Browser end-to-end: configure → ingest → list / detail render multi-dim → Lark radar receives the matching card.

---

## References

First-hand sources (15):

- [GitHub Changelog: Evolving GitHub Issues (2025-04-09)](https://github.blog/changelog/2025-04-09-evolving-github-issues-and-projects/) — Issue Types split from labels
- [GitHub Projects v2 — Custom Fields](https://docs.github.com/en/issues/planning-and-tracking-with-projects/understanding-fields/about-fields)
- [Linear: Priority](https://linear.app/docs/priority)
- [Linear: Labels](https://linear.app/docs/labels)
- [Linear Changelog: Custom Properties (2024)](https://linear.app/changelog) — metadata-driven entry point
- [Jira: Custom Fields](https://support.atlassian.com/jira-cloud-administration/docs/configure-a-custom-field/)
- [Sentry Docs: Issue Tags](https://docs.sentry.io/product/issues/issue-details/issue-tagging/)
- [Sentry Changelog: Issue Priority GA (2024)](https://sentry.io/changelog/issue-priority-ga/)
- [Datadog: Unified Service Tagging](https://docs.datadoghq.com/getting_started/tagging/unified_service_tagging/)
- [PagerDuty: Severity Levels](https://response.pagerduty.com/before/severity_levels/)
- [incident.io: Severities Guide](https://incident.io/guide/foundations/severities)
- [Kubernetes: Issue Triage Labels](https://github.com/kubernetes/community/blob/master/contributors/guide/issue-triage.md)
- [Rust Forge: Compiler Prioritization](https://forge.rust-lang.org/compiler/prioritization.html)
- [Notion / Enterpret feedback taxonomy](https://www.enterpret.com/blog/how-notion-is-supercharging-its-product-feedback-loop-using-enterpret)
- [Crunchy Data: Tags & Postgres Arrays](https://www.crunchydata.com/blog/tags-and-postgres-arrays-a-purrfect-combination) — JSONB vs Array trade-off
- [Postgres docs: jsonb_path_ops vs jsonb_ops](https://www.postgresql.org/docs/current/datatype-json.html#JSON-INDEXING)
- [BCP 47 — Tags for Identifying Languages](https://www.rfc-editor.org/info/bcp47)

In-repo references:
- `docs/proposals/2026/06/2026-06-06-enricher-per-tenant-prompt.md` — the original #10 proposal; Non-goal 2 is superseded by this addendum.
