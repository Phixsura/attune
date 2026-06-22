import type { TFunction } from 'i18next'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import type { GuardPolicy } from '@/features/guard-policies/api/guard-policies'
import {
  GuardPoliciesPage,
  guardPolicyPageTestables,
} from '@/features/guard-policies/components/guard-policies-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const t = ((key: string, opts?: { defaultValue?: string; value?: string }) => {
  const catalog: Record<string, string> = {
    'guard_policies.all_targets': 'all targets',
    'guard_policies.target_labels.channels': 'channels',
    'guard_policies.target_labels.source_ids': 'source ids',
    'guard_policies.target_labels.source_tags': 'source tags',
    'guard_policies.target_labels.purposes': 'purposes',
    'guard_policies.target_labels.stages': 'stages',
    'guard_policies.target_labels.env': 'env',
    'guard_policies.channels.api': 'API',
    'guard_policies.purposes.enrich': 'Enrich',
    'guard_policies.stages.llm_input': 'LLM input',
  }
  if (key === 'guard_policies.priority_value') return `priority ${opts?.value}`
  return catalog[key] ?? opts?.defaultValue ?? key
}) as TFunction

function tenantPolicy(overrides: Partial<GuardPolicy> = {}): GuardPolicy {
  return {
    id: 'gp-1',
    name: 'tenant pii',
    kind: 'default',
    enabled: true,
    priority: 100,
    scope: 'tenant',
    target: {
      channels: ['api'],
      sourceIds: ['source-1'],
      sourceTags: ['regulated'],
      purposes: ['enrich'],
      stages: ['llm_input'],
      environments: ['prod'],
    },
    rules: [
      {
        guard: 'pii',
        stage: 'llm_input',
        entities: ['email', 'phone'],
        action: 'redact',
        replacement: '<PII:{entity}>',
      },
    ],
    ...overrides,
  }
}

describe('GuardPoliciesPage pure policy helpers', () => {
  it('splitCSV trims empty values and preserves non-empty tags', () => {
    expect(guardPolicyPageTestables.splitCSV(' regulated, , vip , api ')).toEqual([
      'regulated',
      'vip',
      'api',
    ])
  })

  it('firstKnown accepts allowed values and falls back for unknown values', () => {
    expect(guardPolicyPageTestables.firstKnown(['email'], ['api', 'email'], 'api')).toBe('email')
    expect(guardPolicyPageTestables.firstKnown(['rss'], ['api', 'email'], 'api')).toBe('api')
    expect(guardPolicyPageTestables.firstKnown(undefined, ['api', 'email'], 'api')).toBe('api')
  })

  it('formFromPolicy derives defaults from the first rule and target', () => {
    const form = guardPolicyPageTestables.formFromPolicy(tenantPolicy())
    expect(form).toMatchObject({
      name: 'tenant pii',
      kind: 'default',
      enabled: true,
      priority: '100',
      channel: 'api',
      sourceIds: 'source-1',
      sourceTags: 'regulated',
      purpose: 'enrich',
      stage: 'llm_input',
      environment: 'prod',
      entities: ['email', 'phone'],
      action: 'redact',
      replacement: '<PII:{entity}>',
    })
  })

  it('formFromPolicy supplies create-dialog defaults for a new policy', () => {
    const form = guardPolicyPageTestables.formFromPolicy(null)
    expect(form.name).toBe('')
    expect(form.kind).toBe('default')
    expect(form.enabled).toBe(true)
    expect(form.channel).toBe('all')
    expect(form.entities).toEqual(['email', 'phone', 'cn_mobile', 'cn_id', 'credit_card'])
  })

  it('policyFromForm trims targets and keeps replacement only for redaction rules', () => {
    const base = guardPolicyPageTestables.formFromPolicy(null)
    const redacted = guardPolicyPageTestables.policyFromForm(
      {
        ...base,
        name: '  regulated email  ',
        priority: '250',
        channel: 'email',
        sourceIds: 'source-1, source-2',
        sourceTags: ' regulated, vip ',
        environment: 'prod, staging',
        entities: ['email'],
        action: 'redact',
        replacement: '  <EMAIL>  ',
      },
      null,
    )
    expect(redacted).toMatchObject({
      id: '',
      name: 'regulated email',
      priority: 250,
      scope: 'tenant',
      target: {
        channels: ['email'],
        sourceIds: ['source-1', 'source-2'],
        sourceTags: ['regulated', 'vip'],
        purposes: ['enrich'],
        stages: ['llm_input'],
        environments: ['prod', 'staging'],
      },
      rules: [
        {
          guard: 'pii',
          stage: 'llm_input',
          entities: ['email'],
          action: 'redact',
          replacement: '<EMAIL>',
        },
      ],
    })

    const blocked = guardPolicyPageTestables.policyFromForm(
      { ...base, name: 'block phone', entities: ['phone'], action: 'block' },
      tenantPolicy({ id: 'gp-existing' }),
    )
    expect(blocked.id).toBe('gp-existing')
    expect(blocked.rules[0]?.replacement).toBe('')
  })

  it('formatTarget explains scoped policies and collapses empty targets to all targets', () => {
    expect(guardPolicyPageTestables.formatTarget(tenantPolicy({ target: undefined }), t)).toBe(
      'all targets',
    )
    expect(
      guardPolicyPageTestables.formatTarget(
        tenantPolicy({
          target: {
            channels: ['api'],
            sourceIds: ['source-1'],
            sourceTags: ['regulated'],
            purposes: ['enrich'],
            stages: ['llm_input'],
            environments: ['prod'],
          },
        }),
        t,
      ),
    ).toBe(
      [
        'channels: API',
        'source ids: source-1',
        'source tags: regulated',
        'purposes: Enrich',
        'stages: LLM input',
        'env: prod',
      ].join('\n'),
    )
  })
})

