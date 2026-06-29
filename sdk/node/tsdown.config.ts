import { defineConfig } from 'tsdown'

// Dual ESM + CJS with split type declarations (.d.ts for ESM, .d.cts for CJS),
// matching the exports map in package.json. native fetch only — no runtime deps.
//
// The widget entry builds an additional IIFE bundle for embedding via <script>.
export default defineConfig([
  {
    entry: ['src/index.ts'],
    format: ['esm', 'cjs'],
    dts: true,
    clean: true,
    treeshake: true,
    sourcemap: true,
    target: 'node20',
  },
  {
    entry: { 'attune-widget': 'src/widget.ts' },
    format: ['iife'],
    globalName: 'Attune',
    platform: 'browser',
    treeshake: true,
    sourcemap: true,
    target: 'es2020',
    minify: true,
  },
])
