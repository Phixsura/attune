import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { PublicVisibilityPage } from '@/features/public-visibility/components/public-visibility-page'
import {
  ModerationState,
  type ModerationSubject,
  PortalSubmissionFieldKind,
  PublicAccessMode,
  type PublicCustomerRequestDetail,
  type PublicCustomerRequestSummary,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

server.use(
  http.get('/fb/v1/console/public-visibility/views', () => HttpResponse.json({ views: [] })),
)

const currentRequestID = '11111111-1111-1111-1111-111111111111'
const similarRequestID = '33333333-3333-3333-3333-333333333333'

describe('PublicVisibilityPage', () => {
  it('lets admins save public policy and request profile changes', async () => {
    mockMe('admin')
    let savedPolicy: unknown
    let loadedProfilePath = ''
    let savedProfile: unknown
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.put('/fb/v1/console/public-visibility/policy', async ({ request }) => {
        savedPolicy = await request.json()
        return HttpResponse.json({ ...policyFixture(), ...(savedPolicy as object) })
      }),
      http.get('/fb/v1/console/public-visibility/requests/:requestId/profile', ({ request }) => {
        loadedProfilePath = new URL(request.url).pathname
        return HttpResponse.json(publicationFixture())
      }),
      http.get('/fb/v1/portal/:tenantSlug/requests/:publicSlug', () =>
        HttpResponse.json(publicRequestDetailFixture()),
      ),
      http.put(
        '/fb/v1/console/public-visibility/requests/:requestId/profile',
        async ({ request }) => {
          savedProfile = await request.json()
          return HttpResponse.json(publicationFixture())
        },
      ),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('公开策略')).toBeInTheDocument())
    expect(screen.getByText('审核队列')).toBeInTheDocument()
    expect(screen.getByText('公开需求资料')).toBeInTheDocument()
    expect(screen.getByText('门户投稿表单')).toBeInTheDocument()
    expect(await screen.findByText('路线图状态映射')).toBeInTheDocument()
    expect(screen.getByText('实时预览')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开公开看板' })).toHaveAttribute(
      'href',
      '/portal/tenant/requests',
    )
    expect(screen.getByRole('link', { name: '打开公开路线图' })).toHaveAttribute(
      'href',
      '/portal/tenant/roadmap',
    )
    expect(screen.getByRole('link', { name: '打开公开门户' })).toHaveAttribute(
      'href',
      '/portal/tenant',
    )
    expect(screen.getAllByLabelText('公开列名称')).toHaveLength(5)
    expect(screen.getAllByLabelText('列顺序')).toHaveLength(5)

    await user.click(await screen.findByRole('combobox', { name: '入口访问' }))
    await user.click(await screen.findByRole('option', { name: '关闭' }))
    await user.click(screen.getByRole('combobox', { name: '需求默认状态' }))
    await user.click(await screen.findByRole('option', { name: '已批准' }))
    await user.click(screen.getByRole('combobox', { name: '投稿写入' }))
    await user.click(await screen.findByRole('option', { name: '匿名' }))
    await user.click(screen.getByRole('combobox', { name: '评论写入' }))
    await user.click(await screen.findByRole('option', { name: '需身份' }))
    await user.click(screen.getByRole('combobox', { name: '投票写入' }))
    await user.click(await screen.findByRole('option', { name: '关闭' }))
    await user.click(screen.getByRole('combobox', { name: '评论默认状态' }))
    await user.click(await screen.findByRole('option', { name: '已批准' }))
    await user.click(screen.getByRole('combobox', { name: '提交者身份' }))
    await user.click(await screen.findByRole('option', { name: '组织名' }))
    await user.click(await screen.findByRole('checkbox', { name: '公开需求' }))
    await user.click(screen.getByRole('checkbox', { name: '显示投票数' }))
    await user.click(screen.getByRole('checkbox', { name: '显示评论数' }))
    await user.click(screen.getByRole('checkbox', { name: '显示提交者' }))
    await user.click(screen.getByRole('button', { name: '保存策略' }))
    await waitFor(() => {
      expect(savedPolicy).toMatchObject({
        portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED,
        defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
        defaultCommentState: ModerationState.MODERATION_STATE_APPROVED,
        submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
        commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
        voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION,
        requestsEnabled: false,
        showVoteCount: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        roadmapStatusMapping: [
          {
            status: 'open',
            label: 'under consideration',
            order: 1,
            included: true,
          },
          {
            status: 'planned',
            label: 'planned',
            order: 2,
            included: true,
          },
          {
            status: 'in_progress',
            label: 'in progress',
            order: 3,
            included: true,
          },
          {
            status: 'shipped',
            label: 'shipped',
            order: 4,
            included: true,
          },
          {
            status: 'cancelled',
            label: 'cancelled',
            order: 5,
            included: false,
          },
        ],
        portalSubmissionForm: {
          headline: 'Share feedback',
          description: 'Tell us what is broken, missing, or worth improving.',
          acknowledgement: 'Thanks. We will review your submission.',
          submitButtonLabel: 'Submit feedback',
          showPageUrl: true,
          fields: [
            {
              key: 'severity',
              label: 'Severity',
              kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
              required: true,
              options: ['low', 'medium', 'high'],
              placeholder: 'Choose a severity',
            },
          ],
        },
      })
    })

    await user.type(
      screen.getByPlaceholderText('粘贴 customer request UUID'),
      ` ${currentRequestID} `,
    )
    await user.click(screen.getByRole('button', { name: '载入' }))
    await waitFor(() => {
      expect(screen.getByText('当前 slug: billing-export')).toBeInTheDocument()
    })
    expect(screen.getByDisplayValue('Next')).toBeDisabled()
    await waitFor(() => {
      expect(screen.getByText('可能重复')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('Pricing dashboard')).toBeInTheDocument()
    })
    expect(screen.getByRole('link', { name: '打开客户需求' })).toHaveAttribute(
      'href',
      `/feedback/customer-requests?request_id=${currentRequestID}`,
    )
    expect(screen.getByRole('link', { name: '设为合并目标' })).toHaveAttribute(
      'href',
      `/feedback/customer-requests?request_id=${currentRequestID}&merge_target_id=${similarRequestID}`,
    )
    expect(screen.getByRole('link', { name: 'Pricing dashboard' })).toHaveAttribute(
      'href',
      '/portal/tenant/requests/pricing-dashboard',
    )
    expect(loadedProfilePath).toBe(
      `/fb/v1/console/public-visibility/requests/${currentRequestID}/profile`,
    )

    await user.clear(screen.getByPlaceholderText('面向客户展示的标题'))
    await user.type(screen.getByPlaceholderText('面向客户展示的标题'), 'Improved billing export')
    await user.clear(screen.getByPlaceholderText('只写可以公开展示的信息'))
    await user.type(screen.getByPlaceholderText('只写可以公开展示的信息'), 'Export invoices safely')
    await user.click(screen.getByRole('checkbox', { name: '进入公开路线图' }))
    await user.click(screen.getByRole('button', { name: '保存资料' }))
    await waitFor(() => {
      expect(savedProfile).toMatchObject({
        requestId: currentRequestID,
        publicSlug: 'billing-export',
        publicTitle: 'Improved billing export',
        publicSummary: 'Export invoices safely',
        includedInPortal: true,
        includedInRoadmap: false,
        submittedByDisplay: 'Jane Customer',
      })
    })
  }, 60_000)

  it('keeps roadmap mappings and portal field editors interactive', async () => {
    mockMe('admin')
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/views', () => HttpResponse.json({ views: [] })),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('路线图状态映射')).toBeInTheDocument())

    const mappingLabels = screen.getAllByLabelText('公开列名称')
    await user.clear(mappingLabels[0])
    await user.type(mappingLabels[0], '待定')

    const mappingOrders = screen.getAllByLabelText('列顺序')
    await user.clear(mappingOrders[0])
    await user.type(mappingOrders[0], '7')

    await user.click(screen.getAllByRole('checkbox', { name: '显示在公开路线图' })[0])
    await user.click(screen.getByRole('button', { name: '恢复默认映射' }))

    expect(screen.getAllByLabelText('公开列名称')[0]).toHaveValue('under consideration')
    expect(screen.getAllByLabelText('列顺序')[0]).toHaveValue(1)

    await user.click(screen.getByRole('button', { name: '添加字段' }))

    const fieldLabels = screen.getAllByLabelText('字段名称')
    await user.clear(fieldLabels[0])
    await user.type(fieldLabels[0], 'Severity level')

    const fieldPlaceholders = screen.getAllByLabelText('占位提示')
    await user.clear(fieldPlaceholders[0])
    await user.type(fieldPlaceholders[0], 'Choose severity')

    await user.click(screen.getAllByRole('button', { name: '下移' })[0])
    await user.click(screen.getAllByRole('button', { name: '删除' })[0])

    expect(screen.getAllByLabelText('字段名称')).toHaveLength(1)
  })

  it('saves and reapplies moderation views', async () => {
    mockMe('admin')
    type SavedViewBody = {
      name?: string
      state?: { queueView?: string; surfaces?: string[] }
    }
    let savedViewBody: SavedViewBody | null = null
    let savedViews = { views: [] as Record<string, unknown>[] }
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/views', () => HttpResponse.json(savedViews)),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.post('/fb/v1/console/public-visibility/views', async ({ request }) => {
        savedViewBody = (await request.json()) as SavedViewBody
        const view = {
          id: 'view-1',
          name: savedViewBody?.name ?? 'Approved portal requests',
          state: savedViewBody?.state,
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        }
        savedViews = { views: [view] }
        return HttpResponse.json({ view })
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('保存的视图')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '门户投稿' }))
    await user.click(screen.getByRole('button', { name: '已公开 (1)' }))
    await user.click(screen.getByRole('button', { name: '保存视图' }))
    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), 'Approved portal requests')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(savedViewBody).toMatchObject({
        name: 'Approved portal requests',
        state: {
          queueView: 'approved',
          surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
        },
      })
    })

    await user.click(screen.getByRole('combobox', { name: '保存的视图' }))
    expect(
      await screen.findByRole('option', { name: 'Approved portal requests' }),
    ).toBeInTheDocument()
  })

  it('loads, updates, and deletes an existing saved moderation view', async () => {
    mockMe('admin')
    type SavedViewState = {
      queueView: string
      surfaces: PublicSurface[]
    }
    type SavedViewRecord = {
      id: string
      name: string
      state: SavedViewState
      createdAt: string
      updatedAt: string
    }
    type SavedViewBody = {
      name?: string
      state?: SavedViewState
    }
    let savedViewBody: SavedViewBody | null = null
    let deletedViewID = ''
    let savedViews: { views: SavedViewRecord[] } = {
      views: [
        {
          id: 'view-1',
          name: 'Approved portal requests',
          state: {
            queueView: 'approved',
            surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
          },
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        },
      ],
    }

    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/views', () => HttpResponse.json(savedViews)),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.put('/fb/v1/console/public-visibility/views/:id', async ({ request, params }) => {
        savedViewBody = (await request.json()) as SavedViewBody
        const updated: SavedViewRecord = {
          id: params.id as string,
          name: savedViewBody?.name ?? 'Updated portal requests',
          state: savedViewBody?.state ?? {
            queueView: 'pending',
            surfaces: [],
          },
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-11T00:00:00Z',
        }
        savedViews = { views: [updated] }
        return HttpResponse.json({ view: updated })
      }),
      http.delete('/fb/v1/console/public-visibility/views/:id', ({ params }) => {
        deletedViewID = params.id as string
        savedViews = { views: [] }
        return HttpResponse.json({})
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('保存的视图')).toBeInTheDocument())
    expect(screen.getByText('当前筛选未绑定保存视图')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByRole('combobox', { name: '保存的视图' })).toBeEnabled())

    await user.click(screen.getByRole('combobox', { name: '保存的视图' }))
    await user.click(await screen.findByRole('option', { name: 'Approved portal requests' }))

    await waitFor(() => {
      expect(screen.getByText('已绑定为 Approved portal requests')).toBeInTheDocument()
      expect(screen.getByText('当前筛选与该保存视图一致。')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '待审 (1)' }))
    expect(screen.getByText('当前筛选已修改，尚未保存到该视图。')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '保存视图' }))
    expect(screen.getByLabelText('视图名称')).toHaveValue('Approved portal requests')

    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), 'Updated portal requests')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(savedViewBody).toMatchObject({
        name: 'Updated portal requests',
        state: {
          queueView: 'pending',
          surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
        },
      })
    })
    await waitFor(() => {
      expect(screen.getByText('已绑定为 Updated portal requests')).toBeInTheDocument()
      expect(screen.getByText('当前筛选与该保存视图一致。')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '删除保存视图' }))

    await waitFor(() => {
      expect(deletedViewID).toBe('view-1')
      expect(screen.getByText('当前筛选未绑定保存视图')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '删除保存视图' })).toBeDisabled()
  })

  it('keeps similar requests locked until the publication is public', async () => {
    mockMe('admin')
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({
          subjects: moderationSubjects(ModerationState.MODERATION_STATE_PENDING),
        }),
      ),
      http.get('/fb/v1/console/public-visibility/requests/:requestId/profile', () =>
        HttpResponse.json(publicationFixture(ModerationState.MODERATION_STATE_PENDING)),
      ),
      http.get('/fb/v1/portal/:tenantSlug/requests/:publicSlug', () => {
        throw new Error('portal similarity lookup must stay disabled until publication is public')
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('公开需求资料')).toBeInTheDocument())
    await user.type(
      screen.getByPlaceholderText('粘贴 customer request UUID'),
      ` ${currentRequestID} `,
    )
    await user.click(screen.getByRole('button', { name: '载入' }))

    await waitFor(() => {
      expect(screen.getByText('可能重复')).toBeInTheDocument()
      expect(screen.getByText('此需求公开后会自动显示相似请求。')).toBeInTheDocument()
    })
    expect(screen.queryByText('Pricing dashboard')).not.toBeInTheDocument()
  })

  it('saves and reapplies moderation views', async () => {
    mockMe('admin')
    type SavedViewBody = {
      name?: string
      state?: { queueView?: string; surfaces?: string[] }
    }
    let savedViewBody: SavedViewBody | null = null
    let savedViews = { views: [] as Record<string, unknown>[] }
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/views', () => HttpResponse.json(savedViews)),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.post('/fb/v1/console/public-visibility/views', async ({ request }) => {
        savedViewBody = (await request.json()) as SavedViewBody
        const view = {
          id: 'view-1',
          name: savedViewBody?.name ?? 'Approved portal requests',
          state: savedViewBody?.state,
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        }
        savedViews = { views: [view] }
        return HttpResponse.json({ view })
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('保存的视图')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '门户投稿' }))
    await user.click(screen.getByRole('button', { name: '已公开 (1)' }))
    await user.click(screen.getByRole('button', { name: '保存视图' }))
    await user.clear(screen.getByLabelText('视图名称'))
    await user.type(screen.getByLabelText('视图名称'), 'Approved portal requests')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => {
      expect(savedViewBody).toMatchObject({
        name: 'Approved portal requests',
        state: {
          queueView: 'approved',
          surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
        },
      })
    })

    await user.click(screen.getByRole('combobox', { name: '保存的视图' }))
    expect(
      await screen.findByRole('option', { name: 'Approved portal requests' }),
    ).toBeInTheDocument()
  })

  it('keeps similar requests locked until the publication is public', async () => {
    mockMe('admin')
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({
          subjects: moderationSubjects(ModerationState.MODERATION_STATE_PENDING),
        }),
      ),
      http.get('/fb/v1/console/public-visibility/requests/:requestId/profile', () =>
        HttpResponse.json(publicationFixture(ModerationState.MODERATION_STATE_PENDING)),
      ),
      http.get('/fb/v1/portal/:tenantSlug/requests/:publicSlug', () => {
        throw new Error('portal similarity lookup must stay disabled until publication is public')
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('公开需求资料')).toBeInTheDocument())
    await user.type(
      screen.getByPlaceholderText('粘贴 customer request UUID'),
      ` ${currentRequestID} `,
    )
    await user.click(screen.getByRole('button', { name: '载入' }))

    await waitFor(() => {
      expect(screen.getByText('可能重复')).toBeInTheDocument()
      expect(screen.getByText('此需求公开后会自动显示相似请求。')).toBeInTheDocument()
    })
    expect(screen.queryByText('Pricing dashboard')).not.toBeInTheDocument()
  })

  it('lets admins add, reorder, and normalize portal submission fields', async () => {
    mockMe('admin')
    let savedPolicy: unknown
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: [] }),
      ),
      http.put('/fb/v1/console/public-visibility/policy', async ({ request }) => {
        savedPolicy = await request.json()
        return HttpResponse.json({
          ...policyFixture(),
          ...(savedPolicy as object),
        })
      }),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('自定义字段')).toBeInTheDocument())
    await waitFor(() => expect(screen.getByLabelText('字段键')).toHaveValue('severity'))

    await user.click(screen.getByRole('button', { name: '添加字段' }))

    await waitFor(
      () => {
        expect(screen.getAllByLabelText('字段键')).toHaveLength(2)
      },
      { timeout: 5000 },
    )
    const fieldKeys = screen.getAllByLabelText('字段键')
    const fieldLabels = screen.getAllByLabelText('字段名称')
    const fieldPlaceholders = screen.getAllByLabelText('占位提示')
    const fieldRequired = screen.getAllByRole('checkbox', { name: '必填' })

    await user.type(fieldKeys[1], ' Extra_Field ')
    await user.type(fieldLabels[1], ' Extra Severity ')
    await user.type(fieldPlaceholders[1], ' Explain the issue ')
    await user.click(fieldRequired[1])

    fireEvent.change(screen.getByLabelText('可选项'), {
      target: { value: ' alpha\n\n beta \n gamma ' },
    })

    await user.click(screen.getAllByRole('button', { name: '上移' })[1])
    await user.click(screen.getAllByRole('button', { name: '删除' })[0])

    await user.click(screen.getByRole('button', { name: '保存策略' }))
    await waitFor(() => {
      expect(savedPolicy).toMatchObject({
        portalSubmissionForm: {
          fields: [
            {
              key: 'severity',
              label: 'Severity',
              kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
              required: true,
              placeholder: 'Choose a severity',
              options: ['alpha', 'beta', 'gamma'],
            },
          ],
        },
      })
    })
  })

  it('filters the moderation queue and posts an approval decision', async () => {
    let approvalBody: unknown
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.post(
        '/fb/v1/console/public-visibility/moderation/moderation-pending\\:approve',
        async ({ request }) => {
          approvalBody = await request.json()
          return HttpResponse.json(
            moderationSubject(
              'moderation-pending',
              'profile-pending',
              ModerationState.MODERATION_STATE_APPROVED,
            ),
          )
        },
      ),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('profile-pending')).toBeInTheDocument())
    expect(screen.queryByText('profile-approved')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '已公开 (1)' }))
    expect(screen.getByText('profile-approved')).toBeInTheDocument()
    expect(screen.queryByText('profile-pending')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '待审 (1)' }))
    await user.click(screen.getByRole('button', { name: '批准' }))
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: '批准审核项' })).toBeInTheDocument()
    })
    expect(screen.getAllByText('profile-pending').length).toBeGreaterThanOrEqual(1)
    await user.type(screen.getByPlaceholderText(/可选；只写处理背景/), 'Reviewed public copy')
    await user.click(screen.getByRole('button', { name: '提交审核' }))

    await waitFor(() => {
      expect(approvalBody).toEqual({
        reasonCode: 'operator.approved',
        reasonNote: 'Reviewed public copy',
      })
    })
  })

  it('posts enforcement decisions for approved and blocked queue items', async () => {
    let hideBody: unknown
    let spamBody: unknown
    let restoreBody: unknown
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policyFixture())),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
      http.post(
        '/fb/v1/console/public-visibility/moderation/moderation-approved\\:hide',
        async ({ request }) => {
          hideBody = await request.json()
          return HttpResponse.json(
            moderationSubject(
              'moderation-approved',
              'profile-approved',
              ModerationState.MODERATION_STATE_HIDDEN,
            ),
          )
        },
      ),
      http.post(
        '/fb/v1/console/public-visibility/moderation/moderation-hidden\\:mark-spam',
        async ({ request }) => {
          spamBody = await request.json()
          return HttpResponse.json(
            moderationSubject(
              'moderation-hidden',
              'profile-hidden',
              ModerationState.MODERATION_STATE_SPAM,
            ),
          )
        },
      ),
      http.post(
        '/fb/v1/console/public-visibility/moderation/moderation-hidden\\:restore',
        async ({ request }) => {
          restoreBody = await request.json()
          return HttpResponse.json(
            moderationSubject(
              'moderation-hidden',
              'profile-hidden',
              ModerationState.MODERATION_STATE_PENDING,
            ),
          )
        },
      ),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByRole('button', { name: '已公开 (1)' })).toBeEnabled())
    await user.click(screen.getByRole('button', { name: '已公开 (1)' }))
    await user.click(screen.getByRole('button', { name: '隐藏' }))
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: '隐藏审核项' })).toBeInTheDocument()
    })
    await user.type(screen.getByPlaceholderText(/可选；只写处理背景/), 'Internal-only detail')
    await user.click(screen.getByRole('button', { name: '提交审核' }))
    await waitFor(() => {
      expect(hideBody).toEqual({
        reasonCode: 'operator.hidden',
        reasonNote: 'Internal-only detail',
      })
    })

    await user.click(screen.getByRole('button', { name: '已拦截 (1)' }))
    await user.click(screen.getByRole('button', { name: '标记垃圾' }))
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: '标记垃圾审核项' })).toBeInTheDocument()
    })
    await user.type(screen.getByPlaceholderText(/可选；只写处理背景/), 'Repeated bot content')
    await user.click(screen.getByRole('button', { name: '提交审核' }))
    await waitFor(() => {
      expect(spamBody).toEqual({
        reasonCode: 'operator.spam',
        reasonNote: 'Repeated bot content',
      })
    })

    await user.click(screen.getByRole('button', { name: '恢复' }))
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: '恢复审核项' })).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '提交审核' }))
    await waitFor(() => {
      expect(restoreBody).toEqual({
        reasonCode: 'operator.restored',
        reasonNote: '',
      })
    })
  })

  it('shows triage controls without policy/profile editing for member users', async () => {
    mockMe('member')
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => {
        throw new Error('members must not fetch public policy')
      }),
      http.get('/fb/v1/console/public-visibility/moderation', () =>
        HttpResponse.json({ subjects: moderationSubjects() }),
      ),
    )

    const { user } = renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('profile-pending')).toBeInTheDocument())
    expect(screen.queryByText('公开策略')).not.toBeInTheDocument()
    expect(screen.queryByText('公开需求资料')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '批准' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '拒绝' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '标记垃圾' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '已公开 (1)' }))
    expect(screen.getByText('profile-approved')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '隐藏' })).not.toBeInTheDocument()
  })

  it('keeps policy and moderation API calls disabled for viewers', async () => {
    mockMe('viewer')
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', () => {
        throw new Error('viewers must not fetch public policy')
      }),
      http.get('/fb/v1/console/public-visibility/moderation', () => {
        throw new Error('viewers must not fetch moderation subjects')
      }),
    )

    renderWithProviders(<PublicVisibilityPage />)

    await waitFor(() => expect(screen.getByText('审核队列')).toBeInTheDocument())
    expect(screen.queryByText('公开策略')).not.toBeInTheDocument()
    expect(screen.queryByText('公开需求资料')).not.toBeInTheDocument()
    expect(screen.getByText('没有待显示的审核项')).toBeInTheDocument()
  })
})

