import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as SettingsRoute } from '@/routes/_authed.settings'
import { server } from '@/testing/mocks/server'

type BeforeLoadFn = NonNullable<typeof SettingsRoute.options.beforeLoad>
type BeforeLoadCtx = Parameters<BeforeLoadFn>[0]

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

function makeContext(section?: string): BeforeLoadCtx {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return {
    context: { queryClient },
    search: section ? { section } : {},
  } as BeforeLoadCtx
}

async function callBeforeLoad(section?: string): Promise<unknown> {
  const beforeLoad = SettingsRoute.options.beforeLoad as BeforeLoadFn
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: { id: 't', name: 'T', slug: 't', locale: 'zh-CN', timezone: 'UTC' },
        user: { openId: 'u', name: 'U', role: 'admin' },
        csrfToken: 'tok',
      }),
    ),
  )

  try {
    await beforeLoad(makeContext(section))
    return null
  } catch (err) {
    return err
  }
}

describe('_authed.settings legacy section redirects', () => {
  it.each([
    ['classification', '/configuration/classification'],
    ['llm', '/configuration/llm'],
    ['enrichment-runtime', '/configuration/enrichment-runtime'],
    ['enrichment_runtime', '/configuration/enrichment-runtime'],
    ['digest', '/integrations/digests'],
    ['digest_subscription', '/integrations/digests'],
    ['audit', '/administration/audit-log'],
    ['audit_log', '/administration/audit-log'],
    ['members', '/administration/members'],
    ['gdpr', '/administration/gdpr'],
    ['tags', '/configuration/tags'],
    ['workflow', '/configuration/workflow'],
  ])('redirects section=%s to %s', async (section, to) => {
    const thrown = await callBeforeLoad(section)
    expect(isRedirect(thrown)).toBe(true)
    const redirect = thrown as ThrownRedirect
    expect(redirect.options.to).toBe(to)
  })

  it('falls back to classification for unknown sections', async () => {
    const thrown = await callBeforeLoad('unknown')
    expect(isRedirect(thrown)).toBe(true)
    const redirect = thrown as ThrownRedirect
    expect(redirect.options.to).toBe('/configuration/classification')
  })
})
