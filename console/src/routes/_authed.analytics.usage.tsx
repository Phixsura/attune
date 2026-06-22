import { createFileRoute } from '@tanstack/react-router'
import { usageQuery } from '@/features/usage/api/get-usage'
import { UsagePage } from '@/routes/_authed.usage'

export const Route = createFileRoute('/_authed/analytics/usage')({
  component: UsagePage,
  loader: ({ context }) => context.queryClient.ensureQueryData(usageQuery()),
})
