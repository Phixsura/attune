import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/guard-policies')({
  beforeLoad: () => {
    throw redirect({ to: '/administration/guard-policies' })
  },
})
