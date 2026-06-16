import { createFileRoute, redirect } from '@tanstack/react-router'
import { llmChannelsQuery, llmRoutesQuery } from '@/features/llm-config/api/llm-config'
import { LLMConfigPage } from '@/features/llm-config/components/llm-config-page'
import { meQuery } from '@/features/session/api/get-me'

export const Route = createFileRoute('/_authed/llm-config')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQuery())
    const role = me.user?.role
    if (role !== 'admin' && role !== 'member') {
      throw redirect({ to: '/feedback' })
    }
  },
  component: LLMConfigPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(llmChannelsQuery()),
      context.queryClient.ensureQueryData(llmRoutesQuery()),
    ])
  },
})
