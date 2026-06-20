# Proposal: ModuleSumIoU surfaces off-list module suggestions

| Field    | Value                                                    |
|----------|----------------------------------------------------------|
| Issue    | [#83](https://github.com/Phixsura/attune/issues/83)      |
| Status   | Accepted                                                 |
| Started  | 2026-06-20                                               |
| Related  | #19 (proto IDL), #66 (inbound framework)                 |

---

## Problem

The eval's `ModuleSumIoU` metric compares only **filtered** modules. When the
LLM systematically suggests off-list values (e.g., tenant whitelist is
`["payment"]` but LLM outputs `["payment", "checkout"]`), the eval report is
completely blind to this behavior:

1. `applyAttrsGate` drops `"checkout"` before eval sees it
2. `scoreRow` computes IoU on the filtered `["payment"]` vs `["payment"]` → 1.0
3. Operator sees perfect score, unaware that LLM consistently wants to add a
   new taxonomy value

This is a **systematic signal loss**. The LLM's off-list suggestions are
potentially valuable taxonomy expansion candidates, but they vanish into
metrics (`EnrichSuggestedAttrsTotal`) without actionable context.

### Industry context

Research across 14+ ML/LLM evaluation platforms found:

| System | Approach | Gap |
|--------|----------|-----|
| Cleanlab | `suggested_label = argmax(pred_probs)` for mislabels | No promotion workflow |
| Evidently | `values_not_in_list_dist: {x: [values], y: [counts]}` | Distribution only, no action |
| Airbnb T-LEAF | `coverage = 1 - (other_classified / total)` | Metric only |
| Snorkel | Sparse conflict matrix for labeling functions | No taxonomy extension |
| LinkedIn NTE | Entity discovery → ML recommendation → Human validation | Closest, but not in eval |

**No platform has a complete detection → analysis → action loop for taxonomy
evolution.** This is an opportunity for attune to lead.

---

## Goals

1. **Surface off-list values** in `EvalReport` with frequency distribution and
   coverage metrics
2. **Provide confidence scoring** so operators know which suggestions are noise
   vs. systematic
3. **Enable one-click promotion** via Console API to add values to taxonomy
4. **Close the feedback loop** so promoted values improve future classification

### Non-goals

- Automatic taxonomy mutation (always operator-approved)
- Historical re-classification (P3, separate issue)
- Cross-tenant intelligence (privacy implications, separate issue)
- Semantic clustering of similar values (P3, can add later)

---

## Proposal

### Data structures

```go
// EvalReport gains a new field
type EvalReport struct {
    // ... existing fields
    SuggestedAttrs SuggestedAttrsReport `json:"suggested_attrs,omitempty"`
}

// SuggestedAttrsReport captures off-list values from classification
type SuggestedAttrsReport struct {
    // Level 1: Basic frequency distribution
    ValueFreq map[string]map[string]int `json:"value_freq"` // dim → value → count
    Coverage  map[string]float64        `json:"coverage"`   // dim → (1 - dropped/total)
    
    // Level 2: Analysis
    Candidates []SuggestedCandidate `json:"candidates,omitempty"` // sorted by confidence desc
    
    // Level 3: Actionable
    Recommendations []SuggestedRecommendation `json:"recommendations,omitempty"`
}

// SuggestedCandidate is one off-list value with analysis metadata
type SuggestedCandidate struct {
    Dim            string  `json:"dim"`
    Value          string  `json:"value"`
    Count          int     `json:"count"`
    Confidence     float64 `json:"confidence"`       // 0-1, see formula below
    CoverageImpact float64 `json:"coverage_impact"`  // predicted Δcoverage if added
    NearestValue   string  `json:"nearest_value,omitempty"` // P3: most similar existing taxonomy value (empty in P0/P1)
}

// SuggestedRecommendation is an actionable suggestion for the operator
type SuggestedRecommendation struct {
    Action string `json:"action"` // P0/P1: "add" only; P3 may add "merge", "investigate"
    Dim    string `json:"dim"`
    Value  string `json:"value"`
    Reason string `json:"reason"`
    Impact string `json:"impact"` // human-readable, e.g., "+12% coverage"
}
```

### Enricher changes

`Classify` currently returns only `domain.Enriched`. For eval to capture
suggested values, we need to expose the diagnostics:

```go
// ClassifyResult is the full output including diagnostics.
type ClassifyResult struct {
    Enriched        domain.Enriched
    DropDiagnostics []domain.AttrDropDiagnostic
}

// ClassifyWithDiagnostics exposes the existing internal classifyWithDiagnostics
// for eval to access drop diagnostics. The internal method already exists;
// this just makes it public with a return type that bundles the diagnostics.
func (e *Enricher) ClassifyWithDiagnostics(
    ctx context.Context, content string, cfg ClassifyConfig,
) (ClassifyResult, error)
```

The accumulator extracts off-list values from `DropDiagnostics` where
`Reason == AttrDropOffListValue` — no redundant `SuggestedValues` field needed.

The existing `Classify` becomes a thin wrapper for backward compatibility.

### Eval changes

`RunConsistency` accumulates suggested values across all sampled rows:

```go
func (ev *Evaluator) RunConsistency(ctx context.Context, since time.Time, sample int) (*EvalReport, error) {
    // ... existing setup
    
    suggestedAcc := newSuggestedAccumulator()
    
    for _, r := range rows {
        // Use ClassifyWithDiagnostics instead of Classify
        result, err := ev.enricher.ClassifyWithDiagnostics(ctx, r.Content, cfg)
        if err != nil {
            continue
        }
        
        // Accumulate suggested values
        suggestedAcc.Add(result.DropDiagnostics, cfg.Dimensions)
        
        // ... existing scoreRow logic
    }
    
    report.SuggestedAttrs = suggestedAcc.Build(report.SampleSize)
    return report, nil
}
```

`ScoreHuman` also surfaces suggested values. When comparing human labels vs AI
labels, both sides may contain off-list values:

- **AI off-list**: LLM suggested something not in taxonomy (same as consistency)
- **Human off-list**: Human labeled with a value not in taxonomy (rarer, but
  indicates taxonomy gap from the ground-truth perspective)

Both are captured in the same `SuggestedAttrsReport`. The `source` distinction
(AI vs human) is not tracked in P0 — a future enhancement could split them.

### Coverage metric

Coverage per dimension measures how much of the LLM's output survives filtering.
**Counted per-value, not per-row** — a multi-kind dim with `["a", "b", "c"]`
where `"c"` is off-list counts as 2 kept, 1 dropped.

```
coverage[dim] = 1 - (dropped_count[dim] / (kept_count[dim] + dropped_count[dim]))
```

- `coverage = 1.0` → LLM always outputs on-list values
- `coverage = 0.85` → 15% of LLM output values were dropped (systematic gap)
- `coverage = 0.5` → Half of values dropped (major taxonomy mismatch)

Note: `AttrDropDiagnostic.Values` is capped at 5 per row (for persistence size),
but `Count` is accurate. Coverage uses `Count`, so the cap doesn't affect it.
`ValueFreq` aggregates across rows, so rare cases where one row has >5 distinct
off-list values for one dim are acceptable losses for P0.

### Confidence scoring

Not all off-list values are equal. A value appearing once might be LLM noise;
a value appearing 50 times across independent samples is a systematic signal.

```go
func computeConfidence(count int, sampleSize int) float64 {
    // Frequency: what fraction of samples produced this value?
    freq := float64(count) / float64(sampleSize)
    
    // Confidence = freq, capped at 1.0
    // Simple and interpretable: confidence 0.24 means "appeared in 24% of samples"
    return math.Min(1.0, freq)
}
```

Simpler is better for P0. The frequency itself is the confidence score — no
compounding formulas. A value appearing in 24% of samples has confidence 0.24.
P3 may add cross-sample agreement metrics (e.g., did independent content pieces
produce the same suggestion?).

### Impact prediction

If we added value V to dimension D, how much would coverage improve?

```go
// predictImpact estimates coverage gain if candidate.Value were added to taxonomy.
// totalDroppedForDim is the total dropped count for this dimension across all samples.
func predictImpact(candidate SuggestedCandidate, currentCoverage float64, totalDroppedForDim int) float64 {
    if totalDroppedForDim == 0 {
        return 0
    }
    // This value's share of the coverage gap for its dimension
    coverageGap := 1 - currentCoverage
    valueShare := float64(candidate.Count) / float64(totalDroppedForDim)
    return coverageGap * valueShare
}
```

Example: dimension has 85% coverage (15% gap). Value "checkout" accounts for 80%
of the drops. Impact = 0.15 × 0.80 = 0.12 (+12% expected coverage).

### CLI output

```
## suggested values (off-list)

| dimension | coverage | top suggestions |
|-----------|----------|-----------------|
| modules   | 85%      | checkout (12), billing (5), auth (3) |
| sentiment | 100%     | — |

Recommendations:
• ADD modules.checkout — appeared 12 times (24%), +12% expected coverage
• ADD modules.billing — appeared 5 times (10%), +5% expected coverage
```

Note: P0/P1 recommendations are purely frequency-based ("ADD" for high-confidence
candidates). P3 may add "INVESTIGATE" or "MERGE" when semantic clustering detects
potential overlap with existing taxonomy values.

### Console API (Level 3)

New endpoints for the promotion workflow:

```proto
// Get suggested values from the most recent eval run
rpc GetEvalSuggestions(GetEvalSuggestionsRequest) returns (GetEvalSuggestionsResponse);

// Promote a suggested value to the taxonomy
rpc PromoteSuggestedValue(PromoteSuggestedValueRequest) returns (PromoteSuggestedValueResponse);

message PromoteSuggestedValueRequest {
    string tenant_id = 1;
    string dimension_name = 2;
    string value = 3;
    attune.v1.I18nString display_name = 4;
    // eval_run_id omitted in P2 — eval runs are not persisted yet.
    // P3 may add eval run persistence and reference here for audit trail.
}
```

The Console UI shows:
1. Eval report with suggested values section
2. "Add to Taxonomy" button next to each high-confidence candidate
3. Modal for display_name entry in supported locales
4. Confirmation and automatic dimension config update

### Feedback loop

When an operator promotes a value:

1. Value is added to `Dimension.Taxonomy` via existing update API
2. Audit log records `source: "suggested"` (P3 may add `eval_run_id` once runs are persisted)
3. Future enrichments use the expanded taxonomy
4. Next eval run shows improved coverage

This closes the loop: LLM suggestion → operator review → taxonomy expansion →
better classification.

---

## Alternatives considered

### A. Metrics-only approach

Just emit `EnrichSuggestedAttrsTotal` (current state) without surfacing in
eval report.

**Rejected:** Metrics lack context. Knowing "checkout was dropped 50 times"
doesn't tell you it was across 50 different samples or 50 retries of one row.
Eval report aggregates across a controlled sample.

### B. Automatic promotion with threshold

If a value appears in >20% of samples, auto-add to taxonomy.

**Rejected:** Violates operator control principle. A value might be
systematically wrong (LLM hallucination), not systematically correct. Human
judgment required.

### C. Store all suggested values in DB

Persist every off-list value to a `suggested_taxonomy` table for async review.

**Rejected for P0/P1:** Adds storage cost and complexity. Eval report already
provides a point-in-time sample. Can add persistent storage as P3 if demand
exists.

---

## Risks and tradeoffs

| Risk | Mitigation |
|------|------------|
| Large eval reports | Cap `Candidates` at 20 per dim; overflow goes to `value_freq` only |
| Noisy suggestions | Confidence threshold (default 0.1) filters single-occurrence noise |
| Console API scope | P1 is CLI-only; Console API is P2 |
| Breaking proto changes | New fields only (additive); no removal or rename |
| Freeform dims (empty taxonomy) | No off-list concept — coverage is always 100%, no suggestions |

---

## Implementation plan

### Phase 1: Core (P0) — 1-2 days

1. Add `SuggestedAttrsReport` and related types to `internal/service/eval/`
2. Create `ClassifyWithDiagnostics` in enricher (exposes existing internal)
3. Add `suggestedAccumulator` to aggregate during `RunConsistency`
4. Update `FormatReport` to render suggested values section
5. Unit tests for accumulator and formatting

### Phase 2: Analysis (P1) — 2-3 days

1. Add `SuggestedCandidate` with confidence scoring
2. Add `SuggestedRecommendation` with impact prediction
3. Update CLI output with recommendations
4. Integration test with real LLM

### Phase 3: Console API (P2) — 3-5 days

1. Proto: `GetEvalSuggestions`, `PromoteSuggestedValue`
2. Handler: wire to eval service and dimension update
3. Console UI: suggestions section, promotion modal
4. E2E test: full promotion workflow

### Phase 4: Advanced (P3) — TBD

1. Semantic clustering of similar values
2. Historical re-classification after promotion
3. Trend analysis across eval runs
4. Cross-tenant anonymized insights

---

## Verification

### Unit tests

- `TestSuggestedAccumulator_Add` — accumulates diagnostics correctly
- `TestSuggestedAccumulator_Build` — computes coverage, confidence, impact
- `TestFormatReport_WithSuggested` — CLI output format

### Integration tests

- `TestRunConsistency_SurfacesSuggested` — full eval run with off-list values
- `TestPromoteSuggestedValue_AddsTaxonomy` — API promotes value correctly

### E2E verification

1. Create tenant with taxonomy `modules: ["payment"]`
2. Ingest samples where LLM wants to output `["payment", "checkout"]`
3. Run `attune eval --mode consistency`
4. Verify report shows `checkout` as suggested with confidence > 0.5
5. (P2) Use Console to promote `checkout`
6. Re-run eval, verify coverage improved to 100%

---

## References

- [Cleanlab](https://github.com/cleanlab/cleanlab) — confident learning, label suggestions
- [Evidently](https://github.com/evidentlyai/evidently) — ML monitoring, data drift
- [Snorkel](https://github.com/snorkel-team/snorkel) — programmatic labeling, conflict detection
- [LinkedIn NTE](https://engineering.linkedin.com/blog/2023/discovering-new-entity-types) — entity discovery pipeline
- [Schema.org pending extension](https://pending.schema.org/) — staging area pattern
- [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) — changelog format
