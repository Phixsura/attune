# Console IA & layout overhaul

| | |
|---|---|
| **Issue** | #144 |
| **Status** | Implemented |
| **Started** | 2026-06-21 CST |
| **Related** | #83 (eval suggested values increased settings density), #93 (MCP server will add more operator/admin surface area), [`console/src/routes/_authed.tsx`](../../../../console/src/routes/_authed.tsx), [`console/src/routes/_authed.settings.tsx`](../../../../console/src/routes/_authed.settings.tsx) |

---

## Problem

The Console has outgrown its original MVP navigation model.

Today, authenticated pages share a single shell with a sticky top bar and a
centered content container ([`_authed.tsx`](../../../../console/src/routes/_authed.tsx)).
That top bar currently carries almost every top-level destination:

- `Feedback`
- `Usage`
- `LLM Usage`
- `LLM Config`
- `Settings`
- `Dead deliveries` (admin only)

Inside `Settings`, the product switches to a second navigation model: a local
left sidebar with 12 sections, all rendered inside one route with a `section=`
search param ([`_authed.settings.tsx`](../../../../console/src/routes/_authed.settings.tsx)).
Those sections currently mix multiple unrelated domains:

- AI classification
- enrichment runtime
- guard policies
- inbound sources
- notify targets
- digest subscription
- API keys
- GDPR
- audit log
- tags
- workflow
- members

This creates five concrete product problems:

1. **Two competing navigation models.** The Console uses a global top nav for
   the product, then a local sidebar only inside `Settings`. This makes the
   mental model inconsistent and hides where "real" product boundaries are.

2. **`Settings` has become a catch-all bucket.** Operator configuration,
   integrations, security/compliance, taxonomy design, and team management are
   all grouped under one label even though they belong to different jobs and
   usage frequencies.

3. **Route ownership is ambiguous.** Some destinations have both a top-level
   route and a pseudo-home inside `Settings` (for example API keys), while other
   features like `Clusters` exist as standalone routes but are mostly discovered
   indirectly from another page.

4. **Responsive behavior is weak.** The top bar is viable only while the number
   of primary destinations is small. As the Console grows, small screens degrade
   into cramped or scroll-heavy navigation instead of an explicit mobile pattern.

5. **The shell does not express hierarchy.** The shared layout provides no
   persistent sidebar, breadcrumb system, section subtitle convention, or width
   tiers for dense management pages. The result is functional but not yet
   production-grade information architecture.

### Code-checked current state

The issue text is directionally right, but the proposal should reconcile it with
the repository's actual current state.

| Concern | Verified reality (2026-06-21) | Why it matters |
|---|---|---|
| Global nav | Implemented in [`topbar.tsx`](../../../../console/src/features/session/components/topbar.tsx) as a flat horizontal list | Any new primary area makes this bar more fragile |
| Shared shell | [`_authed.tsx`](../../../../console/src/routes/_authed.tsx) renders `TopBar` + one centered `max-w-6xl` container | No room for persistent secondary nav or page-width tiers |
| Settings IA | One route with `section` query param and 12 sections | Strong sign that the IA wants to be split, not extended |
| Clusters discoverability | Dedicated route exists at [`/_authed/clusters`](../../../../console/src/routes/_authed.clusters.tsx) | It should become a visible child of Feedback, not remain mostly contextual |
| Theme state | CSS includes `.dark` tokens, but `globals.css` explicitly says dark mode depends on a future `ThemeProvider` | We must either wire a real theme system or remove dormant mode claims |

## Goals / Non-goals

**Goals**

- Define a durable information architecture for the Console before more
  features land.
- Replace the current "top nav for everything, sidebar only in settings" model
  with one consistent app shell.
- Give each destination a clear, singular home while still allowing contextual
  links from related pages.
- Improve discoverability for existing areas, especially `Clusters` and
  admin/operator surfaces currently buried in `Settings`.
- Establish a responsive navigation pattern that scales from desktop to mobile.
- Make a clear product decision on theme support: fully wire it or intentionally
  defer/remove incomplete dark-mode affordances.

**Non-goals**

- Rewriting feature internals, API contracts, or permissions as part of the IA
  work by default.
- Visual redesign for its own sake; the main deliverable is structure, not
  cosmetic novelty.
- New business features. This work reorganizes and clarifies the surfaces that
  already exist.
- Full RBAC redesign. Permission names may be adapted only where the new IA
  makes the old naming actively misleading.

## Industry findings relevant to attune

The external research points to a stable pattern for B2B/admin products like
attune:

