import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/llm-config')({
  beforeLoad: () => {
    throw redirect({ to: '/configuration/llm' })
  },
})
