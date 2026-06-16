// SPDX-License-Identifier: Apache-2.0

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { server } from '@/testing/mocks/server'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

function mockMeResponse(role: string, openId: string) {
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: { id: 't1', name: 'Test', slug: 'test', locale: 'zh-CN', timezone: 'UTC' },
        user: { openId, name: 'Test User', role },
        csrfToken: 'test-token',
      }),
    ),
  )
}

describe('usePermissions', () => {
  describe('role detection', () => {
    it('detects admin role', async () => {
      mockMeResponse('admin', 'admin-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('admin')
      })
      expect(result.current.isAdmin).toBe(true)
      expect(result.current.isMember).toBe(false)
      expect(result.current.isViewer).toBe(false)
      expect(result.current.userId).toBe('admin-user')
    })

    it('detects member role', async () => {
      mockMeResponse('member', 'member-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('member')
      })
      expect(result.current.isAdmin).toBe(false)
      expect(result.current.isMember).toBe(true)
      expect(result.current.isViewer).toBe(false)
    })

    it('detects viewer role', async () => {
      mockMeResponse('viewer', 'viewer-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('viewer')
      })
      expect(result.current.isAdmin).toBe(false)
      expect(result.current.isMember).toBe(false)
      expect(result.current.isViewer).toBe(true)
    })

    it('defaults to viewer when no user data', async () => {
      server.use(http.get('/fb/v1/console/me', () => HttpResponse.json({})))
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('viewer')
      })
      expect(result.current.isViewer).toBe(true)
      expect(result.current.userId).toBeUndefined()
    })
  })

  describe('can() permission check', () => {
    it('admin can do everything', async () => {
      mockMeResponse('admin', 'admin-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('admin')
      })
      expect(result.current.can('feedback:view')).toBe(true)
      expect(result.current.can('feedback:edit')).toBe(true)
      expect(result.current.can('feedback:batch_delete')).toBe(true)
      expect(result.current.can('settings:members:invite')).toBe(true)
    })

    it('member has limited permissions', async () => {
      mockMeResponse('member', 'member-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('member')
      })
      expect(result.current.can('feedback:view')).toBe(true)
      expect(result.current.can('feedback:edit')).toBe(true)
      expect(result.current.can('feedback:batch_delete')).toBe(false)
      expect(result.current.can('settings:members:invite')).toBe(false)
    })

    it('viewer has read-only permissions', async () => {
      mockMeResponse('viewer', 'viewer-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('viewer')
      })
      expect(result.current.can('feedback:view')).toBe(true)
      expect(result.current.can('feedback:edit')).toBe(false)
      expect(result.current.can('settings:members:view')).toBe(false)
    })
  })

  describe('legacy methods', () => {
    it('canView always returns true', async () => {
      mockMeResponse('viewer', 'viewer-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('viewer')
      })
      expect(result.current.canView()).toBe(true)
    })

    it('canEdit matches feedback:edit permission', async () => {
      mockMeResponse('member', 'member-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('member')
      })
      expect(result.current.canEdit()).toBe(true)
    })

    it('canManage is admin-only', async () => {
      mockMeResponse('admin', 'admin-user')
      const { result: adminResult } = renderHook(() => usePermissions(), {
        wrapper: createWrapper(),
      })
      await waitFor(() => expect(adminResult.current.role).toBe('admin'))
      expect(adminResult.current.canManage()).toBe(true)

      mockMeResponse('member', 'member-user')
      const { result: memberResult } = renderHook(() => usePermissions(), {
        wrapper: createWrapper(),
      })
      await waitFor(() => expect(memberResult.current.role).toBe('member'))
      expect(memberResult.current.canManage()).toBe(false)
    })

    it('canDelete allows admin to delete anything', async () => {
      mockMeResponse('admin', 'admin-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('admin')
      })
      expect(result.current.canDelete('other-user')).toBe(true)
      expect(result.current.canDelete()).toBe(true)
    })

    it('canDelete allows member to delete own items only', async () => {
      mockMeResponse('member', 'member-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('member')
      })
      expect(result.current.canDelete('member-user')).toBe(true)
      expect(result.current.canDelete('other-user')).toBe(false)
      expect(result.current.canDelete()).toBe(false)
    })

    it('canDelete denies viewer', async () => {
      mockMeResponse('viewer', 'viewer-user')
      const { result } = renderHook(() => usePermissions(), { wrapper: createWrapper() })

      await waitFor(() => {
        expect(result.current.role).toBe('viewer')
      })
      expect(result.current.canDelete('viewer-user')).toBe(false)
      expect(result.current.canDelete()).toBe(false)
    })
  })
})
