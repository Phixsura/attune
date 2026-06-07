# Attune Console

The Stage B self-service console SPA. Physically independent from the main
repo's pnpm workspace (see
[`attune/docs/2026-05-15-console-tech-stack.md`](../docs/2026-05-15-console-tech-stack.md)).

## Stack

Vite 6 · React 19 · TS 5.9 · TanStack Router · TanStack Query 5 · shadcn/ui
+ Radix · Tailwind 4 · react-i18next · date-fns 3 · Biome 2 · Vitest 4
\+ Testing Library + MSW (tests).

## Run

```bash
pnpm install
pnpm gen:proto     # delegates to repo-root `make proto`; regenerates src/proto/**
pnpm dev           # :10092; /fb/v1 proxied to local attune backend on :8090
```

## API contract sync

`.proto` files (`../proto/attune/v1/*.proto`) are the single source of truth
(#19, CLAUDE.md §11):

```bash
pnpm gen:proto                  # equivalent to `cd .. && make proto`
git diff src/proto/             # must show no diff, else CI's proto-sync job fails
```

ts-proto turns the proto files into TS types and writes them to
`src/proto/attune/v1/*.ts` (read-only). Each feature's
`src/features/<x>/api/*.ts` re-exports the types under
consumption-stable names.

## Architectural boundaries

`pnpm arch` runs dependency-cruiser, enforcing bulletproof-react-style
unidirectional imports:

- `shared → features → app`, one direction only
- no cross-feature imports
- no circular dependencies

See `.dependency-cruiser.cjs` and
[`docs/proposals/2026/06/2026-06-06-feature-organization.md`](../docs/proposals/2026/06/2026-06-06-feature-organization.md).

## Tests

See [`src/testing/README.md`](src/testing/README.md) for the testing
cheat sheet (Vitest + jsdom + MSW + Testing Library).
