# Real-browser publish-artifact smoke for the Node SDK

| Field | Value |
| --- | --- |
| **Issue** | [#168](https://github.com/Phixsura/attune/issues/168) |
| **Status** | Implemented |
| **Started** | 2026-06-28 |
| **Related** | [#37](https://github.com/Phixsura/attune/issues/37) (Node SDK), [2026-06-28-browser-ingest-cors.md](./2026-06-28-browser-ingest-cors.md), [2026-06-28-public-api-version-contract.md](./2026-06-28-public-api-version-contract.md) |

## Problem

The Node SDK's `pnpm e2e` harness already did a lot of real work: it booted a
real attune server, exercised the live HTTP suite, packed the npm artifact,
installed that tarball into an external consumer, and verified ESM/CJS usage.

The browser path still had a blind spot. The harness only proved that the SDK
could be bundled for `platform=browser`; it did not prove that:

- a real browser page on a second origin could preflight and submit ingest with
  the packed artifact;
- the new attune-native CORS allowlist actually admitted only the configured
  origin;
- the browser could observe the exposed `X-Attune-Api-Version` response header;
  or
- browser-specific header behavior (notably the forbidden `User-Agent`
  override) did not break real usage.

For a package explicitly documented as browser-safe for publishable ingest keys,
that gap is too large.

## Goals

- Exercise the packed npm tarball in a real browser, not just Node.
- Verify both the positive and negative CORS cases against a live attune server.
- Keep the test close to real consumer usage: plain installed artifact,
  ordinary static HTML/JS, no workspace link magic.
- Minimize dependency cost.

## Non-goals

- Running full browser E2E on every CI push right now.
- Introducing a full frontend test framework for the SDK examples.
- Testing server-only management APIs in the browser.

## Proposal

1. Extend `sdk/node/scripts/e2e.sh` with a real-browser smoke step after the
   packed tarball is installed into the throwaway consumer.
2. Add a small `scripts/browser-smoke.mjs` runner that:
   - serves the throwaway consumer directory on two distinct localhost origins;
   - loads the installed `dist/index.js` directly in the browser;
   - submits one allowlisted ingest and one blocked-origin ingest; and
   - asserts success/failure plus exposed API-version headers via DOM state.
3. Configure the throwaway attune server with
   `ingest.cors_allowed_origins: ["http://127.0.0.1:<allowed-port>"]`.
4. Run that browser smoke with the dedicated restricted `ingest:write` key
   rather than the broader management key used by the external-consumer admin
   flow.
5. Prefer the freshly installed or cached Playwright Chromium executable before
   ambient system browser channels so release-time verification stays
   deterministic.
6. Harden the SDK's browser behavior so it does not attempt to set a custom
   `User-Agent` header outside Node.
7. Add database assertions after the browser smoke so the allowlisted origin is
   proven to persist exactly one row and the blocked origin persists none.

## Alternatives considered

### Keep the existing esbuild-only browser check

Rejected. That only proves buildability, not runtime browser interoperability.

### Add full `playwright` with bundled browser downloads

Rejected. The SDK only needs automation, not managed browser binaries in the
package itself. `playwright-core` plus local Chrome / Edge or existing cached
Playwright Chromium installs keeps the dependency surface smaller.

### Drive browser tests through the example workspace instead of the packed tarball

Rejected. The release risk is in the published artifact. The smoke should load
the bytes that `npm publish` would ship.

## Risks / tradeoffs

- The browser smoke now depends on a local Chromium-family executable. The
  runner therefore supports explicit executable override and common local
  discovery paths.
- The runner must also clean up ephemeral files and local static servers even
  if one of the startup stages fails; otherwise repeatability suffers during
  local iteration.
- Fixed localhost ports make the release gate flaky when another SDK e2e run or
  developer process is already listening. The harness should therefore allocate
  free localhost ports dynamically instead of assuming static ones.
- This is not yet wired into CI, so it remains a high-value release/developer
  gate rather than a branch-protection gate.

## Implementation plan

1. Add a browser-smoke runner and minimal static page assets generated at
   runtime inside the throwaway consumer.
2. Add a minimal dev-only automation dependency (`playwright-core`).
3. Update the e2e harness to run the browser smoke with the restricted browser
   key and verify DB effects.
4. Make browser launch order deterministic, localhost port allocation
   collision-resistant, and cleanup failure-safe.
5. Update README/changelog and add a client test for browser `User-Agent`
   omission.

## Verification

- Node unit tests cover browser-like `User-Agent` omission.
- `pnpm e2e` now verifies:
  - live Node HTTP e2e;
  - packed tarball ESM/CJS usage;
  - real-browser allowlisted origin success;
  - real-browser blocked-origin failure; and
  - database persistence for only the allowed browser origin.

## References

- `2026-06-19-node-typescript-sdk.md`
- `2026-06-28-browser-ingest-cors.md`
- `2026-06-28-public-api-version-contract.md`
