// SPDX-License-Identifier: Apache-2.0

import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { membersQuery } from '@/features/members/api/list-members'
import { server } from '@/testing/mocks/server'

describe('membersQuery', () => {
  it('returns members array from response', async () => {
    server.use(
      http.get('/fb/v1/console/members', () =>
        HttpResponse.json({
          members: [
            {
              id: 'm1',
              memberType: 'admin',
              userId: 'u1',
              email: 'admin@example.com',
              role: 'admin',
              roleSource: 'bootstrap',
              invitedAt: '1700000000',
              acceptedAt: '1700000001',
            },
            {
              id: 'm2',
              memberType: 'oidc_user',
              userId: 'u2',
              email: 'user@example.com',
              role: 'member',
              roleSource: 'idp',
              invitedAt: '1700000000',
              acceptedAt: '1700000002',
            },
          ],
        }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const members = await qc.fetchQuery(membersQuery())
    expect(members).toHaveLength(2)
    expect(members[0]).toMatchObject({ id: 'm1', role: 'admin' })
    expect(members[1]).toMatchObject({ id: 'm2', role: 'member' })
  })

  it('returns empty array when members is undefined', async () => {
    server.use(http.get('/fb/v1/console/members', () => HttpResponse.json({})))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const members = await qc.fetchQuery(membersQuery())
    expect(members).toEqual([])
  })

  it('surfaces error on 403 forbidden', async () => {
    server.use(
      http.get('/fb/v1/console/members', () =>
        HttpResponse.json({ code: 'FORBIDDEN', message: 'not authorized' }, { status: 403 }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await expect(qc.fetchQuery(membersQuery())).rejects.toMatchObject({
      status: 403,
      code: 'FORBIDDEN',
    })
  })

  it('surfaces error on 500 internal', async () => {
    server.use(
      http.get('/fb/v1/console/members', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'server error' }, { status: 500 }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await expect(qc.fetchQuery(membersQuery())).rejects.toMatchObject({
      status: 500,
      code: 'INTERNAL',
    })
  })
})
