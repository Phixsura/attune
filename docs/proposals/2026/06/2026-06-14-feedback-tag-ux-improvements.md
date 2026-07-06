# Feedback tag UX improvements

| | |
|---|---|
| **Issue** | #28 |
| **Status** | Implemented |
| **Started** | 2026-06-14 CST |
| **Related** | `2026-06-14-feedback-manual-tags.md` (base implementation — backend complete, frontend basic) |

## Problem

The base manual tagging implementation (#28, 12 commits on
`feat/feedback-manual-tags-28`) delivers a working backend and minimal frontend.
The backend supports filtering (`?tag=uuid`), inline creation (`resolveTag`),
batch operations (`BatchUpdate` — 100 rows × 20 ops), and exclusive scope
enforcement. The frontend only surfaces ~40% of this: a DropdownMenu for
adding existing tags by ID, badge display in the detail sheet, and remove.

Compared to GitHub Issues / Linear, the current UX is missing:

- Tags not visible in the feedback list — must open detail sheet to see them.
- No tag filtering in the list — backend `?tag=uuid` is unused.
- No search when tag count grows — DropdownMenu has no type-ahead.
- No inline tag creation — must go to Settings → Tags first.
- No batch operations — must open each row individually.
- No tooltip context — users don't know what a tag means or its constraints.

## Goals

1. Tags visible in the feedback list (below title, GitHub-style).
2. Tag filter in the filter bar (single Select, consistent with dimension filters).
3. Combobox with search + inline creation replaces DropdownMenu.
4. Checkbox multi-select + floating action bar for batch add/remove.
5. Rich tooltip on tag badges (description, exclusive scope, usage count).

## Non-goals

- Multi-tag AND filtering (requires backend changes; single-tag filter is sufficient).
- Drag-and-drop tag reordering.
- Tag color picker in inline creation (auto-assigned; editable in Settings).
- Keyboard shortcuts for batch operations.

## Proposal

Component-first approach: build 3 reusable components, then wire into pages.

### New files (5)

#### `console/src/components/tag/tag-combobox.tsx`

Reusable tag picker with search and inline creation.

```tsx
interface TagComboboxProps {
  availableTags: Tag[]
  onSelect: (tagId: string) => void
  onCreate: (name: string) => void
  disabled?: boolean
  trigger?: ReactNode
}
```

Built with Radix `Popover` + hand-written filterable list (no new dependency).
Input filters `availableTags` by name substring. When input doesn't match any
existing tag, a "Create «input»" option appears at the bottom. Each item shows
the tag color dot, name, and `exclusiveScope` (if set) as muted text.

Used in two places: `FeedbackTagSection` (detail sheet) and
`SelectionActionBar` (batch operations).

#### `console/src/components/tag/tag-badge-tooltip.tsx`

TagBadge wrapped in Radix `Tooltip` showing rich metadata.

```tsx
interface TagBadgeTooltipProps {
  tag: Tag
  onRemove?: () => void
}
```

Tooltip content: tag description (if set), exclusive scope name, usage count.
The existing `TagBadge` component is unchanged — the tooltip variant composes it.

#### `console/src/features/feedback/hooks/use-row-selection.ts`

Pure state hook for multi-select.

```tsx
function useRowSelection(itemIds: string[]): {
  selected: Set<string>
  toggle: (id: string) => void
  toggleAll: () => void
  clear: () => void
  isAllSelected: boolean
}
```

Auto-cleans stale IDs when `itemIds` changes (page turn, filter change).

#### `console/src/features/feedback/api/batch-update-tags.ts`

Mutation hook wrapping `BatchUpdateFeedbackTagsRequest`.

```tsx
mutationFn: (req: BatchUpdateFeedbackTagsRequest) =>
  api('/fb/v1/console/feedback/tags/batch', { method: 'POST', body: req })
```

On success: invalidate `['console', 'feedback']` + `['console', 'tags']`.

#### `console/src/features/feedback/components/selection-action-bar.tsx`

Floating bar rendered when `selected.size > 0`.

```tsx
interface SelectionActionBarProps {
  count: number
  availableTags: Tag[]
  onBatchAdd: (tagIds: string[]) => void
  onBatchRemove: (tagIds: string[]) => void
  onCancel: () => void
}
```

