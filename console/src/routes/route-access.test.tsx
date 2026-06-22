import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as AdministrationRoute } from '@/routes/_authed.administration'
import { Route as DeadDeliveriesRoute } from '@/routes/_authed.administration.dead-deliveries'
import { Route as GDPRRoute } from '@/routes/_authed.administration.gdpr'
import { Route as ConfigurationRoute } from '@/routes/_authed.configuration'
import { Route as IntegrationsRoute } from '@/routes/_authed.integrations'
import { Route as ApiKeysRoute } from '@/routes/_authed.integrations.api-keys'
import { Route as SettingsRoute } from '@/routes/_authed.settings'
import { server } from '@/testing/mocks/server'

type BeforeLoadFn = (...args: any[]) => unknown

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

function mockMe(role: 'admin' | 'member' | 'viewer') {
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: { id: 't', name: 'T', slug: 't', locale: 'zh-CN', timezone: 'UTC' },
        user: { openId: `${role}-user`, name: role, role },
        csrfToken: 'tok',
      }),
    ),
  )
}

async function callBeforeLoad(
  beforeLoad: BeforeLoadFn | undefined,
  extra: Record<string, unknown> = {},
): Promise<unknown> {
  if (!beforeLoad) return null
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  try {
    await beforeLoad({
      context: { queryClient },
      ...extra,
    })
    return null
  } catch (err) {
    return err
  }
}

describe('route access guards', () => {
  it('redirects viewers away from integrations api keys', async () => {
    mockMe('viewer')
    const thrown = await callBeforeLoad(ApiKeysRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('allows members into integrations api keys', async () => {
    mockMe('member')
    expect(await callBeforeLoad(ApiKeysRoute.options.beforeLoad)).toBeNull()
  })

  it('redirects members away from GDPR administration pages', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(GDPRRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('keeps dead deliveries admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)).toBeNull()
  })

  it('maps legacy settings deep links to the first section a member can still access', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(SettingsRoute.options.beforeLoad, {
      search: { section: 'gdpr' },
    })
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/configuration/classification')
  })

  it('keeps legacy settings deep links for allowed sections', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(SettingsRoute.options.beforeLoad, {
      search: { section: 'api-keys' },
    })
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/integrations/api-keys')
  })

  it('redirects group root routes to the first page the role can access', async () => {
    mockMe('member')

    const configuration = await callBeforeLoad(ConfigurationRoute.options.beforeLoad, {
      location: { pathname: '/configuration' },
    })
    expect(isRedirect(configuration)).toBe(true)
    expect((configuration as ThrownRedirect).options.to).toBe('/configuration/classification')

    const integrations = await callBeforeLoad(IntegrationsRoute.options.beforeLoad, {
      location: { pathname: '/integrations' },
    })
    expect(isRedirect(integrations)).toBe(true)
    expect((integrations as ThrownRedirect).options.to).toBe('/integrations/inbound-sources')

    const administration = await callBeforeLoad(AdministrationRoute.options.beforeLoad, {
      location: { pathname: '/administration' },
    })
    expect(isRedirect(administration)).toBe(true)
    expect((administration as ThrownRedirect).options.to).toBe('/administration/members')
  })
})
