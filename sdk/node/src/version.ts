// Baked at build time so the User-Agent reports a version without reading
// package.json at runtime (which isn't reliably present once bundled). Kept in
// sync with package.json "version" by a unit test (version.test.ts).
export const VERSION = '0.2.0'
