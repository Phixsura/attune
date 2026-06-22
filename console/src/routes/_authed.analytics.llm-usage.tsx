import { createFileRoute } from '@tanstack/react-router'
import { llmUsageQuery } from '@/features/usage/api/get-llm-usage'
import { LLMUsagePage } from '@/routes/_authed.llm-usage'

const defaultFilters = { granularity: 'week', range: 'now-30d' } as const

export const Route = createFileRoute('/_authed/analytics/llm-usage')({
  component: LLMUsagePage,
  loader: ({ context }) => context.queryClient.ensureQueryData(llmUsageQuery(defaultFilters)),
})
