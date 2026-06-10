import { createFileRoute } from '@tanstack/react-router'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'

export const Route = createFileRoute('/_authed/api-keys')({
  component: ApiKeysPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(apiKeysQuery()),
})
