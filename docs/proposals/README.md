# Proposals

Design proposals for attune, following [CLAUDE.md §10](../../CLAUDE.md#10--proposals-one-per-issue).

## Layout

```
docs/proposals/
├── YYYY/
│   └── MM/
│       └── YYYY-MM-DD-<slug>.md
└── README.md (this file)
```

- **Filename:** `YYYY-MM-DD-<slug>.md` — date is when the proposal was started.
- **Directory:** file goes into `YYYY/MM/` matching the start date.
- **Find all proposals:** `find docs/proposals -name '*.md' ! -name README.md`

## Creating a new proposal

1. Determine today's date and slug: `2026-07-15-my-feature.md`
2. Create the directory if needed: `mkdir -p docs/proposals/2026/07`
3. Copy the template from CLAUDE.md §10 (header table + ADR sections).
4. Commit alongside the implementing PR with `Closes #N`.
