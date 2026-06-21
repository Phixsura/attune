import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import {
  useActivateEnrichPromptVersion,
  useUpdateEnrichConfig,
} from '@/features/settings/api/update-enrich-config'
import { server } from '@/testing/mocks/server'

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return { qc, wrapper }
}

describe('useUpdateEnrichConfig — cache write side effect', () => {
  it('PUT success writes resp.config into ["console","enrich-config"]', async () => {
    server.use(
      http.put('/fb/v1/console/enrich-config', async ({ request }) => {
        const body = (await request.json()) as { promptTemplate?: string; dimensions: unknown[] }
        return HttpResponse.json({
          config: {
            promptTemplate: body.promptTemplate ?? 'echoed',
            defaultPromptTemplate: 'DEFAULT',
            dimensions: body.dimensions,
            promptPolicy: {
              policyId: 'enrich.legacy_custom_template',
              policyVersion: 'sha256:abc',
              promptVersion: 'enrich.legacy_custom_template@sha256:abc',
              mode: 'legacy_custom_override',
              promptSource: 'custom_template',
              templateLanguage: 'custom',
              displayLocale: 'zh-CN',
              displayLanguageName: 'Simplified Chinese',
              variables: [],
              outputs: [],
              warnings: [],
            },
            promptVersions: [],
          },
        })
      }),
    )
    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useUpdateEnrichConfig(), { wrapper })
    result.current.mutate({
      promptTemplate: 'tpl',
      dimensions: [],
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    // setQueryData per update-enrich-config.ts:17-19 — the cached
    // enrich-config now reflects the server's echoed shape.
    expect(qc.getQueryData(['console', 'enrich-config'])).toEqual({
      promptTemplate: 'tpl',
      defaultPromptTemplate: 'DEFAULT',
      dimensions: [],
      promptPolicy: {
        policyId: 'enrich.legacy_custom_template',
        policyVersion: 'sha256:abc',
        promptVersion: 'enrich.legacy_custom_template@sha256:abc',
        mode: 'legacy_custom_override',
        promptSource: 'custom_template',
        templateLanguage: 'custom',
        displayLocale: 'zh-CN',
        displayLanguageName: 'Simplified Chinese',
        variables: [],
        outputs: [],
        warnings: [],
      },
      promptVersions: [],
    })
  })

  it('PUT failure does not write to cache', async () => {
    server.use(
      http.put('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({ code: 'BAD_REQUEST', message: 'nope' }, { status: 400 }),
      ),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData(['console', 'enrich-config'], { original: true })
    const { result } = renderHook(() => useUpdateEnrichConfig(), { wrapper })
    result.current.mutate({ promptTemplate: '', dimensions: [] })
    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(qc.getQueryData(['console', 'enrich-config'])).toEqual({ original: true })
  })

  it('activate success writes resp.config into ["console","enrich-config"]', async () => {
    server.use(
      http.post('/fb/v1/console/enrich-config/versions/:id\\:activate', ({ params }) =>
        HttpResponse.json({
          config: {
            promptTemplate: undefined,
            defaultPromptTemplate: 'DEFAULT',
            dimensions: [],
            promptPolicy: {
              policyId: 'enrich.default',
              policyVersion: '1',
              promptVersion: 'enrich.default@1',
              mode: 'default',
              promptSource: 'built_in',
              templateLanguage: 'en',
              displayLocale: 'zh-CN',
              displayLanguageName: 'Simplified Chinese',
              variables: [],
              outputs: [],
              warnings: [],
            },
            promptVersions: [
              {
                id: params.id,
                promptVersion: 'enrich.default@1',
                policyId: 'enrich.default',
                policyVersion: '1',
                mode: 'default',
                promptSource: 'built_in',
                createdAt: '2026-06-21T00:00:00Z',
                isActive: true,
                hasTemplate: false,
                dimensionsCount: 0,
                warnings: [],
              },
            ],
          },
        }),
      ),
    )
    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useActivateEnrichPromptVersion(), { wrapper })
    result.current.mutate('version-1')
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(qc.getQueryData(['console', 'enrich-config'])).toMatchObject({
      promptVersions: [{ id: 'version-1', isActive: true }],
    })
  })
})
