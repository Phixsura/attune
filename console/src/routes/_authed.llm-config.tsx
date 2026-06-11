import { createFileRoute } from '@tanstack/react-router'
import { llmChannelsQuery, llmRoutesQuery } from '@/features/llm-config/api/llm-config'
import { LLMConfigPage } from '@/features/llm-config/components/llm-config-page'

export const Route = createFileRoute('/_authed/llm-config')({
  component: LLMConfigPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(llmChannelsQuery()),
      context.queryClient.ensureQueryData(llmRoutesQuery()),
    ])
  },
})
