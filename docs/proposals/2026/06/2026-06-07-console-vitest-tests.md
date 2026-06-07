# Console Vitest unit tests — infrastructure + first wave

| Field    | Value |
|----------|-------|
| Issue    | #13 |
| Status   | **Implemented** |
| Started  | 2026-06-07 |
| Updated  | 2026-06-07 — second scope expansion: `dimensions-editor.tsx` test coverage folded back in per user direction, supersedes the original "deferred to follow-up #88" decision. PR now covers it directly; #88 closed as merged.<br>2026-06-08 — Implemented. Final shape: 17 test files, 79 tests, ~3120 net LOC. Per-file forward-ratchet thresholds on `api-client.ts` / `i18n-resolve.ts` / `get-me.ts` all green. `fileParallelism: false` set in `vite.config.ts` because `setup-tests.ts` patches process-wide globals (MSW server, navigator.clipboard, Element prototype shims) that race across parallel files. |
| Related  | #1 (vitest wired with `--passWithNoTests` as a placeholder in PR #11) · #19 (feature-based console layout, dependency-cruiser rules — testing layout must coexist with the existing `shared → features → app` arrows) · CLAUDE.md §9 (AI assistants must add tests for new behavior) · #88 (originally filed as the `dimensions-editor` follow-up; now closed-as-merged) |

> attune is Apache-2.0 OSS and proposals are the canonical English record so external contributors can read them cold.

---

## TL;DR (60-second pitch)

- **Decision.** Land console testing infrastructure + a first wave of tests in **one PR** that satisfies all three issue-#13 acceptance criteria. The infra mirrors `alan2207/bulletproof-react`'s `react-vite` app — the same repo that already serves as our architecture north-star (see [console/.dependency-cruiser.cjs](../../../../console/.dependency-cruiser.cjs) `Why this tool, not Biome native? — bulletproof-react §unidirectional`).
- **Network-boundary mock with MSW v2**, not `vi.mock`. This follows the unambiguous consensus of the three authorities for this stack — TkDodo (TanStack Query maintainer, *Testing React Query*), Kent C. Dodds (Testing Library author, *Stop mocking fetch*), and bulletproof-react itself — and lets one mechanism cover api-client, hooks, components, and route guards.
- **jsdom** as the single Vitest environment (no `projects` split). Pure-logic tests run fine under jsdom; jsdom gives us a real `location` so the api-client's relative `/fb/v1/...` paths resolve naturally for MSW interception. Vitest 4 removed `environmentMatchGlobs` — `projects` would be the only alternative, and we don't need its complexity.
- **First wave covers** the highest-ROI surfaces named in the issue: the `api()` typed-fetch wrapper + CSRF state, the i18n resolver, `meQuery`'s CSRF side effect, three api-keys / notify-targets dialogs (incl. the **sparse-PATCH diff** logic that is the single highest-value piece of UI code in the console), and the `_authed` route guard's `beforeLoad` → `redirect()` to `/login`. The `--passWithNoTests` flag goes away in the same PR.
- **No new runtime dependencies.** Five test-only devDependencies: `jsdom`, `@testing-library/jest-dom`, `@testing-library/user-event`, `msw` (v2), `@vitest/coverage-v8`.

---

## 1 · Problem

