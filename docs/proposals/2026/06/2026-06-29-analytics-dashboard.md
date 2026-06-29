# Analytics dashboard home page

| Field | Value |
|-------|-------|
| Issue | #202 (gap #73) |
| Status | Implemented |
| Started | 2026-06-29 |
| Related | — |

## Problem

The Console's analytics section contains usage and LLM cost pages, but
lacks a unified overview that combines feedback volume, urgency, and
classification distributions into a single view. Operators must navigate
between multiple pages to assess monthly health.

## Goals

- Single-page analytics overview combining feedback stats and usage trends.
- Reuse existing backend endpoints (no new API work).
- Fit the established PageHero + Card dashboard pattern.
- Become the default analytics landing page in sidebar navigation.

## Non-goals

- New backend aggregation endpoints.
- Real-time streaming (WebSocket/SSE) updates.
- Custom date range selection (uses current calendar month like usage page).

## Proposal

### Route and component

- `routes/_authed.analytics.dashboard.tsx` — route file with loader
  pre-fetching all three queries in parallel.
- `features/feedback/components/analytics-dashboard.tsx` — page component
  composing PageHero metrics, DimStatsBars, UsageSparkline, and
  UsageBarChart from existing shared components.

### Data sources

| Query | Endpoint | Data |
|-------|----------|------|
| `feedbackStatsQuery` | `GET /fb/v1/console/feedback/stats` | total, urgentCount, dims[] |
| `enrichConfigQuery` | `GET /fb/v1/console/enrich-config` | dimension definitions |
| `usageQuery` | `GET /fb/v1/console/usage` | daily series, quota |

### Layout

1. **PageHero**: eyebrow + title + subtitle + 3 metrics (total feedback,
   urgent count, active days)
2. **Two-column grid**: dimension distribution card (DimStatsBars) +
   trend card (total count + sparkline)
3. **Full-width card**: daily bar chart (UsageBarChart)
4. **Empty state**: when total = 0, shows icon + message

### Navigation

Dashboard is added as the first item in the `analytics` nav group
(icon: PieChart), so `/analytics` redirects to `/analytics/dashboard`
by default.

## Verification

- 3 component tests (key metrics rendering, empty state, dimension display)
- Full vitest suite: 836 tests pass (93 files)
- TypeScript: 0 errors
- Biome: 0 lint issues
- Vite build: succeeds
