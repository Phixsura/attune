import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListScopePresetsResponse, ListScopesResponse } from '@/proto/attune/v1/api_key'

export const scopesQuery = () =>
  queryOptions({
    queryKey: ['console', 'api-keys', 'scopes'],
    queryFn: () => api<ListScopesResponse>('/fb/v1/console/api-keys/scopes'),
    staleTime: 1000 * 60 * 60,
  })

export const presetsQuery = () =>
  queryOptions({
    queryKey: ['console', 'api-keys', 'presets'],
    queryFn: () => api<ListScopePresetsResponse>('/fb/v1/console/api-keys/presets'),
    staleTime: 1000 * 60 * 60,
  })
