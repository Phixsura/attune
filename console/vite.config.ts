import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
// vitest/config re-exports vite's defineConfig with the `test` option
// typed in. Importing from 'vite' would surface a TS error on the test
// block because vite's UserConfig has no `test` property.
import { configDefaults, defineConfig } from 'vitest/config'

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
    exclude: [...configDefaults.exclude, 'e2e/**', 'playwright.config.ts'],
    setupFiles: ['./src/testing/setup-tests.ts'],
    // Full-suite jsdom/Radix/user-event flows are CPU-bound in the fork pool.
    // Keep coverage extra roomy while giving ordinary local CI enough budget
    // for long smoke flows to finish under load.
    testTimeout: isCoverageRun ? 120_000 : 30_000,
    // pool: 'forks' (vitest 4 default) gives each test file its own
    // child process, so MSW server instances, navigator.clipboard
    // prototype patches, and api-client's module-level CSRF state
    // are all isolated. No `fileParallelism: false` workaround.
    pool: 'forks',
    coverage: {
      provider: 'v8',
      // Coverage gates logic modules, API adapters, hooks, and helpers. TSX
      // component render paths are still exercised by Vitest, but V8's JSX
      // branch map treats display fallbacks and optional prop guards as branch
      // debt even when the interaction state is covered.
      include: ['src/**/*.ts'],
      exclude: [
        'src/proto/**',
        'src/routeTree.gen.ts',
        'src/**/*.tsx',
        'src/features/reliability/replay-worksheet.ts',
        'src/testing/**',
        '**/*.test.{ts,tsx}',
        'src/main.tsx',
        // Pure TanStack route declaration modules are verified by
        // src/routes/route-wiring-coverage.test.tsx. V8 maps their
        // generated wrappers to synthetic uncovered functions, which
        // makes them noisy in line/function coverage without adding
        // business-path signal.
        'src/routes/_authed.administration.audit-log.tsx',
        'src/routes/_authed.administration.dead-deliveries.tsx',
        'src/routes/_authed.administration.gdpr.tsx',
        'src/routes/_authed.administration.guard-policies.tsx',
        'src/routes/_authed.administration.members.tsx',
        'src/routes/_authed.administration.reliability.tsx',
        'src/routes/_authed.administration.security.tsx',
        'src/routes/_authed.administration.system-readiness.tsx',
        'src/routes/_authed.administration.tsx',
        'src/routes/_authed.analytics.classification-quality.tsx',
        'src/routes/_authed.analytics.llm-usage.tsx',
        'src/routes/_authed.analytics.search-quality.tsx',
        'src/routes/_authed.analytics.tsx',
        'src/routes/_authed.analytics.usage.tsx',
        'src/routes/_authed.api-keys.tsx',
        'src/routes/_authed.change-password.tsx',
        'src/routes/_authed.classification-quality.tsx',
        'src/routes/_authed.clusters.tsx',
        'src/routes/_authed.configuration.classification.tsx',
        'src/routes/_authed.configuration.enrichment-runtime.tsx',
        'src/routes/_authed.configuration.llm.tsx',
        'src/routes/_authed.configuration.tags.tsx',
        'src/routes/_authed.configuration.workflow.tsx',
        'src/routes/_authed.configuration.tsx',
        'src/routes/_authed.control-tower.tsx',
        'src/routes/_authed.feedback.customer-requests.tsx',
        'src/routes/_authed.feedback.clusters.tsx',
        'src/routes/_authed.feedback.index.tsx',
        'src/routes/_authed.feedback.portal.tsx',
        'src/routes/_authed.feedback.terminal-failures.tsx',
        'src/routes/_authed.feedback.tsx',
        'src/routes/_authed.guard-policies.tsx',
        'src/routes/_authed.inbound-sources.tsx',
        'src/routes/_authed.index.tsx',
        'src/routes/_authed.llm-usage.tsx',
        'src/routes/_authed.integrations.api-keys.tsx',
        'src/routes/_authed.integrations.digests.tsx',
        'src/routes/_authed.integrations.external-sync.tsx',
        'src/routes/_authed.integrations.inbound-sources.tsx',
        'src/routes/_authed.integrations.notify-targets.tsx',
        'src/routes/_authed.integrations.public-visibility.tsx',
        'src/routes/_authed.integrations.reply-send-hook.tsx',
        'src/routes/_authed.integrations.request-notifications.tsx',
        'src/routes/_authed.integrations.tsx',
        'src/routes/_authed.llm-config.tsx',
        'src/routes/_authed.mcp-clients.tsx',
        'src/routes/_authed.notify-targets.tsx',
        'src/routes/_authed.outbox-dead.tsx',
        'src/routes/_authed.search-quality.tsx',
        'src/routes/_authed.settings.tsx',
        'src/routes/_authed.tsx',
        'src/routes/_authed.usage.tsx',
        'src/routes/login.tsx',
        'src/routes/login_.error.tsx',
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
        'src/features/settings/api/get-enrich-config.ts': { lines: 90 },
        'src/features/settings/api/update-enrich-config.ts': { lines: 85 },
        'src/features/settings/api/preview-enrich-prompt.ts': { lines: 90 },
        'src/features/api-keys/api/create-api-key.ts': { lines: 90 },
        'src/hooks/use-draft-guard.ts': { lines: 90, statements: 90, branches: 85, functions: 85 },
        'src/hooks/use-keyboard-save.ts': {
          lines: 90,
          statements: 90,
          branches: 80,
          functions: 90,
        },
      },
    },
  },
})
