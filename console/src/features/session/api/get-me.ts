import { queryOptions } from '@tanstack/react-query'
import { api, setCsrfToken } from '@/lib/api-client'
import type { GetMeResponse } from '@/proto/attune/v1/session'

// Feature-stable alias over the ts-proto response type.
export type SessionMe = GetMeResponse

// Query options for the current session. Used by route loaders + UI.
//
// `retry: false` so a 401 on first boot doesn't double-flash the login
// page; bare 401 = "not logged in" not "transient network error".
//
// Side effect: pulls the CSRF token from the response and stashes it in the
// api-client so subsequent state-changing requests can include it.
export const meQuery = () =>
  queryOptions({
    queryKey: ['console', 'me'],
    queryFn: async ({ signal }) => {
      const me = await api<SessionMe>('/fb/v1/console/me', { signal })
      setCsrfToken(me.csrfToken)
      return me
    },
    retry: false,
    staleTime: 60_000,
  })
