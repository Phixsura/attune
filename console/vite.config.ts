import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
// vitest/config re-exports vite's defineConfig with the `test` option
// typed in. Importing from 'vite' would surface a TS error on the test
// block because vite's UserConfig has no `test` property.
import { defineConfig } from 'vitest/config'

// attune console dev server. Proxies /fb/v1 to local attune backend on
// :8090 so the SPA can be developed against the real Go service without
// CORS or cookie domain complications.
//
// In production a same-origin nginx serves /console/* statically and
// /fb/v1/* through to attune — see attune/docs/2026-05-15-console-tech-stack.md.

const apiTarget = process.env.ATTUNE_CONSOLE_API_TARGET ?? 'http://127.0.0.1:8090'
const isCoverageRun = process.argv.some(
  (arg) => arg === '--coverage' || arg.startsWith('--coverage='),
)

export default defineConfig({
  // Prod nginx serves the SPA under /console/* — vite's asset URLs must
  // be path-prefixed accordingly. Dev keeps absolute "/" because the
  // dev server has no path prefix.
  base: process.env.NODE_ENV === 'production' ? '/console/' : '/',

  plugins: [
    // Order matters: router plugin must run before react plugin so the
    // generated routeTree.gen.ts exists when JSX is compiled.
    TanStackRouterVite({
      target: 'react',
      autoCodeSplitting: true,
      // Exclude colocated *.test.{ts,tsx} files in src/routes/ from the
      // generated route tree — they don't export a Route and aren't pages.
      routeFileIgnorePattern: '\\.test\\.(ts|tsx)$',
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    port: 10092,
    strictPort: true,
    proxy: {
      '/fb/v1': {
        target: apiTarget,
        changeOrigin: false,
      },
    },
  },

  // Vitest test config — jsdom default so component tests can render.
  // Pure-logic tests run fine under jsdom too; the single-env choice
  // keeps config simple (Vitest 4 removed environmentMatchGlobs).
  // Full rationale: docs/proposals/2026/06/2026-06-07-console-vitest-tests.md §4-B.
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/testing/setup-tests.ts'],
    // V8 coverage plus jsdom/Radix/user-event flows can exceed the normal
    // fast-fail budget in full-suite CI. Keep ordinary test runs strict.
    testTimeout: isCoverageRun ? 60_000 : 10_000,
    // pool: 'forks' (vitest 4 default) gives each test file its own
    // child process, so MSW server instances, navigator.clipboard
    // prototype patches, and api-client's module-level CSRF state
    // are all isolated. No `fileParallelism: false` workaround.
    pool: 'forks',
    coverage: {
      provider: 'v8',
      include: ['src/**'],
      exclude: [
        'src/proto/**',
        'src/routeTree.gen.ts',
        'src/testing/**',
        '**/*.test.{ts,tsx}',
        'src/main.tsx',
      ],
      reporter: ['text', 'html', 'json-summary', 'lcov'],
      // Forward ratchet on every file the suite already covers ≥85%
      // lines. A regression that drags coverage below the threshold
      // fails CI loudly. Per-file (not global) so adding an untested
      // file doesn't silently lower the project bar.
      thresholds: {
        'src/lib/api-client.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/lib/editable-rows.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/lib/i18n-resolve.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/features/session/api/get-me.ts': { lines: 90, statements: 90 },
        'src/features/feedback/api/list-feedback-infinite.ts': { lines: 90 },
        'src/features/feedback/api/get-feedback-detail.ts': { lines: 90 },
        'src/features/feedback/api/get-feedback-stats.ts': { lines: 90 },
        'src/features/feedback/components/detail-sheet.tsx': { lines: 85 },
        'src/features/feedback/components/dim-stats-bars.tsx': { lines: 85 },
        'src/features/settings/api/get-enrich-config.ts': { lines: 90 },
        'src/features/settings/api/update-enrich-config.ts': { lines: 85 },
        'src/features/settings/api/preview-enrich-prompt.ts': { lines: 90 },
        'src/features/notify-targets/components/edit-dialog.tsx': { lines: 80 },
        'src/features/api-keys/components/dialogs.tsx': { lines: 70 },
        'src/features/api-keys/api/create-api-key.ts': { lines: 90 },
        'src/components/dim/i18n-input.tsx': { lines: 75 },
        'src/components/dim/dimensions-editor.tsx': { lines: 80 },
        'src/hooks/use-draft-guard.ts': { lines: 90, statements: 90, branches: 85, functions: 90 },
        'src/hooks/use-keyboard-save.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/components/unsaved-changes-dialog.tsx': { lines: 65, branches: 60 },
        'src/components/draft-banner.tsx': { lines: 90, statements: 90, branches: 85, functions: 90 },
        'src/components/save-status.tsx': { lines: 90, statements: 90, branches: 85, functions: 90 },
        'src/routes/_authed.tsx': { lines: 60 },
      },
    },
  },
})