Renders above the table: "已选 N 条" + "添加标签" button (opens TagCombobox) +
"移除标签" button (opens TagCombobox filtered to the intersection of tags
across selected rows — computed client-side from `items.filter(i =>
selected.has(i.id)).flatMap(i => i.tags)` deduplicated by tag ID) + "取消"
button.

### Modified files (3)

#### `features/feedback/components/feedback-tags.tsx`

Replace `DropdownMenu` with `TagCombobox`. Wire `onSelect` → existing
`addTag.mutate({ tagId })` and `onCreate` → `addTag.mutate({ tagName })`.
The `AddFeedbackTagRequest` already supports `tagName` + `tagColor` fields;
backend `resolveTag` handles find-or-create.

#### `routes/_authed.feedback.tsx`

Three changes:

1. **FilterBar**: add `tagFilter` state + tag Select (same style as dimension
   Selects). Options from `allTags` (non-archived). Value maps to
   `FeedbackListFilters.tag`.

2. **FeedbackTable**: render `TagBadgeTooltip` below title+content for each
   `f.tags` entry. Pass `allTags` for full tag metadata lookup.
   Add checkbox column: `useRowSelection` drives selection state.

3. **SelectionActionBar**: render above table when `selected.size > 0`.
   Wire to `useBatchUpdateTags`.

#### `features/feedback/api/list-feedback-infinite.ts`

Add `tag?: string` to `FeedbackListFilters`. In query function:
`if (filters.tag) params.set('tag', filters.tag)`.

### Exclusive scope in batch operations

No special handling needed. The backend `addWithScope` in
`tagassignment/handler.go:170-190` already enforces mutual exclusion per
feedback row during batch processing. If a batch adds tag A (scope "priority")
to 50 rows, the backend automatically removes any existing "priority"-scoped
tag from each row individually.

## Alternatives considered

1. **Page-first (modify inline, extract later)**: faster initial velocity but
   TagCombobox logic duplicated between detail sheet and batch bar. Rejected
   because the duplication is certain, not hypothetical.

2. **cmdk dependency for Combobox**: better keyboard navigation out of the box,
   but adds a dependency for one component. The hand-written Popover+list is
   ~60 lines and sufficient. Can upgrade later if needed.

3. **Multi-tag AND filter**: requires backend `?tag=a&tag=b` support + SQL
   intersection query. Deferred — single-tag filter covers the common case.

## Risks / tradeoffs

- **List card height increases** when tags are shown below title. Acceptable —
  GitHub Issues uses the same pattern and the extra ~20px per row is marginal.
- **No new dependencies** — TagCombobox is hand-written. Tradeoff: less polished
  keyboard nav than cmdk, but zero bundle cost and no supply-chain risk.
- **Batch operations trust backend scope enforcement** — frontend sends raw
  tagId lists without checking exclusiveScope. This is correct (backend is
  authoritative) but means the UI won't show a warning before replacing a
  scoped tag.

## Implementation plan

Single PR on existing `feat/feedback-manual-tags-28` branch. ~9 files touched,
all frontend. No proto/backend/migration changes needed.

Order:
1. `tag-combobox.tsx` + unit test
2. `tag-badge-tooltip.tsx`
3. Upgrade `feedback-tags.tsx` to use TagCombobox
4. `use-row-selection.ts` + unit test
5. `batch-update-tags.ts`
6. `selection-action-bar.tsx`
7. Wire into `_authed.feedback.tsx` (filter + list tags + batch)
8. `list-feedback-infinite.ts` tag filter param
9. Update CHANGELOG

## Verification

- Dev server visual testing: list shows tags, filter works, combobox search +
  create, batch select + add/remove, tooltip hover.
- `pnpm tsc -b --noEmit` — zero errors.
- `pnpm biome check` — zero errors.
- `pnpm vitest run` — all pass, coverage thresholds met.
- `pnpm arch` — no dependency-cruiser violations.

## References

- Base implementation proposal: `docs/proposals/2026/06/2026-06-14-feedback-manual-tags.md`
- Backend handler: `internal/handlers/console/tagassignment/handler.go`
- Proto types: `proto/attune/v1/tag.proto`