function mockMe(role: 'admin' | 'delegated_admin' | 'member' | 'viewer') {
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: {
          id: 'tenant-1',
          name: 'Tenant',
          slug: 'tenant',
          locale: 'zh-CN',
          timezone: 'UTC',
        },
        user: { openId: `${role}-user`, name: role, role },
        csrfToken: 'csrf-test-token',
      }),
    ),
  )
}

function policyFixture(overrides: Partial<PublicVisibilityPolicy> = {}): PublicVisibilityPolicy {
  return {
    tenantId: 'tenant-1',
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
    searchIndexingEnabled: true,
    requestsEnabled: true,
    commentsEnabled: true,
    roadmapEnabled: true,
    changelogEnabled: false,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
    showVoteCount: true,
    showCommentCount: true,
    showSubmitterDisplay: true,
    hidePublicTimestamps: false,
    roadmapStatusMapping: [
      {
        status: 'open',
        label: 'under consideration',
        order: 1,
        included: true,
      },
      {
        status: 'planned',
        label: 'planned',
        order: 2,
        included: true,
      },
      {
        status: 'in_progress',
        label: 'in progress',
        order: 3,
        included: true,
      },
      {
        status: 'shipped',
        label: 'shipped',
        order: 4,
        included: true,
      },
      {
        status: 'cancelled',
        label: 'cancelled',
        order: 5,
        included: false,
      },
    ],
    portalSubmissionForm: {
      headline: 'Share feedback',
      description: 'Tell us what is broken, missing, or worth improving.',
      acknowledgement: 'Thanks. We will review your submission.',
      submitButtonLabel: 'Submit feedback',
      showPageUrl: true,
      fields: [
        {
          key: 'severity',
          label: 'Severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
          required: true,
          options: ['low', 'medium', 'high'],
          placeholder: 'Choose a severity',
        },
      ],
    },
    updatedBy: 'admin-1',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
    ...overrides,
  }
}

