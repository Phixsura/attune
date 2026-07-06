<!-- markdownlint-disable MD013 -->

# Extension plane: registry-backed discovery, risk metadata, and governance

| Field | Value |
| --- | --- |
| **Issue** | N/A (platform maturity subtrack under [#202](https://github.com/Phixsura/attune/issues/202)) |
| **Status** | Implemented |
| **Started** | 2026-07-05 |
| **Related** | [#93](https://github.com/Phixsura/attune/issues/93) (MCP server), [#153](https://github.com/Phixsura/attune/issues/153) (MCP governance), [#168](https://github.com/Phixsura/attune/issues/168) (API / SDK release gates), [platform maturity program](./2026-07-05-platform-maturity-program.md) |

---

## Problem

Attune already has the beginnings of a strong extension story:

- adapter frameworks for inbound and outbound channels;
- registry-driven runtime pieces;
- MCP tools and governance work;
- SDK and public contract work.

What is still missing is the governance plane around those extensibility points.
The current surface actually contains two different populations:

- in-tree core capabilities, such as built-in adapters and built-in MCP tools;
- distributable external extensions, such as optional add-ons or signed
  artifacts.

Those two populations should share discovery and policy vocabulary, but they do
not have the same lifecycle or trust requirements. Without that split, the
registry ends up overfitting either to built-ins or to third-party extensions.
Without that plane, the system can register things, but it cannot yet answer
the platform questions that mature systems answer by default:

- what exactly is registered?
- who owns it?
- how risky is it?
- what data can it touch?
- is it enabled by policy or by accident?
- how does it get deprecated safely?

Grafana, Backstage, and Argo CD all treat extensions as governed runtime
objects. Attune needs the same discipline.

## Goals

- Create one authoritative registry for discoverable extensions.
- Attach risk and ownership metadata to extensions at declaration time.
- Distinguish in-tree core capabilities from distributable external
  extensions.
- Make enablement and disablement policy-aware instead of purely ad hoc.
- Keep a safe path for extension deprecation and replacement.
- Preserve a clear boundary between core runtime and optional add-ons.
- Support attestable or signed distribution where the extension model needs it.

## Non-goals

- Do not build a full public marketplace.
- Do not add arbitrary code sandboxing in the first implementation.
- Do not replace the current adapter architecture.
- Do not require all extensions to be third-party or externally signed on day
  one.

## Proposal

### 1. Add a canonical extension catalog

Every extension should have a single declaration record with:

- canonical name;
- type or kind (`core`, `optional`, or `external`);
- owner;
- risk class;
- data class;
- default enabled state;
- authorization or scope requirements;
- deprecation state.

That record becomes the basis for Console labels, docs, audits, and policy
evaluation.

Core entries are always shipped with Attune and remain discoverable as part of
the core runtime. External entries are optional and may need provenance or
signature checks before they can be enabled.

### 2. Make registration policy-aware

Registration should not merely make something discoverable. It should also tell
the runtime how the extension is supposed to behave:

- read-only vs mutating vs destructive;
- metadata-only vs user-content vs secret-adjacent;
- approved by default vs opt-in only;
- core vs optional vs external.

This does not mean the registry becomes a giant policy engine. It means the
registry carries the facts that the policy engine needs.

### 3. Add a deprecation / replacement path

Extensions need an orderly retirement story:

- announce deprecated state;
- keep compatibility aliases when needed;
- disable by policy before removal;
- make the replacement explicit in docs and UI.

### 4. Add signed or attestable delivery where applicable

For extensions that are distributed artifacts rather than built-in code,
Attune should be able to verify provenance or signature metadata.

The first version can be metadata-only, but the contract should leave room for
signing checks without redesigning the registry.
Core built-ins do not need distribution signatures because they ship with the
Attune release itself; the trust boundary there is the Attune binary and the
repo review process.

## Alternatives considered

### Keep extension metadata informal

Rejected. Informal metadata is exactly how extension systems drift into unsafe
defaults.

### Treat every extension as core code

Rejected. That erases the operational difference between built-ins and
optional integrations.

### Add a marketplace before governance

Rejected. The platform needs a governance model before it needs a catalog UI.

## Risks / Tradeoffs

- Too much metadata can make registration heavy.
- Risk labels can become stale if they are not reviewed.
- Signature or attestation support can create false confidence if it is not
  actually enforced.
- A rigid extension contract can make early experimentation harder.

## Implementation Plan

1. Define the extension catalog schema and registry API.
2. Classify current adapters and tool surfaces as `core` entries in the
   catalog.
3. Surface ownership and risk metadata in Console and docs.
4. Add policy evaluation for enablement / disablement of optional and external
   entries.
5. Add tests for deprecation, aliasing, registration constraints, and core vs
   external behavior.

## Verification

- Registry tests for unique names, owners, and risk metadata.
- Registry tests that built-in core entries do not require external provenance.
- Policy tests for enable / disable decisions.
- Console tests that display extension metadata consistently.
- Documentation checks that the catalog matches the shipped extensions.

## References

- [Grafana provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/)
- [Grafana plugin management](https://grafana.com/docs/grafana/latest/administration/plugin-management/)
- [Grafana plugin signatures](https://grafana.com/docs/grafana/latest/administration/plugin-management/plugin-sign/)
- [Backstage permissions overview](https://backstage.io/docs/permissions/overview/)
- [Backstage permission policy](https://backstage.io/docs/permissions/writing-a-policy/)
- [Argo CD declarative setup](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)
- [Argo CD RBAC](https://argo-cd.readthedocs.io/en/stable/operator-manual/rbac/)
