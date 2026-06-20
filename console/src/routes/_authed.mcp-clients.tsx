import { createFileRoute } from '@tanstack/react-router'
import { mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { MCPClientsPage } from '@/features/mcp-clients/components/mcp-clients-page'

export const Route = createFileRoute('/_authed/mcp-clients')({
  component: MCPClientsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(mcpClientsQuery()),
})