The console SPA has zero tests. [console/package.json](../../../../console/package.json) already lists `vitest@^4.1.0` and `@testing-library/react@^16.3.0` in devDependencies, and [.github/workflows/ci.yml:240](../../../../.github/workflows/ci.yml#L240) runs `pnpm vitest run --passWithNoTests` — so CI reports green, but that green is a placeholder. None of the test infrastructure required to actually exercise React components exists in the repository:

- **No DOM environment.** Neither `jsdom` nor `happy-dom` is in `console/node_modules` (verified by direct `ls`; they are not transitive dependencies of anything else). The `vite.config.ts` has no `test` block at all — so Vitest currently defaults to the `node` environment, which cannot render any component that touches `document`.
- **No assertion or interaction helpers.** `@testing-library/jest-dom` and `@testing-library/user-event` are not installed, so even a smoke test cannot use `toBeInTheDocument()` or simulate a click on a Radix dialog.
- **No setup file.** No place to mount jest-dom matchers, manage MSW lifecycle, or shim the DOM APIs Radix relies on but jsdom does not implement (`ResizeObserver`, `matchMedia`, `Element.scrollIntoView`, `Element.hasPointerCapture`).
- **No mocked backend.** All hooks and route loaders ultimately call `api()` against `/fb/v1/console/*`. There is no fixture for that surface.

Concretely, the SPA's most security-relevant client-side code is the api-client at [console/src/lib/api-client.ts](../../../../console/src/lib/api-client.ts): a 81-line typed-fetch wrapper that holds CSRF state in a module-level closure, injects `X-CSRF-Token` for every non-GET, parses the server's unified `{code, message, requestId}` error envelope, and feeds every other API call in the app. It is untested. So is the `_authed` route guard at [console/src/routes/_authed.tsx:12-21](../../../../console/src/routes/_authed.tsx#L12-L21), which decides — for every authenticated page in the console — whether to `redirect()` to `/login` based on whether `meQuery()` succeeded. So is the sparse-PATCH diff logic in [console/src/features/notify-targets/components/edit-dialog.tsx:65-79](../../../../console/src/features/notify-targets/components/edit-dialog.tsx#L65-L79), which decides which fields to send to the backend and includes a non-obvious "secret cleared" bit that distinguishes "user did not touch the secret" from "user explicitly cleared it".

A regression in any of these three places would not be caught by `tsc`, `biome check`, or `dependency-cruiser`. The cost of catching them in production (silently broken auth, wrong PATCH payloads quietly overwriting tenant config) is high relative to the cost of writing the tests.

Two non-test cleanup items surface in the same scope and land in this PR (see §4-L): **(a)** the issue refers to a "generated API-client wrapper", but the api-client is hand-written — only `src/proto/**` is generated. The `gen:api` script reference and `src/api/types.ts` line in [console/.gitignore](../../../../console/.gitignore) (plus a matching `biome.json` override include) belong to an abandoned openapi-typescript plan — verified: no `gen:api` script exists in `package.json`, and no `src/api/types.ts` file exists in the tree. **(b)** the issue mentions forms as if they were managed by `react-hook-form` / `zod`; `react-hook-form`, `zod`, and `@hookform/resolvers` are all in `dependencies` but completely unused (verified by grep across `console/src/`). The forms are plain controlled inputs with `useState`; the interesting logic to test is the sparse-PATCH diff, not form validation.

---

## 2 · Goals

- Satisfy issue #13's three acceptance bullets in one PR: real tests run under `pnpm vitest run`, key components / hooks / api-client are covered, `--passWithNoTests` is removed from CI.
- Establish a test layout (`src/testing/`) and a single Vitest configuration that supports both pure-logic and DOM-touching tests, so feature authors do not have to pick an environment or wire a setup per file.
- Mock the backend at the **network boundary** (MSW), so the same mocks serve unit tests of the api-client, hook tests, component tests, and route-guard tests — and so tests survive internal refactors of the api-client.
- **Close issue #13 completely in one PR**: cover every surface implied by its acceptance bullets — including the originally-deferred feedback infinite query, feedback detail/stats, settings (`EnrichConfig`) read/write/preview hooks, small auxiliary components (`i18n-input`, `dim-stats-bars`, `detail-sheet`), **and the 380-LOC `dimensions-editor.tsx`** (identity tracking, Name/Value readOnly lock for persisted rows, urgent_set toggle, removeTaxonomy-syncs-urgentSet, add/remove flows) — plus two non-test cleanups (delete unused `react-hook-form`/`zod`/`@hookform/resolvers`; remove dead `gen:api`/`src/api/types.ts` references). Per direction after benchmarking & decision review and a second scope-expansion confirmation: thoroughness in one well-tested PR beats fragmented follow-ups. **Issue #88 (originally filed as the `dimensions-editor` follow-up) is closed-as-merged at PR open time.**
- Add code coverage reporting (`@vitest/coverage-v8`) **with a soft forward-ratchet threshold** on the highest-trust paths (api-client, i18n-resolve, `meQuery`), so future regressions on those surfaces fail CI rather than slip silently. The `console/coverage/` path is already present in the root [.gitignore:29](../../../../.gitignore#L29), indicating prior intent.
- **Forward-friendly MSW handler set**: the default `mocks/handlers.ts` covers every current `/fb/v1/console/*` endpoint (see §4-G's inventory), not only those exercised by the first wave. `onUnhandledRequest: 'error'` then makes "new endpoint added without a mock" fail loud in the very first test that touches it.
- Hold the line on existing CI gates: `pnpm tsc -b --noEmit`, `pnpm biome check src`, and `pnpm arch` (dependency-cruiser) must remain green with the new files in place.

## 3 · Non-goals

- **Not** introducing E2E tests (Playwright / Cypress). The issue scope is `vitest` unit/integration; E2E is a separate conversation with its own runner and CI dimension.
- **Not** test-driven implementation of new product features. This PR adds tests against existing code only.
- **Not** adopting `react-hook-form` / `zod` for the dialogs. Removing the unused dependencies is in scope of a separate cleanup; reaching 100% coverage of validation logic that does not exist would be busywork.
- **Not** changing the api-client's runtime behavior. If a test would force a behavior change to the api-client, that is a separate proposal.
- **Not** mocking i18n strings to English. Tests assert on stable identifiers (test ids, role + name where possible, exported callbacks) so the existing `zh-CN`-only i18n bundle is the source of truth and we don't need a translation harness.
- **Not** testing the drag/drop or row-reordering paths of `dimensions-editor.tsx` (the current implementation has none — when reordering UI lands later, that test PR comes with it). The 380-LOC editor itself **is** in scope: identity tracking, Name/Value readOnly lock, add/remove dim+taxonomy, urgent_set toggle, removeTaxonomy syncing urgent_set.
- **Not** testing visually-driven components without business logic (`features/usage/components/{bar-chart,sparkline}.tsx`, `components/brand/*`).
- **Not** setting global coverage thresholds. The threshold is applied only to the three highest-trust paths in §4-B as a forward ratchet — not as a project-wide gate. Project-wide gates wait for a measured baseline.

---

## 4 · Proposal

### A · New devDependencies (five, all dev-only)

| Package | Version | Why we need it | Why we can't avoid it |
|---|---|---|---|
| `jsdom` | `^25` (latest) | DOM environment for Vitest | Without this, Vitest runs in `node` env and any component test that touches `document` throws. |
| `@testing-library/jest-dom` | `^6` | `toBeInTheDocument`, `toBeDisabled`, etc. | Native `expect` has no DOM-aware matchers. |
| `@testing-library/user-event` | `^14` | High-fidelity simulated user interactions | `fireEvent` does not exercise the full pointer/keyboard sequence Radix relies on. |
| `msw` | `^2` (currently 2.14.6) | Network-boundary backend mock | See §4-G for full rationale; in short, the consensus mock layer for `fetch` + TanStack Query stacks. |
| `@vitest/coverage-v8` | matched to vitest 4 | Coverage report | Coverage path is already gitignored — the intent is documented; this is just turning it on. |

Total install adds ~30 MB to `console/node_modules/` (rough estimate; final number measured by the PR's `pnpm-lock.yaml` diff). All are dev-only and never enter the production bundle (verified later in the PR by `pnpm exec vite build` size diff being zero).

This is five packages where attune's CLAUDE.md §8 dependency baseline asks for justification. The justification per CLAUDE.md is "bundle cost, activity, alternatives considered" — bundle cost is zero (dev-only), activity is healthy for all five (MSW v2 stable; jsdom maintained by jsdom org under tobie/jeffcarp; testing-library packages by testing-library/Kent C. Dodds), and alternatives are enumerated in §5.

### B · `vite.config.ts` — add `test` block

```ts
// At the top of vite.config.ts, alongside existing imports
/// <reference types="vitest" />

// Inside the existing defineConfig({...}) call, alongside `plugins`, `resolve`, `server`:
test: {
  globals: true,                      // describe/it/expect/vi auto-imported
  environment: 'jsdom',
  setupFiles: ['./src/testing/setup-tests.ts'],
  // Exclude generated code & test-only utilities from coverage.
  coverage: {
    provider: 'v8',
    include: ['src/**'],
    exclude: [
      'src/proto/**',                 // generated by ts-proto
      'src/routeTree.gen.ts',         // generated by TanStack Router plugin
      'src/testing/**',               // the dummies live here
      '**/*.test.{ts,tsx}',
      'src/main.tsx',                 // composition root, exercised by build
    ],
    reporter: ['text', 'html'],       // text for CI logs, html for local
    // Soft forward ratchet — only the highest-trust paths gated for now.
    // Per-file thresholds (no `perFile: true`) apply each metric to each
    // listed file independently. A regression on one of these surfaces
    // — the api-client's CSRF/error envelope, the i18n resolver, or
    // meQuery's CSRF side effect — should fail CI loudly. The remaining
    // surfaces are measured but ungated; expanding the gate as the
    // suite stabilises is a follow-up decision.
    thresholds: {
      'src/lib/api-client.ts':         { lines: 90, statements: 90, branches: 80, functions: 90 },
      'src/lib/i18n-resolve.ts':       { lines: 90, statements: 90, branches: 80, functions: 90 },
      'src/features/session/api/get-me.ts': { lines: 90, statements: 90 },
    },
  },
},
```

Rationale, one decision at a time:

- **Single environment (`jsdom`) instead of `projects`.** Vitest 4 removed `environmentMatchGlobs`; the only Vitest-native multi-env mechanism left is `test.projects` (web search verified, vitest.dev/guide/migration). Splitting infra adds complexity without buying anything — pure-logic tests run perfectly well under jsdom (slightly slower, but the suite size makes this immeasurable), and jsdom gives the api-client tests a real `location.origin` so its relative path `/fb/v1/...` resolves to an absolute URL that MSW can intercept. In `node` env we'd have to either configure a base URL or use a custom resolver.
- **`globals: true`.** Avoids an `import { describe, it, expect, vi } from 'vitest'` line at the top of every test file. This is the bulletproof-react choice. The cost is the tsconfig fix below.
- **Coverage `include: ['src/**']`** — explicit, so adding a new feature directory automatically gets measured. **`exclude`** lists are the surface area we deliberately don't measure: generated code (`src/proto/**`, `src/routeTree.gen.ts`) and the test utilities themselves.
- **`reporter: ['text', 'html']`** — `text` shows up in CI logs (no extra GitHub Action needed); `html` lands under `console/coverage/html/` for local inspection. The path is already gitignored at the repo root.

### C · `tsconfig.app.json` — register Vitest globals

Required, not optional. The CI gate `pnpm tsc -b --noEmit` runs after `pnpm exec vite build`; with `globals: true` but no `types` entry, every `it()` / `vi.fn()` would be flagged as an undefined name and the typecheck would fail. Verified by reading [console/tsconfig.app.json](../../../../console/tsconfig.app.json): `include: ["src"]`, `strict: true`, `noUnusedLocals: true`.

```diff
 {
   "compilerOptions": {
     "target": "ES2022",
     "lib": ["ES2023", "DOM", "DOM.Iterable"],
+    "types": ["vitest/globals", "@testing-library/jest-dom"],
     ...
   },
   "include": ["src"]
 }
```

(The `@testing-library/jest-dom` types entry lets `expect(x).toBeInTheDocument()` typecheck under strict mode.)

### D · `biome.json` — relax two rules for test files only

[console/biome.json](../../../../console/biome.json) currently lints all of `src/**/*.{ts,tsx}` with `noExplicitAny: error` and full a11y. Test files legitimately need `as unknown as Foo` casts to mock proto types, and Radix dialogs render `role="dialog"` / `role="alertdialog"` — the a11y lints don't fire there in our usage, but a future test for a custom modal might need `aria-label` shortcuts.

```diff
 "overrides": [
+  {
+    "includes": ["src/**/*.test.{ts,tsx}", "src/testing/**"],
+    "linter": {
+      "rules": {
+        "suspicious": { "noExplicitAny": "off" },
+        "style": { "useImportType": "off" }
+      }
+    }
+  },
   { "includes": ["src/api/types.ts", "src/routeTree.gen.ts", "src/proto/**"], ... }
 ]
```

We turn off `useImportType` for test files specifically because `vi.mocked()` and similar APIs need the value-level import.

As part of §4-L's cleanup, the existing override's `includes` array drops the dead `"src/api/types.ts"` entry (no such file has ever existed; the `gen:api` script it referenced is not in `package.json`). The override's other two entries (`src/routeTree.gen.ts`, `src/proto/**`) stay.

### E · Directory layout (mirrors bulletproof-react / `apps/react-vite`)

```
console/src/
  testing/
    setup-tests.ts          # jest-dom matchers; MSW lifecycle; Radix/clipboard shims
    test-utils.tsx          # renderWithProviders() — i18n + fresh QueryClient(retry:false)
    router-utils.tsx        # createTestRouter() for route-guard tests
    mocks/
      server.ts             # setupServer(...handlers)
      handlers.ts           # default /fb/v1/console/* handlers; override per-test with server.use()
  features/
    api-keys/
      api/
        create-api-key.ts
        create-api-key.test.ts        ← NEW (co-located, mirrors source)
      components/
        dialogs.tsx
        dialogs.test.tsx              ← NEW
    notify-targets/components/
        edit-dialog.tsx
        edit-dialog.test.tsx          ← NEW
  lib/
    api-client.ts
    api-client.test.ts                ← NEW
    i18n-resolve.ts
    i18n-resolve.test.ts              ← NEW
  routes/
    _authed.tsx
    _authed.test.tsx                  ← NEW
```

**Why co-located tests, not a parallel `__tests__/` tree.** Feature cohesion (CLAUDE.md §5: "files are grouped into feature subpackages"). Moving the test file with the source on a refactor is automatic. The cost — a slightly larger `find` listing per feature — is negligible against the win of "tests don't get lost in a refactor".

**Why `src/testing/` and not `src/lib/testing/`.** `src/testing/` is the bulletproof-react convention and crucially it must not be subject to the `shared → features → app` arrow enforced by [console/.dependency-cruiser.cjs](../../../../console/.dependency-cruiser.cjs). The cruiser config has three forbidden rules — `no-cross-feature`, `shared-no-up`, `features-no-app` — and `src/testing/` matches none of their `from` patterns (`^src/features/…`, `^src/(components|lib|proto)/…`). Verified: `pnpm arch` will be silent on the new directory.

### F · `src/testing/setup-tests.ts` — what it does

```ts
import '@testing-library/jest-dom/vitest'
import { server } from './mocks/server'

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

beforeEach(() => {
  // Radix Dialog/Select need APIs jsdom doesn't implement.
  if (!('ResizeObserver' in window)) {
    vi.stubGlobal(
      'ResizeObserver',
      vi.fn(() => ({ observe: vi.fn(), unobserve: vi.fn(), disconnect: vi.fn() })),
    )
  }
  if (!window.matchMedia) {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false, media: query, onchange: null,
        addListener: vi.fn(), removeListener: vi.fn(),
        addEventListener: vi.fn(), removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    )
  }
  Element.prototype.scrollIntoView = vi.fn()
  // jsdom doesn't implement these PointerEvent affordances; Radix uses them
  // to manage focus/scroll inside dialogs and dropdowns.
  if (!Element.prototype.hasPointerCapture) Element.prototype.hasPointerCapture = vi.fn()
  if (!Element.prototype.releasePointerCapture) Element.prototype.releasePointerCapture = vi.fn()
  if (!Element.prototype.setPointerCapture) Element.prototype.setPointerCapture = vi.fn()

  // SecretKeyDialog calls navigator.clipboard.writeText(); jsdom has no clipboard.
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } })
})
```

`onUnhandledRequest: 'error'` is critical — it catches a "I forgot to register a handler" bug as a clear test failure instead of letting the test pass with `undefined` data and confusing downstream assertions. This is TkDodo's explicit recommendation in *Testing React Query*.

### G · Why MSW, not `vi.mock('@/lib/api-client')` (the key architectural call)

The instinct for a thin api-client is "just `vi.mock` the module". We rejected that after benchmarking against the three authorities for this stack:

- **TkDodo (TanStack Query maintainer), *Testing React Query*.** "Mock at the network level… don't mock the whole hook." Explicit: per-test isolated `QueryClient`, `retry: false`, MSW with `onUnhandledRequest: 'error'`.
- **Kent C. Dodds (Testing Library author), *Stop mocking fetch*.** Mocking fetch or the API client re-implements the backend in every test; tests become brittle to refactors of how the client serializes requests. MSW intercepts at the wire and returns indistinguishable-from-real responses.
- **bulletproof-react, `apps/react-vite`.** Uses MSW v2 with `setupServer`, lifecycle in `setup-tests.ts`, handlers per resource. We fetched the actual source to confirm.

The technical payoff for *attune specifically* is bigger than these generic arguments suggest, because the api-client has non-trivial wire-format responsibilities:

- It injects `X-CSRF-Token` only for non-GET — testing that with `vi.mock('@/lib/api-client')` skips the very contract we want to assert.
- It parses the server's `{code, message, requestId}` error envelope — same problem.
- The `meQuery` side-effect that writes the CSRF token into the api-client's module-level state ([console/src/features/session/api/get-me.ts:20](../../../../console/src/features/session/api/get-me.ts#L20)) is invisible if the api-client is mocked.

With MSW, **one** mock layer covers api-client unit tests, hook tests, component tests, and route-guard tests. Hand-stubbing `fetch` to test the api-client and then `vi.mock`ing the api-client to test hooks would be two parallel testing dialects in the same suite.

The single remaining case where direct `fetch` stubbing wins is testing **non-HTTP** failures — `fetch` rejecting with a `TypeError` (DNS error), `AbortError` from a signal. For those, two short test cases in `api-client.test.ts` will use `vi.stubGlobal('fetch', vi.fn().mockRejectedValue(...))`. This is a narrow exception, not a parallel mechanism.

#### MSW handler inventory — forward-friendly coverage of `/fb/v1/console/*`

Per user direction, the default `mocks/handlers.ts` covers **every** current console endpoint, not only those exercised by this wave's tests. With `onUnhandledRequest: 'error'`, the first test that touches an unmocked endpoint fails loud — preventing the "I added a query for a new endpoint and the test silently got `undefined`" failure mode.

| Endpoint | Method(s) | Default behavior |
|---|---|---|
| `/fb/v1/console/me` | GET | returns `{ tenant, user, csrfToken }` minimal fixture |
| `/fb/v1/console/install/start` | GET | 302 with `Location` echo for redirect probes |
| `/fb/v1/console/api-keys` | GET / POST | list + create |
| `/fb/v1/console/api-keys/:id/revoke` | POST | revoke |
| `/fb/v1/console/notify-targets` | GET / POST | list + create |
| `/fb/v1/console/notify-targets/:id` | PATCH / DELETE | update + delete |
| `/fb/v1/console/notify-targets/:id/test` | POST | test trigger |
| `/fb/v1/console/enrich-config` | GET / PUT | read + update tenant `EnrichConfig` |
| `/fb/v1/console/enrich-config/preview` | POST | preview prompt rendering |
| `/fb/v1/console/feedback` | GET | list (cursor pagination via `?cursor=` param; `?q=` + per-dim attr filters + `?urgent=`) |
| `/fb/v1/console/feedback/:id` | GET | detail |
| `/fb/v1/console/feedback/stats` | GET | dim aggregates |

Per-test overrides — error envelopes, paginated cursors, empty lists, slow responses — use `server.use(http.<verb>(path, handler))` inside the test body; they are reset by `afterEach(server.resetHandlers)`. The default fixtures return minimal well-typed `ts-proto` shapes; ~150 LOC total for `handlers.ts`.

### H · `src/testing/test-utils.tsx` — `renderWithProviders`

```ts
export function renderWithProviders(ui: ReactElement, opts?: { queryClient?: QueryClient }) {
  const queryClient = opts?.queryClient ?? new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  return {
    queryClient,
    user: userEvent.setup(),
    ...render(ui, {
      wrapper: ({ children }) => (
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
        </I18nextProvider>
      ),
    }),
  }
}
// Re-export RTL + userEvent so tests have one import source.
export * from '@testing-library/react'
export { default as userEvent } from '@testing-library/user-event'
```

A fresh `QueryClient` per test ensures cache state never leaks across tests (TkDodo's first rule). `retry: false` is non-negotiable — without it, a test of an error path waits ~3s for the default retry storm before asserting the error state.

### I · `src/testing/router-utils.tsx` — `createTestRouter`

For the `_authed` route guard. TanStack Router's official testing pattern (search-verified, tanstack.com/router/.../setup-testing): build a code-based test router with `createRouter` + `createMemoryHistory`, pass `routerContext: { queryClient }`, render `<RouterProvider router={...} />`. We will write the helper as code-based (not file-based) because the production routes are file-based and we want test routes to be explicit about which guard they're exercising.

### J · Test surface inventory (the actual tests)

Roughly ~50 cases across ~14 test files; ~1000 net LOC of test code. The tiers are organized by what the test needs, not by importance.

**Tier 1 — pure logic, queries, mutations (high signal per LOC):**

*lib (the universal foundation)*

- **`src/lib/api-client.test.ts`** (~10 cases, all via MSW + the small fetch-stub exception):
  - GET request omits `X-CSRF-Token` even when token is set.
  - POST/PUT/PATCH/DELETE include `X-CSRF-Token` when set; omit when null.
  - 204 returns `undefined` regardless of body.
  - 2xx with JSON body returns the parsed body typed.
  - 2xx with empty/non-JSON body returns `null`-shaped value (consistent with `parsed: unknown = null`).
  - 4xx/5xx with `{code, message, requestId}` envelope throws `ApiError` with all four fields populated.
  - 4xx without envelope throws `ApiError` with `code: 'unknown'` and `message: 'HTTP <status>'`.
  - `fetch` rejection (network error) propagates as the raw error (verifies we do not swallow it).
  - `AbortSignal` propagation — an aborted signal causes the test to see `AbortError`.
  - `setCsrfToken(null)` clears the token; subsequent non-GET sends no header.

- **`src/lib/i18n-resolve.test.ts`** (~6 cases): preferred-order, `default` fallback, any-non-empty last resort, empty-map returns `""`, `{entries}` wrapper unwraps, plain map passes through unchanged.

*session*

- **`src/features/session/api/get-me.test.ts`** (~2 cases): on 200 with `csrfToken: "abc"`, `getCsrfToken()` returns `"abc"` after the queryFn runs (per [get-me.ts:20](../../../../console/src/features/session/api/get-me.ts#L20)); on 401, the queryFn throws and CSRF state is untouched. The smallest test that proves the CSRF-state side effect every non-GET surface relies on.

*api-keys*

- **`src/features/api-keys/api/create-api-key.test.ts`** (~2 cases): POST hits the right endpoint with `{label}` body; `onSuccess` invalidates `['console','api-keys']` (per [create-api-key.ts:14-16](../../../../console/src/features/api-keys/api/create-api-key.ts#L14-L16)) — assertion: the next `list` query refetches.

*feedback*

- **`src/features/feedback/api/list-feedback-infinite.test.ts`** (~5 cases) — covers the URL builder that turns `FeedbackListFilters` into the cursor-paginated `/fb/v1/console/feedback?…` query:
  - Empty filters → `GET /fb/v1/console/feedback` with no querystring.
  - Per-dim `attrs` entries become `?<dim>=<value>` params; entries with empty `dim` or empty `value` are skipped ([list-feedback-infinite.ts:34-36](../../../../console/src/features/feedback/api/list-feedback-infinite.ts#L34-L36)).
  - `q` and `urgent` map to their query params; `urgent: false` is sent as `"false"`, not omitted (per the `!= null` guard at [list-feedback-infinite.ts:38](../../../../console/src/features/feedback/api/list-feedback-infinite.ts#L38)).
  - `getNextPageParam` returns the response's `nextCursor`; `null` ends pagination.
  - Second-page request uses `?cursor=<token>` from `pageParam`.

- **`src/features/feedback/api/get-feedback-detail.test.ts`** (~2 cases): URL composed with id (`/feedback/:id`); 404 surfaces the error envelope.

- **`src/features/feedback/api/get-feedback-stats.test.ts`** (~1 case): smoke fetch returning a populated `FeedbackStats`; stable query key `['console','feedback','stats']`.

*settings (enrich-config — the highest-value remaining surface for #10's editor)*

- **`src/features/settings/api/get-enrich-config.test.ts`** (~2 cases): unwraps `resp.config` from `GetEnrichConfigResponse` ([get-enrich-config.ts:11-12](../../../../console/src/features/settings/api/get-enrich-config.ts#L11-L12)); error envelope surfaces.

- **`src/features/settings/api/update-enrich-config.test.ts`** (~2 cases) — covers the cache-write side effect the settings page relies on:
  - Successful PUT triggers `queryClient.setQueryData(['console','enrich-config'], resp.config)` ([update-enrich-config.ts:17-19](../../../../console/src/features/settings/api/update-enrich-config.ts#L17-L19)) — verified by reading the query data after `mutate` resolves.
  - Failure path does not write to cache.

- **`src/features/settings/api/preview-enrich-prompt.test.ts`** (~1 case): smoke POST returning a rendered prompt body.

**Tier 2 — components / forms (the medium-weight wave):**

- **`src/features/api-keys/components/dialogs.test.tsx`** (~5 cases):
  - `CreateKeyDialog`: submit disabled when input is empty / whitespace; submit calls `onSubmit(trimmedLabel)` and resets the input on success; pending state shows the spinner and disables inputs.
  - `SecretKeyDialog`: clicking copy fires `navigator.clipboard.writeText(secret)` and surfaces a toast (via `vi.mock('sonner', …)` at module boundary — narrow exception, Sonner uses portals).
  - `RevokeKeyDialog`: confirm calls `onConfirm`, cancel calls `onCancel`, both disabled while `pending`.

- **`src/features/notify-targets/components/edit-dialog.test.tsx`** (~4 cases) — **the single highest-value component test in the wave**:
  - Untouched dialog → confirm sends empty patch path → `onClose` called, `onSubmit` not called.
  - User changes `url` only → submitted patch has only `{ url: <trimmed> }`.
  - User toggles "clear secret" checkbox → submitted patch has `{ secret: '' }`; user types a new secret → submitted patch has `{ secret: '<value>' }`; user types and then toggles clear → submitted patch has `{ secret: '' }` (verified at the diff logic in [edit-dialog.tsx:72-73](../../../../console/src/features/notify-targets/components/edit-dialog.tsx#L72-L73)).
  - Switching the `target` prop re-seeds all fields ([edit-dialog.tsx:53-61](../../../../console/src/features/notify-targets/components/edit-dialog.tsx#L53-L61)).

- **`src/features/feedback/components/detail-sheet.test.tsx`** (~2 cases): renders feedback content + dimension chips; closed state hides the sheet.

- **`src/features/feedback/components/dim-stats-bars.test.tsx`** (~2 cases): renders bars from `FeedbackStats`; empty stats renders an empty-state instead of a zero-bar chart.

- **`src/components/dim/i18n-input.test.tsx`** (~3 cases): renders inputs for the active locale chain; default-locale fallback when the requested locale is missing; onChange propagates the per-locale value to the parent; "+ Add locale" appends a new empty entry.

- **`src/components/dim/dimensions-editor.test.tsx`** (~10 cases) — covers the core editor mechanism (WeakMap-based identity tracking that distinguishes "new card" from "persisted card", driving Name + Kind + Taxonomy.Value editability):
  - Initial render: `value=[3 dims]` renders 3 cards with the right display name (resolved via `useDisplayName`); empty name falls back to `dim.editor.unnamed`.
  - **Add dim**: clicking "Add dimension" calls `onChange` with `value.length + 1`; the new card's `Dimension` has `name=''`, `kind='single'`, empty `taxonomy[]` (per [dimensions-editor.tsx:371-380](../../../../console/src/components/dim/dimensions-editor.tsx#L371-L380)).
  - **Remove dim**: clicking the trash button on a card calls `onChange` with the dim filtered out.
  - **Persisted-vs-new identity**: a dim that arrived via `value` prop has `name` input rendered with `readOnly={true}` and Kind Select with `disabled={true}` (per [dimensions-editor.tsx:195,207](../../../../console/src/components/dim/dimensions-editor.tsx#L195-L207)); a freshly-added dim has both editable.
  - **Identity transfer across patches**: editing a *new* dim's name fires `onChange` with the new value, and the next render still treats it as "new" (Name still editable) — proves the WeakMap re-binds the merged object to the same id (per [dimensions-editor.tsx:71-77](../../../../console/src/components/dim/dimensions-editor.tsx#L71-L77)).
  - **Add taxonomy**: clicking "Add value" inside an expanded card calls `onChange` with the dim's `taxonomy` length+1; the new entry has `value=''` and `displayName={entries:{default:''}}`.
  - **Remove taxonomy syncs urgent_set**: removing a taxonomy whose value is in `urgentSet` removes it from both arrays in the same `onChange` (per [dimensions-editor.tsx:264-270](../../../../console/src/components/dim/dimensions-editor.tsx#L264-L270)) — the canonical "two state slices must update together" invariant.
  - **Urgent_set toggle**: clicking a taxonomy chip in the urgent-set strip toggles its `value` in/out of `dim.urgentSet`.
  - **Help text changes with kind**: `kind: 'multi'` renders `taxonomy_help_multi`; `kind: 'single'` renders `taxonomy_help_single` (per [dimensions-editor.tsx:240-243](../../../../console/src/components/dim/dimensions-editor.tsx#L240-L243)).
  - **Card collapse/expand**: clicking the header toggles the `CardContent` visibility.

**Tier 3 — route guard:**

- **`src/routes/_authed.test.tsx`** (~3 cases):
  - MSW handler returns 401 for `/fb/v1/console/me` → after the test router boots, current location is `/login` with `?redirect=<original>` set.
  - MSW handler returns 200 → no redirect; the page renders with the topbar.
  - Navigating to `/feedback` while unauthenticated lands at `/login?redirect=/feedback`; navigating to `/api-keys` lands at `/login?redirect=/api-keys` ([_authed.tsx:18](../../../../console/src/routes/_authed.tsx#L18)).

**Out of scope for this wave (explicit, with the reason):**

- [console/src/components/dim/dimensions-editor.tsx](../../../../console/src/components/dim/dimensions-editor.tsx) (380 LOC) — landed recently for #10; warrants a dedicated test PR with its own decisions about drag/drop, urgent-set derivation, and taxonomy reordering. A follow-up issue is filed in §4-L.
- `_authed.feedback.tsx` / `_authed.settings.tsx` / `_authed.notify-targets.tsx` route files — composition glue around the hooks above. Unit-level coverage of the underlying queries/mutations + isolated dialog/component tests give us the assertion surface; a route smoke test would mostly re-assert the same things at integration-cost. Promoted to "consider once an integration tier exists".
- Visual-only surfaces: `features/usage/components/{bar-chart,sparkline}.tsx`, `components/brand/*`. No business logic.
- `dimension-chips.tsx` — purely presentational; assertions would be near-snapshot. Skipped.
- `login.tsx` `startURL` — trivial string equality; folded into the `_authed` redirect test as both assert the same `?redirect=` contract.

### K · CI change

Single line in [.github/workflows/ci.yml:240](../../../../.github/workflows/ci.yml#L240):

```diff
-      - run: pnpm vitest run --passWithNoTests
+      - run: pnpm vitest run --coverage
```

`--coverage` is required for the §4-B per-file thresholds to fire — without it, the v8 collector never runs and the ratchet on api-client / i18n-resolve / get-me is silently skipped. The overhead at this suite size is sub-second. No separate coverage job, no artifact upload yet (defer to a follow-up if we want HTML reports in PR previews); the inline `text` reporter prints a summary table to the CI log.

### L · Cleanup commits (in scope per user direction)

Two non-test cleanups land in the same PR so the console codebase is in a coherent state when the test infra arrives.

**L-1 · Remove unused form-validation dependencies.**

`react-hook-form`, `zod`, and `@hookform/resolvers` appear in `console/package.json` `dependencies` but have **zero references** in `console/src/**` (verified by repository-wide grep — see §1). They are dead weight in the install graph and a false signal to anyone reading the dependency list.

```bash
cd console && pnpm remove react-hook-form zod @hookform/resolvers
```

The change is dependency-only; no source code touches. Note: these are `dependencies` not `devDependencies`, so removing them slightly reduces production bundle weight (Vite tree-shakes them today since they're unreferenced, but removing them removes the install-side weight and the parse cost on cold installs).

**L-2 · Remove dead `gen:api` / `openapi-typescript` references.**

[console/.gitignore](../../../../console/.gitignore) has a commented block referencing `pnpm gen:api` and `src/api/types.ts`. Neither exists: `gen:api` is not a script in `package.json` (only `gen:proto` is, per [console/package.json](../../../../console/package.json) line ~12), and `src/api/types.ts` has never been written. The commented gitignore line plus the matching `biome.json` override `"src/api/types.ts"` entry are the only remaining traces of an abandoned openapi-typescript plan.

- Strip the comment block (3 lines) from `console/.gitignore`.
- Strip the `"src/api/types.ts"` entry from `console/biome.json`'s override `includes` array.

Net change: 4 lines removed across two config files.

**L-3 · `dimensions-editor.tsx` test coverage (originally a follow-up; folded into this PR).** The 380-LOC editor at `console/src/components/dim/dimensions-editor.tsx` was first staged as a deferred follow-up (issue #88) on the rationale "needs its own design proposal". After a second scope-expansion confirmation, the user chose to absorb it into the same PR. The covered behaviors are listed in §4-J's Tier 2 inventory under `dimensions-editor.test.tsx` (~10 cases). **Issue #88 is closed as "merged into #13's PR" at PR open time**, with its body updated to point at this proposal and the implementing PR; the Closed comment cross-references both.

Drag/drop and row-reordering paths remain out of scope (§3 Non-goals) because the implementation has none — when reordering UI lands later, its test PR comes with it.

---

## 5 · Alternatives considered

### A · `happy-dom` instead of `jsdom`

`happy-dom` is faster but ships fewer DOM affordances. Radix components routinely hit edge APIs (`PointerEvent`, `hasPointerCapture`, scroll APIs) — community reports indicate happy-dom needs *more* shims for Radix than jsdom does, and bulletproof-react chose jsdom. Speed difference at this suite size is well under a second. **Rejected.**

### B · Zero-dep mocking with `vi.mock` + global `fetch` stub

Saves one dependency (MSW). Cost: two parallel mock dialects in the same suite (fetch stub for api-client, module mock for hooks), no shared contract for handler reuse, tests brittle to api-client refactors. Contradicts TkDodo + Kent C. Dodds + bulletproof-react consensus. **Rejected.** (This was my initial position, reversed after benchmarking; the reasoning is preserved here so a future reviewer can see why we chose to add a dependency.)

### C · Vitest `projects` for split node/jsdom environments

`projects` is the Vitest 4-blessed replacement for `environmentMatchGlobs`. Splitting `lib/*.test.ts` to `node` and `features/**/*.test.tsx` to `jsdom` saves perhaps 100 ms per pure-logic file at the cost of two config blocks and a mental rule for every new test. Pure-logic tests already run fine under jsdom. **Rejected.** Revisit if the suite grows past several hundred files and CI duration becomes the bottleneck.

### D · Three-PR phasing (infra → Tier 2 → Tier 3) or two-PR phasing (infra+Tier1 → Tier2+Tier3+cleanup)

Multiple PRs of ~5–8 commits each instead of one PR of ~14–16. Smaller reviewer surface per PR. Cost: multiple CHANGELOG entries, multiple proposals' worth of bookkeeping, the `--passWithNoTests` flag stays in CI for the duration of PR 1 (issue not closeable), and the cleanup (`react-hook-form`/`zod` removal, `gen:api` refs) drifts into a separate scope. User explicitly chose single-PR twice (first at decision time, again at scope-expansion time after seeing the new commit count). **Rejected per direction.**

### G · Filing follow-ups for `dimensions-editor` etc. as separate issues NOW vs only mentioning in proposal §3

This proposal files a dedicated follow-up issue for the 380-line `dimensions-editor` test debt at PR-open time. The alternative — letting §3 Non-goals carry that debt — relies on humans re-reading the proposal months later. GitHub issues are the project's standing tracker; mentioning in a proposal is not. **Rejected the proposal-only path.**

### E · MSW v1, `nock`, `mock-fetch`, other interceptors

MSW v2 is the current line, requires Node ≥18 (we run Node 22 in CI). `nock` only works in Node-style HTTP; doesn't intercept `fetch` cleanly in jsdom. `mock-fetch`-style libraries are at the same layer as a hand-stub. **Rejected.**

### F · TanStack Router file-based route tests (renderWithFileRoutes)

The official TanStack Router docs document a `renderWithFileRoutes` helper for testing file-based routes end to end. It depends on the entire route tree being loaded into a test router and would couple every route test to the global route table. The code-based `createTestRouter` is leaner — we register just the route under test plus a minimal `/login` stub. **Used as the default for this wave**; file-based pattern stays available for a future "integration" suite if we want it.

---

## 6 · Risks / tradeoffs

- **Radix Select interactions in jsdom are the most fragile surface.** Mitigation: where a test depends on the dropdown's open state, prefer asserting the *submitted callback output* over deeply walking the pointer interactions. For `EditNotifyDialog`'s audience select, the test asserts the patch object, not the open/close of the Radix menu. If a future test needs the deeper interaction, `vitest browser mode` becomes the right answer — but that is a much larger lift and outside scope.
- **MSW v2 requires Node ≥18.** CI uses Node 22 (verified in [.github/workflows/ci.yml](../../../../.github/workflows/ci.yml), `console` job `setup-node` with `node-version: 22`). No risk in CI. Local Node 18+ is documented in `console/README.md` already; we will reconfirm.
- **`tsc -b --noEmit` typechecks test files under `strict: true` and `noUnusedLocals: true`.** Tests must therefore be well-typed — no untyped `as any` shortcuts in the production-checked paths. The biome override (§4-D) relaxes lint, not type checking; this is intentional. We accept this cost — strict types in tests catch a real class of mistakes (assertions against the wrong type).
- **Sonner toasts use React portals.** Asserting "toast shown" is best done by mocking the `sonner` module — narrow `vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))` at the test file's top — rather than fighting the portal. Documented in `src/features/api-keys/components/dialogs.test.tsx` as a local convention.
- **Coverage % is not gated.** Adding a threshold gate now would either be too low to mean anything or too high to land. We measure first; gate later, if at all.
- **`pnpm arch` (dependency-cruiser) does not currently restrict `src/testing/`.** We confirmed the `from` patterns in [console/.dependency-cruiser.cjs](../../../../console/.dependency-cruiser.cjs) do not match `src/testing/**`. If future testing utilities want to import a feature provider, that is allowed by current rules. We will not tighten this further in this PR — the cruiser is for *production* import discipline, and tests legitimately need to compose providers from multiple layers.
- **Coverage threshold may fail on first push.** The §4-B thresholds (api-client / i18n-resolve / get-me ≥90% lines) are forward ratchets, so if the Tier 1 tests do not actually cover ≥90% of those files on first run, CI will fail. Mitigation: run `pnpm vitest run --coverage` locally before pushing each test commit and read the per-file table; adjust either the test or the threshold (downward by 5%) at that point rather than at PR review time. The proposal commits to 90% because the three files are small (api-client = 81 LOC, i18n-resolve = 87 LOC, get-me = 25 LOC) and the planned cases are exhaustive — but reality wins over intent.
- **Husky pre-commit hook runs `pnpm biome check` and `pnpm tsc -b --noEmit` on changed paths.** Verified in [.husky/pre-commit](../../../../.husky/pre-commit). New test files will hit both gates locally before they reach CI; this is desirable.

---

## 7 · Implementation plan

Single feature branch off `main` (proposed name: `test/console-vitest-suite`), one PR titled `test(console): add vitest unit tests + MSW infra + cleanup`. Conventional Commits scope `console` per [CLAUDE.md §4](../../../CLAUDE.md#4--conventional-commits). Commits land roughly in this order so each is independently reviewable; bisect points are deliberate.

1. **`chore(console): add jsdom + RTL/jest-dom/user-event/msw/coverage-v8 devDeps`**
   `pnpm add -D` the five packages; commit `package.json` + `pnpm-lock.yaml`. No code change.
2. **`chore(console): remove unused react-hook-form/zod/@hookform/resolvers (L-1)`**
   `pnpm remove` the three; commit `package.json` + `pnpm-lock.yaml`. Confirms zero source touches via `pnpm build` smoke.
3. **`chore(console): drop dead gen:api / src/api/types.ts references (L-2)`**
   3 lines from `.gitignore`, 1 entry from `biome.json` override `includes`.
4. **`test(console): wire vitest jsdom env, setup, providers, MSW server with forward-friendly handler set`**
   `vite.config.ts` `test` block (incl. coverage + thresholds); `tsconfig.app.json` `types`; `biome.json` test override; `src/testing/{setup-tests, test-utils, router-utils, mocks/{server, handlers}}.ts(x)`. After this commit, `pnpm vitest run --coverage` runs (zero tests but real infra; thresholds inert until Tier 1 lands).
5. **`test(console/lib): cover api-client + i18n resolver`**
   `api-client.test.ts` + `i18n-resolve.test.ts`. Lands the §4-B threshold-gated surfaces in one shot — Tier 1 verified to hit ≥90% lines on the api-client and i18n-resolve files via local `pnpm vitest run --coverage` before push.
6. **`test(console/session): cover meQuery CSRF side effect`**
   `get-me.test.ts`. Lands the third threshold-gated file.
7. **`test(console/api-keys): cover create mutation + 3 dialogs`**
   `create-api-key.test.ts` + `dialogs.test.tsx`.
8. **`test(console/notify-targets): cover EditNotifyDialog sparse PATCH diff`**
   `edit-dialog.test.tsx`. The single highest-value component test.
9. **`test(console/feedback): cover list infinite + detail + stats queries`**
   `list-feedback-infinite.test.ts` + `get-feedback-detail.test.ts` + `get-feedback-stats.test.ts`.
10. **`test(console/feedback): cover detail-sheet + dim-stats-bars components`**
    `detail-sheet.test.tsx` + `dim-stats-bars.test.tsx`.
11. **`test(console/settings): cover enrich-config get/update cache write + preview`**
    Three test files for the three settings hooks.
12. **`test(console/dim): cover i18n-input`**
    `i18n-input.test.tsx`.
13. **`test(console/dim): cover dimensions-editor identity tracking + add/remove + urgent_set sync`**
    `dimensions-editor.test.tsx`. The largest single component test in the suite (~400 LOC of test code for a 380-LOC source file).
14. **`test(console/_authed): cover beforeLoad redirect on 401`**
    `_authed.test.tsx`.
15. **`ci(console): pnpm vitest run --coverage; drop --passWithNoTests`**
    The one-line CI change. Lands at the end so the suite is non-empty when CI first runs without the flag.
16. **`docs(changelog): note console test suite + cleanup`**
    `CHANGELOG.md` `[Unreleased] ### Added` and `### Removed` entries — "Console SPA test infrastructure (Vitest jsdom + MSW + Testing Library + v8 coverage with per-file thresholds) and tests covering the api-client (CSRF + error envelope), i18n resolver, `meQuery`, api-keys / notify-targets / feedback / settings queries+mutations+dialogs, `i18n-input`, and the `_authed` route guard. `--passWithNoTests` removed from CI." Under `### Removed`: "Unused `react-hook-form` / `zod` / `@hookform/resolvers` dependencies and dead `gen:api` / `src/api/types.ts` references."

PR body uses `Closes #13` per CLAUDE.md §10 and includes the proposal doc in the same PR (proposal lands in commit 1 alongside the dep add). Status in the proposal flips to `Accepted` before commit 1 is pushed (after user review), then to `Implemented` in a final small commit on the same branch immediately before the PR's "Ready for review" flip.

**At PR-open time**, a follow-up GitHub issue is filed for `dimensions-editor` test coverage (§3 Non-goals, §4-L-3), referencing this PR.

Estimate: 1.5–2 days of focused work for one engineer, ~16 commits + small fix-ups. Roughly 1400–1600 net new lines of test code (the +400 LOC vs the previous estimate comes from `dimensions-editor.test.tsx`), ~200 LOC of removed/cleaned config.

**Pre-push verification, per commit class** (the §8 commands run incrementally):
- After any `test/` commit: `pnpm vitest run --coverage` for that suite, `pnpm tsc -b --noEmit`, `pnpm biome check src`.
- After commit 14 (CI change): `pnpm vitest run --coverage` for the full suite, `pnpm arch`, `pnpm exec vite build`.
- Before pushing the branch: same as above plus a clean install (`rm -rf node_modules && pnpm install --frozen-lockfile`) to catch lockfile drift introduced by commits 1–3.

---

## 8 · Verification

**On the PR branch, local:**

- `pnpm install` → adds the five new devDeps; removes `react-hook-form` / `zod` / `@hookform/resolvers` from `dependencies`; lockfile delta limited to those plus transitive deps.
- `pnpm vitest run --coverage` → all tests green. No `unhandled request` warnings printed. Per-file thresholds on `src/lib/api-client.ts`, `src/lib/i18n-resolve.ts`, `src/features/session/api/get-me.ts` met (≥90% lines). Coverage HTML lands under `console/coverage/html/`; test files, `src/proto/**`, and `src/routeTree.gen.ts` excluded.
- `pnpm tsc -b --noEmit` → green. (Catches a missing `types: ["vitest/globals", "@testing-library/jest-dom"]` entry directly.)
- `pnpm biome check src` → green. New test files pass under the relaxed override; the dropped `src/api/types.ts` override entry does not re-fire.
- `pnpm arch` → green. `src/testing/` does not show up in violations; the removed `react-hook-form` / `zod` / `@hookform/resolvers` are not referenced anywhere.
- `pnpm exec vite build` → production bundle size unchanged or slightly smaller (proves the new devDeps are dev-only, and the removed rhf/zod packages were truly dead code).
- Clean reinstall (`rm -rf console/node_modules && pnpm install --frozen-lockfile && pnpm vitest run --coverage`) reproduces all of the above — guards against locally-cached state.

**On CI:**

- The `console` job from [.github/workflows/ci.yml](../../../../.github/workflows/ci.yml) runs `vitest run --coverage` with no `--passWithNoTests`. Real tests, real green, per-file thresholds enforced.
- The `ci-gate` aggregator stays green.

**Smoke test of regression-catching value (run locally before PR open; revert before final commit):**

- Remove the `X-CSRF-Token` header injection from [api-client.ts:51-53](../../../../console/src/lib/api-client.ts#L51-L53) → `api-client.test.ts` fails on the CSRF cases AND the v8 threshold fails on `src/lib/api-client.ts`.
- Remove `setCsrfToken(me.csrfToken)` in [get-me.ts:20](../../../../console/src/features/session/api/get-me.ts#L20) → `get-me.test.ts` fails on the side-effect assertion AND the v8 threshold fails on `src/features/session/api/get-me.ts`.
- Invert the sparse-PATCH logic in [edit-dialog.tsx:65-79](../../../../console/src/features/notify-targets/components/edit-dialog.tsx#L65-L79) so it always sends every field → `edit-dialog.test.tsx` fails on the "untouched dialog → empty patch" case.
- Drop one of the `if (dim && value)` guards in [list-feedback-infinite.ts:34-36](../../../../console/src/features/feedback/api/list-feedback-infinite.ts#L34-L36) → `list-feedback-infinite.test.ts` fails on the empty-entry-skipped case.
- Remove the urgent-set sync in `removeTaxonomy` at [dimensions-editor.tsx:264-270](../../../../console/src/components/dim/dimensions-editor.tsx#L264-L270) → `dimensions-editor.test.tsx` fails on the "removeTaxonomy syncs urgent_set" case.
- Remove the `qc.setQueryData(...)` line in [update-enrich-config.ts:18](../../../../console/src/features/settings/api/update-enrich-config.ts#L18) → `update-enrich-config.test.ts` fails on the cache-write assertion.

The PR description does not include the smoke test; the reviewer can reproduce locally if curious.

---

## 9 · Open questions

None blocking acceptance. The following remain as live follow-ups (issues filed alongside this PR):

- **Drag/drop and row-reordering UI for `dimensions-editor.tsx`** — the implementation has none; tests come with the UI when it lands.
- **Project-wide coverage thresholds** — once the per-file ratchet on Tier 1 paths runs green for a few weeks, consider extending to `features/**/api/**`. Not in this PR.
- **TanStack Router file-based route tests** — useful if we ever introduce a true integration tier against the live route tree. Not in this PR.
- **MSW handlers + ts-proto fixture generator** — `mocks/handlers.ts` returns hand-crafted minimal shapes. If `ts-proto`-generated factory functions ever land, those would let handlers stop drifting from the wire contract automatically. Out of scope; flagged for the future.

---

## 10 · References

**Authoritative testing-stack guidance (industry consensus benchmarked):**

- [TkDodo — *Testing React Query*](https://tkdodo.eu/blog/testing-react-query) — TanStack Query maintainer; network-boundary MSW recommendation; per-test isolated `QueryClient`; `retry: false`; `onUnhandledRequest: 'error'`.
- [Kent C. Dodds — *Stop mocking fetch*](https://kentcdodds.com/blog/stop-mocking-fetch) — Testing Library author; rationale for network-level interception over module mocks.
- [bulletproof-react · `apps/react-vite`](https://github.com/alan2207/bulletproof-react) — same north-star already cited in [console/.dependency-cruiser.cjs](../../../../console/.dependency-cruiser.cjs); used as the structural template for `src/testing/`, `setup-tests.ts` shape, and devDependency choice.
- [TanStack Router — *Setup Testing*](https://tanstack.com/router/latest/docs/framework/react/how-to/setup-testing) — official testing helpers (`createRouter`, `createMemoryHistory`, `routerContext`) used in `router-utils.tsx`.
- [Vitest 4 Migration Guide](https://vitest.dev/guide/migration.html) — `environmentMatchGlobs` removal; `projects` as the new mechanism (which we deliberately do not need).
- [Vitest — *Test Environment*](https://vitest.dev/guide/environment) — jsdom vs happy-dom, env config semantics.
- [Luis Ball — *Using React Testing Library with RadixUI*](https://www.luisball.com/blog/using-radixui-with-react-testing-library) — concrete shim list (`PointerEvent`, `hasPointerCapture`, `ResizeObserver`, `scrollIntoView`, `matchMedia`).

**In-repo cross-references:**

- [CLAUDE.md §8 — Security baseline (dependency justification)](../../../CLAUDE.md#8--security-baseline)
- [CLAUDE.md §9 — For AI assistants specifically (tests for new behavior)](../../../CLAUDE.md#9--for-ai-assistants-specifically)
- [CLAUDE.md §10 — Proposals (one per issue)](../../../CLAUDE.md#10--proposals-one-per-issue)
- [docs/proposals/2026/06/2026-06-06-feature-organization.md](2026-06-06-feature-organization.md) — feature-based console layout; testing layout coexists.
- [console/.dependency-cruiser.cjs](../../../../console/.dependency-cruiser.cjs) — import boundaries; `src/testing/` deliberately outside the restricted `from` patterns.
- [.github/workflows/ci.yml](../../../../.github/workflows/ci.yml) — `console` job; line 240 is the `--passWithNoTests` removal site.
