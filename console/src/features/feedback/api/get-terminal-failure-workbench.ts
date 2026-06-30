import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetTerminalFailureWorkbenchResponse } from '@/proto/attune/v1/ingest'

export type TerminalFailureWorkbench = GetTerminalFailureWorkbenchResponse
export const terminalFailureWorkbenchQueryKey = [
  'console',
  'feedback',
  'terminal-failures',
] as const

export const terminalFailureWorkbenchQuery = () =>
  queryOptions({
    queryKey: terminalFailureWorkbenchQueryKey,
    queryFn: ({ signal }) =>
      api<TerminalFailureWorkbench>('/fb/v1/console/feedback/terminal-failures', {
        signal,
      }),
    staleTime: 30_000,
  })
