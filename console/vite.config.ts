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

export default defineConfig({
  // Prod nginx serves the SPA under /console/* — vite's asset URLs must
  // be path-prefixed accordingly. Dev keeps absolute "/" because the
  // dev server has no path prefix.
  base: process.env.NODE_ENV === 'production' ? '/console/' : '/',

  plugins: [
    // Order matters: router plugin must run before react plugin so the
    // generated routeTree.gen.ts exists when JSX is compiled.
    TanStackRouterVite({ target: 'react', autoCodeSplitting: true }),
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
        target: 'http://127.0.0.1:8090',
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
      reporter: ['text', 'html'],
      // Soft forward ratchet on the highest-trust paths. A regression
      // on the api-client's CSRF/error envelope, the i18n resolver, or
      // meQuery's CSRF side effect should fail CI loudly.
      thresholds: {
        'src/lib/api-client.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/lib/i18n-resolve.ts': { lines: 90, statements: 90, branches: 80, functions: 90 },
        'src/features/session/api/get-me.ts': { lines: 90, statements: 90 },
      },
    },
  },
})
