import { defineConfig } from 'tsdown'

// Dual ESM + CJS with split type declarations (.d.ts for ESM, .d.cts for CJS),
// matching the exports map in package.json. native fetch only — no runtime deps.
export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  clean: true,
  treeshake: true,
  target: 'node20',
})
