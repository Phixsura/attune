import { createFileRoute } from '@tanstack/react-router'
import { llmChannelsQuery, llmRoutesQuery } from '@/features/llm-config/api/llm-config'
import { LLMConfigPage } from '@/features/llm-config/components/llm-config-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration/llm')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'llm_config:view' }),
  component: LLMConfigPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(llmChannelsQuery()),
      context.queryClient.ensureQueryData(llmRoutesQuery()),
    ])
  },
})