1. **Information architecture comes before navigation chrome.** NN/g separates
   IA ("what belongs where") from navigation UI ("how users reach it"). This is
   important because attune's problem is not only the top bar; it is that the
   current grouping no longer reflects the product's real domains.

2. **Growing products favor vertical primary navigation.** NN/g and IBM Carbon
   both recommend vertical navigation for broad or expanding information
   architectures. attune's route set is already past the point where a flat top
   nav is the most resilient choice.

3. **Local navigation should be contextual, not a second global system.**
   Atlassian and NN/g both steer products toward one stable global frame and
   localized in-section navigation, rather than stacking unrelated nav models.

4. **Settings should not absorb every management feature.** Mature products
   such as Stripe and Shopify separate analytics, configuration, integrations,
   administration, and personal/account concerns instead of making `Settings`
   the default home for everything non-operational.

5. **Responsive navigation should change form, not just shrink.** Material
   guidance favors persistent sidebar/drawer patterns on wide screens and drawer
   or similarly explicit navigation on small screens, not horizontal overflow as
   the primary responsive answer.

### Implication for attune

attune should treat the Console as a **growing operator/admin product**, not as
an MVP with a few extra tabs. The most durable pattern is:

- top bar for identity and utilities
- left sidebar for primary navigation
- contextual sub-navigation inside major sections when needed
- one canonical home per destination

## Proposal

### 1. Adopt a single app-shell model

Replace the current shell with:

- a slim top bar for brand, tenant, global search/future command palette,
  notifications/future utilities, and account menu
- a persistent left sidebar on desktop for primary navigation
- a drawer/sheet version of that sidebar on mobile
- a content area that supports width tiers (`default`, `wide`, `full`) based on
  page density rather than one global `max-w-6xl`

This gives the Console one stable navigation grammar instead of a top-level
model plus a special-case `Settings` sub-app.

### 2. Reframe the primary IA around product domains

Proposed top-level groups:

| Group | Routes / surfaces | Why |
|---|---|---|
| **Feedback** | `Feedback`, `Clusters` | Core operator workflow; clusters is not "advanced settings", it is part of triage/discovery |
| **Analytics** | `Usage`, `LLM Usage` | Both are reporting/measurement surfaces |
| **Configuration** | classification, tags, workflow, enrichment runtime, LLM config | Defines how attune behaves |
| **Integrations** | inbound sources, notify targets, API keys, digest subscription | Connects attune to inbound/outbound systems and access surfaces |
| **Administration** | members, audit log, GDPR, guard policies, dead deliveries | Security, governance, operations, and admin-only recovery surfaces |

This structure intentionally removes `Settings` as the single umbrella for
every non-feedback function.

### 3. Give each destination one canonical home

Apply Shopify Polaris's "one home, many doors" principle:

- `API keys` has one canonical location under `Integrations`
- `Clusters` has one canonical location under `Feedback`
- `Dead deliveries` has one canonical location under `Administration`
- contextual cards, shortcuts, and links can still point to these destinations
  from related pages

We should avoid a state where a feature is both a top-level page and also "the
real thing inside Settings".

### 4. Use section-local navigation instead of a mega Settings page

`Settings` should not remain a single route with `section=<12 options>`.

Recommended transition:

- turn current settings sections into real route segments grouped by domain
- keep thin index pages for each major area where useful
- reserve local side navigation for sections that genuinely have multiple
  siblings, for example:
  - `Configuration`
  - `Integrations`
  - `Administration`

Illustrative route tree:

```text
/feedback
/feedback/clusters

/analytics/usage
/analytics/llm-usage

/configuration/classification
/configuration/tags
/configuration/workflow
/configuration/enrichment-runtime
/configuration/llm

/integrations/inbound-sources
/integrations/notify-targets
/integrations/api-keys
/integrations/digests

/administration/members
/administration/audit-log
/administration/gdpr
/administration/guard-policies
/administration/dead-deliveries
```

This is a target IA, not a claim that all route names must match exactly. We
can preserve backward-compatible redirects while converging on clearer homes.

### 5. Establish explicit page-shell conventions

Define and reuse a small app-shell contract:

- **Primary nav**: persistent sidebar or mobile drawer
- **Page header**: title + subtitle + optional actions
- **Breadcrumbs**: only for deep pages, not every screen
- **Secondary nav**: allowed only within a clear section domain
- **Width tiers**:
  - `default` for forms and ordinary lists
  - `wide` for dense tables and analytics
  - `full` for highly interactive surfaces like feedback triage

This avoids each route improvising layout rules independently.

### 6. Make theme support an explicit decision

Current CSS token setup suggests dark mode support, but the product does not yet
ship a complete theme system. This proposal recommends choosing one of two
paths during implementation:

1. **Complete it**: add `ThemeProvider`, persisted preference, a toggle entry in
   the shell, and QA all major pages in both themes.
2. **Defer it honestly**: remove dormant language and styles that imply a
   shipped dark mode until we are ready to support it fully.

Half-enabled theme infrastructure increases maintenance cost and confuses the
layout work.

### 7. Migration strategy

This should land incrementally, not as a single unstable big bang.

Suggested sequence:

1. Build the new shell primitives behind existing routes.
2. Move primary navigation from top bar into sidebar/drawer.
3. Split the current `Settings` route into domain homes and leaf routes.
4. Add redirects from old URLs/search-param forms to new canonical routes.
5. Remove duplicate entry points and dead route affordances.
6. Finalize theme decision and responsive QA.

## Alternatives considered

1. **Keep the top bar and simply reorder labels.**
   Rejected: this does not solve the main structural problem. It may delay pain
   for one or two releases, but the product will keep growing and the same IA
   debt will reappear quickly.

2. **Keep `Settings`, but group its sidebar more neatly.**
   Rejected: this improves readability inside one page but preserves the deeper
   problem that unrelated domains all share the same parent and URL semantics.

3. **Use a global top bar plus a global sidebar at the same time.**
   Rejected: this tends to create two primary navigation systems competing for
   attention. The top bar should become utility-first, not remain a second main
   nav.

4. **Do a full visual redesign before deciding IA.**
   Rejected: this reverses the right order. The structure needs to be settled
   first so visual work supports the product model instead of papering over it.

## Risks / tradeoffs

- **Route churn:** changed URLs can break bookmarks and operator muscle memory.
  Mitigation: redirects, clear release notes, and stable canonical paths.

- **Permission naming drift:** today's permission names are heavily
  `settings:*`-oriented. If new IA names diverge too much, we may either keep
  legacy permission names temporarily or introduce aliases and migrate in a
  controlled pass.

- **Partial migration confusion:** if some sections move while others still live
  in the old Settings route, the interim state can feel inconsistent. Mitigation:
  migrate by domain, not one random page at a time.

- **Shell abstraction creep:** too much app-shell generalization can slow down
  delivery. The goal is a small, opinionated shell contract, not a design-system
  framework project.

## Implementation plan

1. Accept this proposal and align on the target IA groups.
2. Audit current routes, links, and permission dependencies against the target
   structure.
3. Implement a new authenticated shell with:
   - desktop sidebar
   - mobile drawer
   - utility top bar
   - page-width tiers
4. Migrate global destinations into the new primary nav.
5. Split `Settings` into real route domains and leaf pages.
6. Add redirects from:
   - `/settings?section=...`
   - existing duplicate top-level pages whose canonical home changes
7. Decide and implement the theme path: complete or defer/remove.
8. Update tests:
   - route tests
   - navigation visibility tests by role
   - responsive smoke coverage where practical
9. Add `[Unreleased]` changelog entry in the implementing PR.

## Verification

- Route inventory after migration has:
  - no duplicate canonical homes
  - no unreachable operator surfaces
  - no settings-only pseudo-routes for globally significant features
- Desktop UI exposes one primary navigation model.
- Mobile UI uses an explicit drawer/sheet navigation pattern.
- `Clusters` is directly discoverable from the primary Feedback section.
- `Usage` and `LLM Usage` are grouped under analytics/reporting.
- `API keys`, `Inbound sources`, and `Notify targets` are no longer buried
  inside one giant Settings page.
- Theme state is either fully wired and tested, or intentionally removed as a
  shipped claim.
- Console checks stay green:
  - `pnpm tsc -b --noEmit`
  - `pnpm biome check`
  - `pnpm vitest run`
  - `pnpm arch`

## References

- NN/g — Information architecture vs. navigation:
  https://www.nngroup.com/articles/ia-vs-navigation/
- NN/g — Local navigation:
  https://www.nngroup.com/articles/local-navigation/
- NN/g — Vertical navigation:
  https://www.nngroup.com/videos/vertical-navigation/
- IBM Carbon — UI shell left panel usage:
  https://carbondesignsystem.com/components/UI-shell-left-panel/usage/
- Shopify Polaris — Information architecture:
  https://polaris-react.shopify.com/foundations/information-architecture
- Stripe Dashboard basics:
  https://docs.stripe.com/dashboard/basics
- Atlassian app navigation guidance:
  https://developer.atlassian.com/cloud/jira/platform/navigation/
- Android responsive navigation guidance:
  https://developer.android.com/develop/ui/views/layout/build-responsive-navigation
- Material Design 3 — Navigation drawer:
  https://m3.material.io/components/navigation-drawer/overview
