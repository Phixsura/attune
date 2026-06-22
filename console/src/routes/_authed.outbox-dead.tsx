import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/outbox-dead')({
  beforeLoad: () => {
    throw redirect({ to: '/administration/dead-deliveries' })
  },
})
