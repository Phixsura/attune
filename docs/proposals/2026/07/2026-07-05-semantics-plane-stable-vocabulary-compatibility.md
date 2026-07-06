<!-- markdownlint-disable MD013 -->

# Semantics plane: stable vocabulary and compatibility policy

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#149](https://github.com/Phixsura/attune/issues/149) (preflight), [#156](https://github.com/Phixsura/attune/issues/156) (SLO burn-rate / tenant impact), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune already emits metrics, docs, and control-plane objects. The missing
piece is a single semantic contract that keeps those names aligned.

OpenTelemetry's core lesson is that telemetry stays durable when its vocabulary
is stable. Mature platforms carry that lesson beyond metrics into resources,
deployments, owners, and release state.

Without a shared vocabulary, the same concept can appear under multiple names:

- deployment environment;
- runtime profile;
- service name;
- owner/team;
- policy mode;
- lifecycle state;
- extension risk.

That makes observability, docs, and operator workflows harder to keep in sync.

## Goals

- Define a stable vocabulary for the platform's core semantics.
- Keep telemetry labels and control-plane objects aligned to that vocabulary.
- Publish compatibility rules for config, APIs, SDKs, and metadata.
- Prevent silent renames of important runtime concepts.
- Keep the vocabulary small enough to stay usable.

## Non-goals

- Do not localize stable identifiers.
- Do not create an overly large taxonomy.
- Do not rewrite OpenTelemetry conventions.
- Do not force every internal term into a public contract.

## Proposal

### 1. Define a canonical glossary

The repo should have a small glossary for the concepts that repeat across
runtime, docs, and telemetry:

- environment;
- profile;
- service;
- owner;
- policy mode;
- release state;
- lifecycle state;
- risk class.

### 2. Tie emitted labels to the glossary

Labels on metrics, traces, dashboards, and admin objects should map back to the
same glossary terms.

That makes it easier to validate:

- whether a label is stable;
- whether a name can be renamed safely;
- whether a new concept needs a new term or just a new value.

### 3. Publish compatibility rules

Attune should state which semantic changes are:

- additive;
- breaking;
- deprecated-with-shim;
- rename-only-with-alias;
- supported only behind a migration step.

### 4. Keep docs and UI terminology aligned

The Console, deployment docs, and proposal docs should reuse the same words for
the same things.

That is how the platform avoids the drift between "what the docs say" and "what
the runtime actually means."

## Alternatives considered

### Let each subsystem name things independently

Rejected. This is the easiest path to inconsistent operator expectations.

### Solve semantics only in telemetry

Rejected. The contract has to cover docs and control-plane objects too.

### Make the glossary too broad too early

Rejected. The vocabulary should stay small and intentionally curated.

## Risks / Tradeoffs

- A glossary can become shelfware if nobody enforces it.
- Overly strict semantic policies can slow healthy iteration.
- A weak glossary can look official while still being too vague to use.

## Implementation Plan

1. Publish the canonical glossary.
2. Map current runtime and telemetry terms to the glossary.
3. Add compatibility rules for key semantic changes.
4. Add lightweight validation where labels or names are generated.
5. Review docs and UI wording against the glossary.

## Verification

- Tests for emitted label stability where practical.
- Documentation checks for glossary usage.
- Review of renamed or newly introduced semantics against the compatibility
  policy.

## References

- [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/specs/semconv/)
- [OpenTelemetry resource conventions](https://opentelemetry.io/docs/specs/semconv/resource/)
- [OpenTelemetry service conventions](https://opentelemetry.io/docs/specs/semconv/resource/service/)
- [OpenTelemetry deployment environment conventions](https://opentelemetry.io/docs/specs/semconv/resource/deployment-environment/)
