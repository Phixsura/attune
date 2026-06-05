import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

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
})
