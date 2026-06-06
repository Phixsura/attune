// dependency-cruiser config — architectural import boundaries (issue #19).
//
// Why this tool, not Biome native? See
// docs/proposals/2026/06/2026-06-06-feature-organization.md §4-B for the full
// research, in short:
//   1. Biome's noRestrictedImports matches the literal import specifier,
//      so `import '../foo'` slips past any rule written against
//      `@/features/foo`. dependency-cruiser resolves tsconfig paths and the
//      real filesystem, so both spellings collapse to the same node.
//   2. We need cross-feature isolation written as ONE rule (capture group
//      `$1`), not an O(n²) per-feature override matrix.
//
// Run locally: `pnpm arch`. CI gate: console job (.github/workflows/ci.yml).

module.exports = {
  forbidden: [
    {
      name: 'no-cross-feature',
      severity: 'error',
      comment:
        'A feature may not import another feature (bulletproof-react §unidirectional). ' +
        'Move the shared piece up into src/components/ or src/lib/ instead.',
      from: { path: '^src/features/([^/]+)/.+' },
      to: {
        path: '^src/features/([^/]+)/.+',
        pathNot: '^src/features/$1/.+',
      },
    },
    {
      name: 'shared-no-up',
      severity: 'error',
      comment:
        'Shared layers (components/, lib/, proto/) must not import from features/, ' +
        'routes/, or app/. Imports flow shared → features → app, one direction only.',
      from: { path: '^src/(components|lib|proto)/.+' },
      to: { path: '^src/(features|routes|app)/.+' },
    },
    {
      name: 'features-no-app',
      severity: 'error',
      comment:
        'Features must not reach into routes/ or app/ — TanStack route files are the ' +
        'composition root, features know nothing about routing.',
      from: { path: '^src/features/.+' },
      to: { path: '^src/(app|routes)/.+' },
    },
    {
      name: 'no-circular',
      severity: 'error',
      comment: 'Module graphs must be acyclic.',
      from: {},
      to: { circular: true },
    },
  ],
  options: {
    tsConfig: { fileName: 'tsconfig.json' },
    tsPreCompilationDeps: true, // pick up type-only imports
    doNotFollow: { path: 'node_modules' },
    exclude: { path: 'src/proto/' }, // generated, treat as opaque
  },
};