describe('GuardPoliciesPage user flows', () => {
  it('renders policies, previews effective rules, and creates a tenant policy', async () => {
    let previewBody: unknown
    let createBody: unknown
    server.use(
      http.get('/fb/v1/console/guard-policies', () =>
        HttpResponse.json({ items: [tenantPolicy()] }),
      ),
      http.post('/fb/v1/console/guard-policies/effective', async ({ request }) => {
        previewBody = await request.json()
        return HttpResponse.json({ rules: tenantPolicy().rules })
      }),
      http.post('/fb/v1/console/guard-policies', async ({ request }) => {
        createBody = await request.json()
        return HttpResponse.json({ ...tenantPolicy(), id: 'created' })
      }),
    )

    const { user } = renderWithProviders(<GuardPoliciesPage />)

    await waitFor(() => {
      expect(screen.getByText('tenant pii')).toBeInTheDocument()
    })
    expect(screen.getByText('策略总数')).toBeInTheDocument()
    expect(screen.getByText('策略治理建议')).toBeInTheDocument()
    expect(screen.getByText(/渠道: API/)).toBeInTheDocument()
    expect(screen.getAllByText('pii').length).toBeGreaterThanOrEqual(1)

    await user.type(screen.getByLabelText('来源 ID'), 'source-1')
    await user.type(screen.getByLabelText('来源标签'), 'regulated, vip')
    await user.click(screen.getByRole('button', { name: '预览生效规则' }))
    await waitFor(() => {
      expect(previewBody).toEqual({
        channel: 'api',
        sourceId: 'source-1',
        sourceTags: ['regulated', 'vip'],
        purpose: 'enrich',
        environment: '',
      })
    })
    expect(screen.getAllByText('脱敏').length).toBeGreaterThanOrEqual(1)

    await user.click(screen.getByRole('button', { name: '新建策略' }))
    const createDialog = screen.getByRole('dialog', { name: '新建防护策略' })
    await user.type(within(createDialog).getByLabelText('名称'), 'email default')
    await user.click(within(createDialog).getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(createBody).toMatchObject({
        policy: {
          name: 'email default',
          kind: 'default',
          enabled: true,
          priority: 100,
          scope: 'tenant',
          target: {
            channels: [],
            sourceIds: [],
            sourceTags: [],
            purposes: ['enrich'],
            stages: ['llm_input'],
            environments: [],
          },
          rules: [
            {
              guard: 'pii',
              stage: 'llm_input',
              entities: ['email', 'phone', 'cn_mobile', 'cn_id', 'credit_card'],
              action: 'redact',
              replacement: '<PII:{entity}>',
            },
          ],
        },
      })
    })
  })

  it('edits and deletes tenant policies while system policies stay read-only', async () => {
    let patchBody: unknown
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/guard-policies', () =>
        HttpResponse.json({
          items: [
            tenantPolicy(),
            tenantPolicy({
              id: 'system-1',
              name: 'system default',
              scope: 'system',
              tenantId: undefined,
            }),
          ],
        }),
      ),
      http.patch('/fb/v1/console/guard-policies/:id', async ({ params, request }) => {
        patchBody = await request.json()
        return HttpResponse.json({ ...tenantPolicy(), id: params.id, name: 'edited pii' })
      }),
      http.delete('/fb/v1/console/guard-policies/:id', ({ params }) => {
        deletedId = String(params.id)
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<GuardPoliciesPage />)

    await waitFor(() => {
      expect(screen.getByText('tenant pii')).toBeInTheDocument()
      expect(screen.getByText('system default')).toBeInTheDocument()
    })
    expect(screen.getAllByTitle('系统策略只读')).toHaveLength(2)
    for (const button of screen.getAllByTitle('系统策略只读')) {
      expect(button).toBeDisabled()
    }

    await user.click(screen.getByTitle('编辑策略'))
    const editDialog = screen.getByRole('dialog', { name: '编辑防护策略' })
    const nameInput = within(editDialog).getByLabelText('名称')
    await user.clear(nameInput)
    await user.type(nameInput, 'edited pii')
    await user.click(within(editDialog).getByRole('button', { name: '保存' }))
    await waitFor(() => {
      expect(patchBody).toMatchObject({ policy: { id: 'gp-1', name: 'edited pii' } })
    })

    await user.click(screen.getByTitle('删除策略'))
    const deleteDialog = screen.getByRole('dialog', { name: '删除防护策略' })
    await user.click(within(deleteDialog).getByRole('button', { name: '删除策略' }))
    await waitFor(() => {
      expect(deletedId).toBe('gp-1')
    })
  })
})
