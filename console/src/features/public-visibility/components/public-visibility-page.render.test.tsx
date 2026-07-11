import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { PublicVisibilityPage } from '@/features/public-visibility/components/public-visibility-page'
import {
  ModerationState,
  type ModerationSubject,
  PortalSubmissionFieldKind,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

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
    expect(screen.getByText('实时预览')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '打开公开门户' })).toHaveAttribute(
      'href',
      '/portal/tenant',
    )

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

    await user.type(screen.getByPlaceholderText('粘贴 customer request UUID'), ' request-1 ')
    await user.click(screen.getByRole('button', { name: '载入' }))
    await waitFor(() => {
      expect(screen.getByText('当前 slug: billing-export')).toBeInTheDocument()
    })
    expect(loadedProfilePath).toBe('/fb/v1/console/public-visibility/requests/request-1/profile')

    await user.clear(screen.getByPlaceholderText('面向客户展示的标题'))
    await user.type(screen.getByPlaceholderText('面向客户展示的标题'), 'Improved billing export')
    await user.clear(screen.getByPlaceholderText('只写可以公开展示的信息'))
    await user.type(screen.getByPlaceholderText('只写可以公开展示的信息'), 'Export invoices safely')
    await user.click(screen.getByRole('checkbox', { name: '进入公开路线图' }))
    await user.click(screen.getByRole('button', { name: '保存资料' }))
    await waitFor(() => {
      expect(savedProfile).toMatchObject({
        requestId: 'request-1',
        publicSlug: 'billing-export',
        publicTitle: 'Improved billing export',
        publicSummary: 'Export invoices safely',
        includedInPortal: true,
        includedInRoadmap: false,
        submittedByDisplay: 'Jane Customer',
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

function moderationSubjects(): ModerationSubject[] {
  return [
    moderationSubject(
      'moderation-pending',
      'profile-pending',
      ModerationState.MODERATION_STATE_PENDING,
    ),
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

function publicationFixture(): PublicRequestPublication {
  return {
    profile: {
      id: 'profile-1',
      tenantId: 'tenant-1',
      requestId: 'request-1',
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
    moderation: moderationSubject(
      'moderation-pending',
      'profile-pending',
      ModerationState.MODERATION_STATE_PENDING,
    ),
  }
}
