| Field | Value |
|-------|-------|
| Issue | #72 |
| Status | Implemented |
| Started | 2026-06-07 |
| Related | — |

# Organize proposals by year/month directories

## Problem

`docs/proposals/` accumulated 13 files in 3 days (2026-06-04 → 2026-06-06).
At the current rate the flat directory becomes unbrowsable within weeks.

## Goals

- Keep the `YYYY-MM-DD-<slug>.md` filename prefix (date-ordered, `find`-friendly).
- Add a `YYYY/MM/` folder layer so `ls` at any level stays manageable.
- Preserve `git log --follow` history for every moved file.
- Fix all intra-repo references so no links break.

## Non-goals

- Renaming file slugs.
- Changing proposal template or §10 process semantics.
- Moving the `assets/` directory (shared across proposals, stays at root).

## Proposal

```
docs/proposals/
├── 2026/
│   └── 06/
│       ├── 2026-06-04-standalone-dockerfile.md
│       ├── 2026-06-05-docker-compose.md
│       └── …
├── README.md   ← brief layout guide
└── (assets/ if needed later)
```

Use `git mv` for each file so rename detection preserves history.
Update CLAUDE.md §10 location rule to `docs/proposals/YYYY/MM/YYYY-MM-DD-<slug>.md`.

## Alternatives considered

| Option | Pros | Cons |
|--------|------|------|
| Keep flat | No migration needed | Unbrowsable at scale |
| Year-only (`YYYY/`) | Fewer levels | Still 50+ files/dir within a year |
| Year/month (chosen) | ~10 files/dir at current rate | One extra level of nesting |

## Risks / tradeoffs

- Existing bookmarks or external links to GitHub blob paths will 404 until
  updated. Mitigated: the repo is pre-release, external linking is minimal.
- Slightly longer paths in cross-references. Acceptable trade-off for
  navigability.

## Implementation plan

1. `git mv` all 12 existing proposals into `docs/proposals/2026/06/`.
2. Update CLAUDE.md §10 location rule.
3. Fix all cross-references (CLAUDE.md §5, console/README.md,
   .dependency-cruiser.cjs, superpowers plan, self-references in proposals).
4. Add `docs/proposals/README.md` explaining the layout.

## Verification

- `git log --follow` works on moved files.
- `grep -r "docs/proposals/2026-06-0"` finds no stale references outside
  the migration plan.
- `find docs/proposals -maxdepth 1 -name '*.md'` returns only README.md.

## References

- Issue #72
- CLAUDE.md §10 (proposals process)
