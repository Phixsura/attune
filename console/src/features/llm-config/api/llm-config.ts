import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateLLMChannelRequest,
  ListLLMChannelAbilitiesResponse,
  ListLLMChannelModelsResponse,
  ListLLMChannelsResponse,
  ListLLMRoutesResponse,
  LLMChannel,
  LLMChannelAbility,
  LLMProviderModel,
  LLMRoute,
  TestLLMChannelRequest,
  TestLLMChannelResponse,
  UpdateLLMChannelRequest,
  UpsertLLMChannelAbilityRequest,
  UpsertLLMRouteRequest,
} from '@/proto/attune/v1/llm_config'

const BASE = '/fb/v1/console/llm'

export type {
  CreateLLMChannelRequest,
  LLMChannel,
  LLMChannelAbility,
  LLMProviderModel,
  LLMRoute,
  TestLLMChannelRequest,
  TestLLMChannelResponse,
  UpdateLLMChannelRequest,
  UpsertLLMChannelAbilityRequest,
  UpsertLLMRouteRequest,
}

export const llmChannelsQuery = () =>
  queryOptions({
    queryKey: ['console', 'llm', 'channels'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListLLMChannelsResponse>(`${BASE}/channels`, { signal })
      return resp.items
    },
    staleTime: 30_000,
  })

export const llmAbilitiesQuery = (channelId: string) =>
  queryOptions({
    queryKey: ['console', 'llm', 'channels', channelId, 'abilities'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListLLMChannelAbilitiesResponse>(
        `${BASE}/channels/${channelId}/abilities`,
        { signal },
      )
      return resp.items
    },
    enabled: channelId.length > 0,
    staleTime: 30_000,
  })

export const llmChannelModelsQuery = (channelId: string, enabled = true) =>
  queryOptions({
    queryKey: ['console', 'llm', 'channels', channelId, 'models'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListLLMChannelModelsResponse>(`${BASE}/channels/${channelId}/models`, {
        signal,
      })
      return resp.items
    },
    enabled: enabled && channelId.length > 0,
    staleTime: 5 * 60_000,
  })

export const llmRoutesQuery = () =>
  queryOptions({
    queryKey: ['console', 'llm', 'routes'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListLLMRoutesResponse>(`${BASE}/routes`, { signal })
      return resp.items
    },
    staleTime: 30_000,
  })

export function useCreateLLMChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateLLMChannelRequest) =>
      api<LLMChannel>(`${BASE}/channels`, { method: 'POST', body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['console', 'llm'] }),
  })
}

export function useUpdateLLMChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: Omit<UpdateLLMChannelRequest, 'id'> }) =>
      api<LLMChannel>(`${BASE}/channels/${id}`, { method: 'PATCH', body }),
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'channels'] })
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'channels', vars.id, 'abilities'] })
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'channels', vars.id, 'models'] })
    },
  })
}

export function useDeleteLLMChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`${BASE}/channels/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['console', 'llm'] }),
  })
}

export function useTestLLMChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: Omit<TestLLMChannelRequest, 'id'> }) =>
      api<TestLLMChannelResponse>(`${BASE}/channels/${id}/test`, { method: 'POST', body }),
    onSettled: (_, __, vars) => {
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'channels'] })
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'channels', vars.id, 'abilities'] })
    },
  })
}

export function useUpsertLLMAbility() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({
      channelId,
      body,
    }: {
      channelId: string
      body: Omit<UpsertLLMChannelAbilityRequest, 'channelId'>
    }) =>
      api<LLMChannelAbility>(`${BASE}/channels/${channelId}/abilities`, {
        method: 'PUT',
        body,
      }),
    onSuccess: (_, vars) => {
      qc.invalidateQueries({
        queryKey: ['console', 'llm', 'channels', vars.channelId, 'abilities'],
      })
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'routes'] })
    },
  })
}

export function useDeleteLLMAbility() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ channelId, logicalModel }: { channelId: string; logicalModel: string }) =>
      api<void>(`${BASE}/channels/${channelId}/abilities/delete`, {
        method: 'POST',
        body: { logicalModel },
      }),
    onSuccess: (_, vars) => {
      qc.invalidateQueries({
        queryKey: ['console', 'llm', 'channels', vars.channelId, 'abilities'],
      })
      qc.invalidateQueries({ queryKey: ['console', 'llm', 'routes'] })
    },
  })
}

export function useUpsertLLMRoute() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpsertLLMRouteRequest) =>
      api<LLMRoute>(`${BASE}/routes`, { method: 'PUT', body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['console', 'llm', 'routes'] }),
  })
}

export function useDeleteLLMRoute() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ tenantId, purpose }: { tenantId: string; purpose: string }) =>
      api<void>(`${BASE}/routes/delete`, { method: 'POST', body: { tenantId, purpose } }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['console', 'llm', 'routes'] }),
  })
}
