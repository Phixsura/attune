import { createFileRoute } from '@tanstack/react-router'
import { notifyTargetsQuery } from '@/features/notify-targets/api/list-notify-targets'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'

export const Route = createFileRoute('/_authed/notify-targets')({
  component: NotifyTargetsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(notifyTargetsQuery()),
})
