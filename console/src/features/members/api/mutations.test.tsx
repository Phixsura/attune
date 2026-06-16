// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useInviteMember } from '@/features/members/api/invite-member'
import { useRemoveMember } from '@/features/members/api/remove-member'
import { useUpdateMemberRole } from '@/features/members/api/update-member-role'
import { server } from '@/testing/mocks/server'

function wrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
}

describe('useInviteMember', () => {
  it('POSTs email + role and returns the created member', async () => {
    let body: { email?: string; role?: string } = {}
    server.use(
      http.post('/fb/v1/console/members', async ({ request }) => {
        body = (await request.json()) as { email?: string; role?: string }
        return HttpResponse.json({ member: { id: 'new', email: body.email, role: body.role } })
      }),
    )
    const { result } = renderHook(() => useInviteMember(), { wrapper: wrapper() })

    result.current.mutate({ email: 'new@example.com', role: 'member' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(body).toEqual({ email: 'new@example.com', role: 'member' })
  })

  it('surfaces a conflict error', async () => {
    server.use(
      http.post('/fb/v1/console/members', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'exists' }, { status: 409 }),
      ),
    )
    const { result } = renderHook(() => useInviteMember(), { wrapper: wrapper() })

    result.current.mutate({ email: 'dup@example.com', role: 'member' })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toMatchObject({ status: 409, code: 'CONFLICT' })
  })
})

describe('useUpdateMemberRole', () => {
  it('PATCHes the role to the member id', async () => {
    let captured: { id?: string; role?: string } = {}
    server.use(
      http.patch('/fb/v1/console/members/:id', async ({ request, params }) => {
        captured = {
          id: String(params.id),
          role: ((await request.json()) as { role?: string }).role,
        }
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { result } = renderHook(() => useUpdateMemberRole(), { wrapper: wrapper() })

    result.current.mutate({ id: 'm2', role: 'viewer' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(captured).toEqual({ id: 'm2', role: 'viewer' })
  })

  it('surfaces a forbidden error', async () => {
    server.use(
      http.patch('/fb/v1/console/members/:id', () =>
        HttpResponse.json({ code: 'FORBIDDEN', message: 'nope' }, { status: 403 }),
      ),
    )
    const { result } = renderHook(() => useUpdateMemberRole(), { wrapper: wrapper() })

    result.current.mutate({ id: 'm2', role: 'admin' })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toMatchObject({ status: 403, code: 'FORBIDDEN' })
  })
})

describe('useRemoveMember', () => {
  it('DELETEs the member by id', async () => {
    let deletedId = ''
    server.use(
      http.delete('/fb/v1/console/members/:id', ({ params }) => {
        deletedId = String(params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { result } = renderHook(() => useRemoveMember(), { wrapper: wrapper() })

    result.current.mutate('m3')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(deletedId).toBe('m3')
  })

  it('surfaces a conflict error (last admin)', async () => {
    server.use(
      http.delete('/fb/v1/console/members/:id', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'last admin' }, { status: 409 }),
      ),
    )
    const { result } = renderHook(() => useRemoveMember(), { wrapper: wrapper() })

    result.current.mutate('m1')

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toMatchObject({ status: 409, code: 'CONFLICT' })
  })
})
