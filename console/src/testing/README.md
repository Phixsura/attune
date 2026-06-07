# Console testing — 30-line cheat sheet

Stack: **Vitest 4 (jsdom)** + **Testing Library** + **MSW v2** + **v8 coverage**.
Layout mirrors [`alan2207/bulletproof-react`](https://github.com/alan2207/bulletproof-react/tree/master/apps/react-vite) / `apps/react-vite`. Design rationale: `docs/proposals/2026/06/2026-06-07-console-vitest-tests.md`.

## Where things live

| File | Purpose |
|---|---|
| `setup-tests.ts` | Per-test: jest-dom matchers · `cleanup()` · MSW lifecycle · Radix jsdom shims |
| `test-utils.tsx` | `renderWithProviders()` (i18n + fresh `QueryClient(retry:false)` + `userEvent.setup()`) and RTL re-exports |
| `router-utils.tsx` | `makeTestRouter()` for TanStack Router guard/loader tests |
| `mocks/server.ts` | `setupServer(...handlers)` |
| `mocks/handlers.ts` | Default handler per `/fb/v1/console/*` endpoint, **typed against ts-proto** — proto change → file fails to compile |

## What to use, when

| You're testing… | Use |
|---|---|
| A pure function or query options | `import { describe, expect, it } from 'vitest'`; no render. |
| A hook (no DOM) | `renderHook(() => useThing(), { wrapper })` — see `features/api-keys/api/create-api-key.test.ts` for the `createElement(QueryClientProvider, ...)` wrapper that keeps the file `.ts` (no JSX). |
| A component | `renderWithProviders(<Thing />)` → `user`, `screen.getByTestId(...)`, assert. |
| Production route's `beforeLoad` / `loader` | `import { Route } from '@/routes/_x'` → call `Route.options.beforeLoad(...)` with a mocked context. See `routes/_authed.test.tsx`. |
| A full page composition | `Route.options.component` is wrapped by lazy() (codesplit). `await component.preload()` first, render inside `<Suspense fallback={null}>`. See `routes/_authed.feedback.test.tsx`. |

## Per-test handler override

Default handlers in `mocks/handlers.ts` only need to be shape-correct enough so the page doesn't crash. For case-specific shapes (error envelopes, paginated cursors, 401s, slow responses), override in the test body:

```ts
import { http, HttpResponse } from 'msw'
import { server } from '@/testing/mocks/server'

server.use(
  http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })),
)
```

`afterEach(server.resetHandlers)` clears overrides automatically.

`onUnhandledRequest: 'error'` is on — a request to an endpoint with no handler fails loudly.

## Conventions

- **`data-testid` over text matching.** Tests should not couple to `zh-CN.json`. If you find yourself writing `getByRole('button', { name: '保存' })`, add a testid to the source. Naming: `<feature>-<action>` (e.g. `edit-notify-save`, `create-key-submit`).
- **Reset module-level state.** `setCsrfToken(null)` in `beforeEach` for any test that exercises the api-client's CSRF state.
- **One mock dialect.** MSW for network. `vi.spyOn(navigator.clipboard, 'writeText')` for clipboard (user-event v14 installs its own; spy after `renderWithProviders`). `vi.mock('sonner', ...)` for portal-based toasts (one carve-out — see `dialogs.test.tsx`).
- **Threshold gates.** `vite.config.ts` carries per-file coverage thresholds for the high-trust paths. If your change drops coverage below the gate, the right move is to add the test — not lower the threshold.
- **Fixtures should be typed.** `mocks/handlers.ts` defaults already are. For overrides, prefer `type Foo = ... ; const f: Foo = { ... }` so a proto change surfaces at compile time.

## Run

```bash
pnpm vitest run                         # CI mode (forks, parallel)
pnpm vitest                             # watch mode
pnpm vitest run --coverage              # with v8 coverage + threshold gating
pnpm vitest run src/lib/api-client      # one file (or directory)
pnpm vitest run -t "CSRF"               # by test name pattern
```