function moderationSubjects(
  state: ModerationState = ModerationState.MODERATION_STATE_PENDING,
): ModerationSubject[] {
  return [
    moderationSubject('moderation-pending', 'profile-pending', state),
    moderationSubject(
      'moderation-approved',
      'profile-approved',
      ModerationState.MODERATION_STATE_APPROVED,
      { reasonCode: 'operator.approved' },
    ),
    moderationSubject(
      'moderation-hidden',
      'profile-hidden',
      ModerationState.MODERATION_STATE_HIDDEN,
      { reasonCode: 'operator.hidden' },
    ),
  ]
}

function moderationSubject(
  id: string,
  subjectId: string,
  state: ModerationState,
  overrides: Partial<ModerationSubject> = {},
): ModerationSubject {
  return {
    id,
    tenantId: 'tenant-1',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    subjectId,
    state,
    reasonCode: '',
    reasonNote: '',
    submittedByDisplay: 'Jane Customer',
    reviewedBy: '',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
    ...overrides,
  }
}

function publicationFixture(
  moderationState: ModerationState = ModerationState.MODERATION_STATE_APPROVED,
): PublicRequestPublication {
  return {
    profile: {
      id: 'profile-1',
      tenantId: 'tenant-1',
      requestId: currentRequestID,
      publicSlug: 'billing-export',
      publicTitle: 'Billing export',
      publicSummary: 'Customers can export billing data.',
      publicState: 'planned',
      roadmapColumn: 'Next',
      includedInPortal: true,
      includedInRoadmap: true,
      updatedBy: 'admin-1',
      createdAt: '2026-07-10T00:00:00Z',
      updatedAt: '2026-07-10T00:00:00Z',
    },
    moderation: moderationSubject('moderation-pending', 'profile-pending', moderationState),
  }
}

function publicRequestDetailFixture(): PublicCustomerRequestDetail {
  return {
    request: publicRequestSummaryFixture('billing-export', 'Billing export'),
    links: ['/portal/tenant/requests/billing-export'],
    comments: [],
    canComment: false,
    similarRequests: [
      publicRequestSummaryFixture('pricing-dashboard', 'Pricing dashboard', {
        id: similarRequestID,
        summary: 'Show pricing comparisons in one place.',
        voteCount: 18,
        commentCount: 4,
        submittedByDisplay: 'Portal visitor',
        viewerHasVoted: true,
      }),
    ],
  }
}

function publicRequestSummaryFixture(
  slug: string,
  title: string,
  overrides: Partial<PublicCustomerRequestSummary> = {},
): PublicCustomerRequestSummary {
  return {
    id: `${slug}-id`,
    slug,
    title,
    summary: 'Customers want this.',
    state: 'planned',
    roadmapColumn: 'Next',
    voteCount: 12,
    commentCount: 3,
    submittedByDisplay: 'Jane Customer',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
    viewerHasVoted: false,
    ...overrides,
  }
}
