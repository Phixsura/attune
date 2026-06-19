import { defineConfig } from 'tsdown'

// Dual ESM + CJS with split type declarations (.d.mts / .d.cts), matching the
// exports map in package.json. native fetch only — no runtime dependencies.
export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  clean: true,
  treeshake: true,
  target: 'node20',
})
