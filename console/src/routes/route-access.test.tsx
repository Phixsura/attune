import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as AdministrationRoute } from '@/routes/_authed.administration'
import { Route as DeadDeliveriesRoute } from '@/routes/_authed.administration.dead-deliveries'
import { Route as GDPRRoute } from '@/routes/_authed.administration.gdpr'
import { Route as MembersRoute } from '@/routes/_authed.administration.members'
import { Route as ReliabilityRoute } from '@/routes/_authed.administration.reliability'
import { Route as SecurityRoute } from '@/routes/_authed.administration.security'
import { Route as ConfigurationRoute } from '@/routes/_authed.configuration'
import { Route as EnrichmentRuntimeRoute } from '@/routes/_authed.configuration.enrichment-runtime'
import { Route as LLMConfigRoute } from '@/routes/_authed.configuration.llm'
import { Route as TagsRoute } from '@/routes/_authed.configuration.tags'
import { Route as WorkflowRoute } from '@/routes/_authed.configuration.workflow'
import { Route as InboundSourcesRoute } from '@/routes/_authed.inbound-sources'
import { Route as IntegrationsRoute } from '@/routes/_authed.integrations'
import { Route as ApiKeysRoute } from '@/routes/_authed.integrations.api-keys'
import { Route as DigestRoute } from '@/routes/_authed.integrations.digests'
import { Route as ExternalSyncRoute } from '@/routes/_authed.integrations.external-sync'
import { Route as IntegrationsInboundSourcesRoute } from '@/routes/_authed.integrations.inbound-sources'
import { Route as NotifyTargetsRoute } from '@/routes/_authed.integrations.notify-targets'
import { Route as PublicVisibilityRoute } from '@/routes/_authed.integrations.public-visibility'
import { Route as ReplySendHookRoute } from '@/routes/_authed.integrations.reply-send-hook'
import { Route as RequestNotificationsRoute } from '@/routes/_authed.integrations.request-notifications'
import { Route as SettingsRoute } from '@/routes/_authed.settings'
import { server } from '@/testing/mocks/server'

type BeforeLoadFn = (...args: any[]) => unknown
type LoaderFn = (args: { context: { queryClient: QueryClient } }) => Promise<unknown>

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

function mockMe(role: 'admin' | 'delegated_admin' | 'member' | 'viewer') {
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

  it('redirects the legacy inbound sources entrypoint to the integrations page', async () => {
    const thrown = await callBeforeLoad(InboundSourcesRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/integrations/inbound-sources')
  })

  it('allows members into integrations api keys', async () => {
    mockMe('member')
    expect(await callBeforeLoad(ApiKeysRoute.options.beforeLoad)).toBeNull()
  })

  it('preloads only the api key registry for integrations api keys', async () => {
    mockMe('member')
    expect(await callBeforeLoad(ApiKeysRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/api-keys', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/service-accounts', () => {
        throw new Error('service accounts should not be preloaded by the api keys route loader')
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ApiKeysRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toEqual([])
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/api-keys']))
  })

  it('allows members to view public visibility moderation but redirects viewers', async () => {
    mockMe('member')
    expect(await callBeforeLoad(PublicVisibilityRoute.options.beforeLoad)).toBeNull()

    mockMe('viewer')
    const thrown = await callBeforeLoad(PublicVisibilityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('preloads the public visibility moderation queue for the integration route', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/public-visibility/moderation', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ subjects: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = PublicVisibilityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(PublicVisibilityRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/public-visibility/moderation?limit=50']))
  })

  it('preloads inbound sources for the integrations page and exposes a component', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/inbound/sources', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = IntegrationsInboundSourcesRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toEqual([])
    expect(IntegrationsInboundSourcesRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/inbound/sources']))
  })

  it('redirects members away from GDPR administration pages', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(GDPRRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('allows delegated admins into GDPR administration pages', async () => {
    mockMe('delegated_admin')
    expect(await callBeforeLoad(GDPRRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps dead deliveries admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps the reliability summary admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ReliabilityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(ReliabilityRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps the security emergency access page admin-only', async () => {
    mockMe('delegated_admin')
    const thrown = await callBeforeLoad(SecurityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(SecurityRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps reply send hook configuration admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ReplySendHookRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(ReplySendHookRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps request notifications admin-only and preloads notification settings', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(RequestNotificationsRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(RequestNotificationsRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          appNotificationsEnabled: true,
          emailNotificationsEnabled: false,
        })
      }),
      http.get('/fb/v1/console/request-notifications/sender', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ id: 'sender-1', fromEmail: 'updates@example.test' })
      }),
      http.get('/fb/v1/console/request-notifications/webhook-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ targets: [] })
      }),
      http.get('/fb/v1/console/request-notifications/deliveries', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ deliveries: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = RequestNotificationsRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(RequestNotificationsRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/request-notifications/settings',
        '/fb/v1/console/request-notifications/sender',
        '/fb/v1/console/request-notifications/webhook-targets',
        '/fb/v1/console/request-notifications/deliveries?limit=25',
      ]),
    )
  })

  it('keeps external sync operational-only and preloads its console shell data', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ExternalSyncRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('delegated_admin')
    expect(await callBeforeLoad(ExternalSyncRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/external-sync/health', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: '',
        })
      }),
      http.get('/fb/v1/console/external-sync/connections', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ connections: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ExternalSyncRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(ExternalSyncRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set(['/fb/v1/console/external-sync/health', '/fb/v1/console/external-sync/connections']),
    )
  })

  it('allows admins into leaf configuration and integration routes', async () => {
    mockMe('admin')

    for (const route of [
      MembersRoute,
      TagsRoute,
      WorkflowRoute,
      LLMConfigRoute,
      EnrichmentRuntimeRoute,
      DigestRoute,
      NotifyTargetsRoute,
    ]) {
      await expect(callBeforeLoad(route.options.beforeLoad)).resolves.toBeNull()
      expect(route.options.component).toBeTypeOf('function')
    }
  })

  it('preloads simple leaf route data', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/members', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ members: [] })
      }),
      http.get('/fb/v1/console/tags', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ tags: [] })
      }),
      http.get('/fb/v1/console/workflow/states', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ states: [] })
      }),
      http.get('/fb/v1/console/notify-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const context = { queryClient }
    const membersLoader = MembersRoute.options.loader as LoaderFn
    const tagsLoader = TagsRoute.options.loader as LoaderFn
    const workflowLoader = WorkflowRoute.options.loader as LoaderFn
    const notifyTargetsLoader = NotifyTargetsRoute.options.loader as LoaderFn

    await expect(membersLoader({ context })).resolves.toEqual([])
    await expect(tagsLoader({ context })).resolves.toEqual([])
    await expect(workflowLoader({ context })).resolves.toEqual([])
    await expect(notifyTargetsLoader({ context })).resolves.toEqual([])
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/members',
        '/fb/v1/console/tags',
        '/fb/v1/console/workflow/states?include_archived=true',
        '/fb/v1/console/notify-targets',
      ]),
    )
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

  it('routes delegated admins to operational settings pages', async () => {
    mockMe('delegated_admin')

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
    expect((administration as ThrownRedirect).options.to).toBe('/administration/audit-log')
  })
})
