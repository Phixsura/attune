import { createFileRoute, redirect } from '@tanstack/react-router'

// "/" inside the authed layout opens the operational overview.

export const Route = createFileRoute('/_authed/')({
  beforeLoad: () => {
    throw redirect({ to: '/control-tower' })
  },
})
