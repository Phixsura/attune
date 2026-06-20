# VoC / Feedback-Intelligence Landscape — Competitive Benchmark

| | |
|---|---|
| **Started** | 2026-06-20 |
| **Status** | Reference (benchmark input for #32 / #95 / inbound expansion proposals) |
| **Method** | Multi-source web research (5 angles) → 21 sources → 90 claims → 25 adversarially verified → 21 confirmed / 4 refuted |
| **Scope** | The whole product category (user-feedback intelligence / VoC platforms), open-source + commercial SaaS |
| **Related** | #32 (Discord outbound), #95 (registry-driven ValidSources), #34 (multi-channel), #83 (off-list taxonomy suggestions) |

> ⚠️ **Evidence strength**: most commercial-vendor capabilities are sourced from the
> vendors' own marketing / product / help pages or vendor blogs — self-described
> capability and positioning, not third-party benchmarks. This doc only relies on
> "capability X exists / positioning Y holds"; it does **not** rely on unverified
> performance/comparative numbers ("70% less time", "broadest coverage").

---

## 1 · attune's pipeline (the benchmark baseline)

```
inbound (webhook / email / API) → enricher (classify kind·severity·modules·sentiment·language
  → embedding clustering/dedup → LLM reply draft) → PostgreSQL → fan-out (webhook / GitHub Issue / Slack / Lark / Discord*)
```

`*` Discord outbound is #32, in progress.

---

## 2 · Category landscape (three tiers)

| Tier | Representative | AI enrichment | Cluster/dedup | Auto reply-draft | Inbound breadth | OSS | BYO-LLM |
|---|---|---|---|---|---|---|---|
| **Commercial AI-native leaders** | **Enterpret** | ✅ adaptive taxonomy | ✅ cross-channel semantic merge | ❌ | 50+ sources | ❌ | ❌ |
| | **Dovetail** | ✅ auto-classify + sentiment trends | ✅ theme clustering | ❌ | wide (calls / tickets / surveys / reviews) | ❌ | ❌ |
| | **Unwrap.ai** | ✅ NLP classify / sentiment | ✅ across 3000+ sources | ❌ | very wide | ❌ | ❌ |
| **OSS · with AI** | **Quackback** (AGPL-3.0) | ✅ dedup + theme summary | ✅ (dedup-equivalent) | ❌ | ~24 integrations | ✅ | ✅ |
| | **abc-user-feedback** (Apache-2.0, LINE) | ✅ summary / translation / sentiment / issue rec | ❌ | ❌ | medium | ✅ | partial |
| **OSS · voting board** | **Astuto** (archived 2026-02) / **Fider** | ❌ | ❌ | ❌ | voting portal | ✅ | — |
| **→ attune** | | ✅ kind / severity / modules / sentiment / lang | ✅ embedding clustering | ✅ **draft-reply** | webhook + email (+ API tag) | ✅ (Apache-2.0) | ✅ multi-protocol |

---

## 3 · Where the leaders' moats actually are (high confidence)

The category moat is **not in auto-reply, and not in fan-out** — the verified moats are:

1. **Inbound ingest breadth** — Enterpret's integrations page enumerates ~70+ named
   connectors (Zendesk / Intercom / Gong / app stores / Reddit / warehouse
   Snowflake+Census / CSV). The hardest barrier, and attune's biggest current gap
   (webhook + email only).
2. **Adaptive Taxonomy** — Enterpret's core differentiator: "every feedback is
   classified into your taxonomy the moment it lands, zero manual tagging", and the
   taxonomy self-evolves with new feedback — no PM rebuilding tags by hand.
3. **Cross-channel semantic dedup** — collapsing the same underlying issue across
   channels into a single issue **tied to CSAT / retention / revenue**, rather than
   counting duplicates. attune has embedding clustering but it is single-store dedup
   today, not tied to business metrics.

---

## 4 · attune's whitespace and differentiation

- **The cleanest whitespace: LLM auto reply-draft.** Across all 21 verified claims,
  **no benchmarked solution clearly has auto reply-draft**. Commercial leaders lean
  "analysis/insight" (trends for PMs); OSS solutions either have no AI or only do
  summaries. attune does the full `classify → cluster → **draft reply** → fan-out`
  pipeline — this stage is the real differentiator (medium confidence, cross-source
  inference).
- **The only combination nobody else has at once**: `OSS + self-hosted + BYO-LLM +
  end-to-end pipeline + data sovereignty`. Commercial solutions are all hosted-LLM,
  not self-hostable, no BYO-LLM; OSS solutions are either shallow or have no AI.

### ⚠️ Strongest direct competitor: Quackback

Positioning nearly overlaps attune (AGPL, self-hosted, BYO-LLM, dedup + theme
summary), and:

- **broader integrations** (~24 vs attune's few)
- **very fast iteration** (v0.12.4 / 2026-06-19, 79 releases)
- product surface leans voting-board / roadmap, **no auto-reply seen**

attune's three counter-points: **draft-reply auto-reply** · **Apache-2.0 (more
enterprise-friendly than AGPL)** · **multi-protocol BYO-LLM (OpenAI / Anthropic /
Gemini) + strong observability / contract engineering**.

---

## 5 · Strategic implications for the roadmap

1. **Inbound breadth > outbound channel count.** More outbound channels will never
   catch Enterpret; the real gap is **inbound sources**. v0.6's email ingest +
   Adapter SDK (#95 registry-driven) should take priority over piling on more
   outbound channels (#32 still ships, but framed as "rounding out common outbound",
   not a strategic focus).
2. **Hold and amplify draft-reply** — a category-wide whitespace; it should be a
   headline capability told externally, not buried in the pipeline.
3. **Follow up on "adaptive taxonomy"** — #83's off-list module suggestions are
   already the first step toward an Enterpret-style self-learning taxonomy; worth
   deepening along this line.
4. **Sharpen the differentiation narrative vs Quackback**: Apache license + auto-reply
   + multi-protocol LLM.

---

## 6 · Sources (by angle)

**Commercial leaders**
- Enterpret VoC: https://www.enterpret.com/solutions/voice-of-customer (primary)
- Enterpret integrations: https://www.enterpret.com/platform/customer-feedback-integration (primary)
- Enterpret Adaptive Taxonomy: https://www.enterpret.com/platform/adaptive-taxonomy
- Dovetail 2025-10 launch: https://markets.financialcontent.com/stocks/article/bizwire-2025-10-8-dovetail-launches-ai-first-customer-intelligence-platform-to-power-customer-led-product-development (secondary)
- Dovetail Channels docs: https://docs.dovetail.com/help/channels
- Unwrap.ai ML: https://www.unwrap.ai/blog-post/how-unwrap-ai-uses-machine-learning-to-turn-customer-feedback-into-actionable-insights (blog)

**Open-source / self-hosted**
- Quackback: https://github.com/QuackbackIO/quackback (primary) · https://quackback.io/integrations · https://quackback.io/self-hosted
- LINE abc-user-feedback: https://github.com/line/abc-user-feedback (primary)
- Astuto (archived): https://github.com/astuto/astuto (primary)
- Fider: https://openalternative.co/fider (secondary)

---

## 7 · Caveats / not covered

- **Productboard, Canny, Cycle, Savio, Kraftful** had **no surviving evidence** this
  round — do not extrapolate conclusions to them.
- **Pricing tiers / target-customer segmentation** are thinly covered: only point
  facts (Quackback "no per-seat", abc-user-feedback "free OSS"); commercial pricing
  and ICP were not sufficiently substantiated.
- **Astuto was archived 2026-02-08** and is no longer maintained (usable as a
  historical comparison, no future roadmap).
- 4 claims were refuted by adversarial verification (including one that overstated
  Enterpret's taxonomy vs Productboard); "attune's auto-reply is clean whitespace"
  and "BYO-LLM combination differentiation" are cross-source inferences (medium), not
  single-source direct evidence.
