import { expect, type Page, type Route, test } from '@playwright/test'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '../../src/proto/attune/v1/public_visibility'
import type { GetMeResponse } from '../../src/proto/attune/v1/session'
import {
  collectConsoleDiagnostics,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  gotoConsoleRoute,
} from './helpers'

const zh = {
  accessMode: '\u5165\u53e3\u8bbf\u95ee',
  anonymous: '\u533f\u540d',
  approved: '\u5df2\u516c\u5f00',
  approvedState: '\u5df2\u6279\u51c6',
  approve: '\u6279\u51c6',
  blocked: '\u5df2\u62e6\u622a',
  close: '\u5173\u95ed',
  comments: '\u8bc4\u8bba\u5199\u5165',
  commentsToggle: '\u516c\u5f00\u8bc4\u8bba',
  currentSlug: '\u5f53\u524d slug',
  defaultState: '\u9700\u6c42\u9ed8\u8ba4\u72b6\u6001',
  hide: '\u9690\u85cf',
  hideTimestamps: '\u9690\u85cf\u516c\u5f00\u65f6\u95f4',
  identified: '\u9700\u8eab\u4efd',
  includedInRoadmap: '\u8fdb\u5165\u516c\u5f00\u8def\u7ebf\u56fe',
  indexing: '\u5141\u8bb8\u641c\u7d22\u7d22\u5f15',
  load: '\u8f7d\u5165',
  pending: '\u5f85\u5ba1',
  policyTitle: '\u516c\u5f00\u7b56\u7565',
  profileTitle: '\u516c\u5f00\u9700\u6c42\u8d44\u6599',
  publicSummaryPlaceholder: '\u53ea\u5199\u53ef\u4ee5\u516c\u5f00\u5c55\u793a\u7684\u4fe1\u606f',
  publicTitlePlaceholder: '\u9762\u5411\u5ba2\u6237\u5c55\u793a\u7684\u6807\u9898',
  publicVisibility: '\u516c\u5f00\u53ef\u89c1\u6027',
  reasonNotePlaceholder:
    '\u53ef\u9009\uff1b\u53ea\u5199\u5904\u7406\u80cc\u666f\uff0c\u4e0d\u7c98\u8d34\u539f\u59cb\u53cd\u9988\u6216\u654f\u611f\u4fe1\u606f\u3002',
  reject: '\u62d2\u7edd',
  requestsToggle: '\u516c\u5f00\u9700\u6c42',
  restore: '\u6062\u590d',
  roadmapToggle: '\u516c\u5f00\u8def\u7ebf\u56fe',
  savePolicy: '\u4fdd\u5b58\u7b56\u7565',
  saveProfile: '\u4fdd\u5b58\u8d44\u6599',
  spam: '\u6807\u8bb0\u5783\u573e',
  submitDecision: '\u63d0\u4ea4\u5ba1\u6838',
  submissions: '\u6295\u7a3f\u5199\u5165',
} as const

const requestID = '11111111-1111-4111-8111-111111111111'

test.describe('Public visibility browser behavior', () => {
  test('admin can save policy, publish a profile, and execute every moderation action', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const mock = await installPublicVisibilityMocks(page, { role: 'admin' })

    await gotoConsoleRoute(page, '/integrations/public-visibility')

    await expect(page.getByRole('heading', { level: 1, name: zh.publicVisibility })).toBeVisible()
    await expect(page.getByText(zh.policyTitle)).toBeVisible()
    await expect(page.getByText(zh.profileTitle)).toBeVisible()
    await expect(queueButton(page, zh.pending)).toHaveText(`${zh.pending} (2)`)
    await expect(queueButton(page, zh.approved)).toHaveText(`${zh.approved} (1)`)
    await expect(queueButton(page, zh.blocked)).toHaveText(`${zh.blocked} (1)`)

    await selectOption(page, zh.accessMode, zh.close)
    await selectOption(page, zh.defaultState, zh.approvedState)
    await selectOption(page, zh.submissions, zh.anonymous)
    await selectOption(page, zh.comments, zh.identified)
    await page.getByRole('checkbox', { exact: true, name: zh.requestsToggle }).click()
    await page.getByRole('checkbox', { exact: true, name: zh.commentsToggle }).click()
    await page.getByRole('checkbox', { exact: true, name: zh.roadmapToggle }).click()
    await page.getByRole('checkbox', { exact: true, name: zh.indexing }).click()
    await page.getByRole('checkbox', { exact: true, name: zh.hideTimestamps }).click()
    await page.getByRole('button', { name: zh.savePolicy }).click()

    await expect.poll(() => mock.policyUpdates.length).toBe(1)
    expect(mock.policyUpdates[0]).toMatchObject({
      portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED,
      defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
      submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
      commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
      requestsEnabled: false,
      commentsEnabled: false,
      roadmapEnabled: false,
      searchIndexingEnabled: true,
      hidePublicTimestamps: true,
    })

    await page.getByPlaceholder(/customer request UUID/).fill(` ${requestID} `)
    await page.getByRole('button', { name: zh.load }).click()
    await expect(page.getByText(`${zh.currentSlug}: billing-export`)).toBeVisible()
    expect(mock.profileReads).toEqual([requestID])

    await page.getByPlaceholder(zh.publicTitlePlaceholder).fill('Improved billing export')
    await page.getByPlaceholder(zh.publicSummaryPlaceholder).fill('Safe export of invoice data')
    await page.getByRole('checkbox', { name: zh.includedInRoadmap }).click()
    await page.getByRole('button', { name: zh.saveProfile }).click()

    await expect.poll(() => mock.profileUpdates.length).toBe(1)
    expect(mock.profileUpdates[0]).toMatchObject({
      requestId: requestID,
      publicSlug: 'billing-export',
      publicTitle: 'Improved billing export',
      publicSummary: 'Safe export of invoice data',
      includedInPortal: true,
      includedInRoadmap: false,
      submittedByDisplay: 'Ada Customer',
    })

    await moderateRow(page, 'profile-approve', zh.approve, 'copy reviewed for public portal')
    await moderateRow(page, 'profile-reject', zh.reject, 'contains private implementation detail')

    await queueButton(page, zh.approved).click()
    await moderateRow(page, 'profile-hide', zh.hide, 'published copy is now outdated')

    await queueButton(page, zh.blocked).click()
    await moderateRow(page, 'profile-restore', zh.spam, 'automated abuse pattern')
    await moderateRow(page, 'profile-restore', zh.restore, '')

    expect(mock.moderationActions).toEqual([
      {
        action: 'approve',
        body: { reasonCode: 'operator.approved', reasonNote: 'copy reviewed for public portal' },
        id: 'moderation-approve',
      },
      {
        action: 'reject',
        body: {
          reasonCode: 'operator.rejected',
          reasonNote: 'contains private implementation detail',
        },
        id: 'moderation-reject',
      },
      {
        action: 'hide',
        body: { reasonCode: 'operator.hidden', reasonNote: 'published copy is now outdated' },
        id: 'moderation-hide',
      },
      {
        action: 'mark-spam',
        body: { reasonCode: 'operator.spam', reasonNote: 'automated abuse pattern' },
        id: 'moderation-restore',
      },
      {
        action: 'restore',
        body: { reasonCode: 'operator.restored', reasonNote: '' },
        id: 'moderation-restore',
      },
    ])
    await queueButton(page, zh.approved).click()
    await expect(moderationRow(page, 'profile-restore')).toContainText(zh.approvedState)
    await expectNoDocumentOverflow(page)
    expect(mock.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('member sees moderation triage without policy/profile or enforcement controls', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const mock = await installPublicVisibilityMocks(page, { role: 'member' })

    await gotoConsoleRoute(page, '/integrations/public-visibility')

    await expect(page.getByRole('heading', { level: 1, name: zh.publicVisibility })).toBeVisible()
    await expect(page.getByText(zh.policyTitle)).toHaveCount(0)
    await expect(page.getByText(zh.profileTitle)).toHaveCount(0)
    await expect(moderationRow(page, 'profile-approve')).toBeVisible()
    await expect(
      moderationRow(page, 'profile-approve').getByRole('button', { name: zh.approve }),
    ).toBeVisible()
    await expect(
      moderationRow(page, 'profile-approve').getByRole('button', { name: zh.hide }),
    ).toHaveCount(0)

    expect(mock.policyReads).toBe(0)
    expect(mock.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })
})

async function selectOption(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label }).click()
  await page.getByRole('option', { name: option }).click()
}

async function moderateRow(page: Page, subjectID: string, action: string, note: string) {
  const row = moderationRow(page, subjectID)
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: action }).click()
  const dialogTitle = `${action}\u5ba1\u6838\u9879`
  await expect(page.getByRole('dialog', { name: dialogTitle })).toBeVisible()
  await page.getByPlaceholder(zh.reasonNotePlaceholder).fill(note)
  await page.getByRole('button', { name: zh.submitDecision }).click()
  await expect(page.getByRole('dialog', { name: dialogTitle })).toHaveCount(0)
}

function queueButton(page: Page, name: string) {
  return page.getByRole('button', { name: new RegExp(`^${escapeRegExp(name)} \\(`) })
}

function moderationRow(page: Page, subjectID: string) {
  return page.locator('li').filter({ hasText: subjectID })
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

type Role = 'admin' | 'member'

type PublicVisibilityMockOptions = {
  role: Role
}

type PublicVisibilityMock = {
  moderationActions: Array<{ action: string; body: unknown; id: string }>
  policyReads: number
  policyUpdates: unknown[]
  profileReads: string[]
  profileUpdates: unknown[]
  unhandledRequests: string[]
}

type PublicVisibilityMockState = PublicVisibilityMock & {
  policy: PublicVisibilityPolicy
  publications: Record<string, PublicRequestPublication>
  subjects: ModerationSubject[]
}

async function installPublicVisibilityMocks(
  page: Page,
  options: PublicVisibilityMockOptions,
): Promise<PublicVisibilityMock> {
  const state: PublicVisibilityMockState = {
    moderationActions: [],
    policy: policyFixture(),
    policyReads: 0,
    policyUpdates: [],
    profileReads: [],
    profileUpdates: [],
    publications: {
      [requestID]: publicationFixture(),
    },
    subjects: moderationSubjects(),
    unhandledRequests: [],
  }

  await page.route('**/fb/v1/console/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const path = url.pathname.slice('/fb/v1/console'.length) || '/'

    if (method === 'GET' && path === '/me') {
      await fulfillJson(route, meFixture(options.role))
      return
    }
    if (method === 'GET' && path === '/auth/providers') {
      await fulfillJson(route, { oidc_only: false, providers: [{ type: 'local' }] })
      return
    }
    if (method === 'GET' && path === '/public-visibility/policy') {
      state.policyReads += 1
      await fulfillJson(route, state.policy)
      return
    }
    if (method === 'PUT' && path === '/public-visibility/policy') {
      const body = readJsonBody(route)
      state.policyUpdates.push(body)
      state.policy = { ...state.policy, ...(body as object), updatedAt: now() }
      await fulfillJson(route, state.policy)
      return
    }
    if (method === 'GET' && path === '/public-visibility/moderation') {
      await fulfillJson(route, { subjects: state.subjects })
      return
    }

    const profileMatch = path.match(/^\/public-visibility\/requests\/([^/]+)\/profile$/)
    if (profileMatch && method === 'GET') {
      const id = decodeURIComponent(profileMatch[1])
      state.profileReads.push(id)
      await fulfillJson(route, state.publications[id] ?? {}, state.publications[id] ? 200 : 404)
      return
    }
    if (profileMatch && method === 'PUT') {
      const id = decodeURIComponent(profileMatch[1])
      const body = readJsonBody(route) as {
        includedInPortal?: boolean
        includedInRoadmap?: boolean
        publicSlug?: string
        publicSummary?: string
        publicTitle?: string
        requestId?: string
        submittedByDisplay?: string
      }
      state.profileUpdates.push(body)
      const publication = savedPublication(id, body)
      state.publications[id] = publication
      upsertSubject(state, publication.moderation)
      await fulfillJson(route, publication)
      return
    }

    const actionMatch = path.match(
      /^\/public-visibility\/moderation\/([^/]+):(approve|reject|hide|mark-spam|restore)$/,
    )
    if (actionMatch && method === 'POST') {
      const [, id, action] = actionMatch
      const body = readJsonBody(route)
      state.moderationActions.push({ action, body, id })
      const subject = updateSubjectForAction(state, id, action, body)
      await fulfillJson(route, subject)
      return
    }

    state.unhandledRequests.push(`${method} ${path}`)
    await fulfillJson(
      route,
      { message: `Unhandled public visibility mock: ${method} ${path}` },
      501,
    )
  })

  return state
}

function meFixture(role: Role): GetMeResponse {
  return {
    csrfToken: 'csrf-public-visibility',
    tenant: {
      id: 'tenant-public-visibility',
      locale: 'zh-CN',
      name: 'Public Visibility Tenant',
      slug: 'public-visibility',
      timezone: 'Asia/Singapore',
    },
    user: {
      name: role === 'admin' ? 'Public Visibility Admin' : 'Public Visibility Member',
      openId: `user-public-visibility-${role}`,
      role,
    },
  }
}

function policyFixture(): PublicVisibilityPolicy {
  return {
    changelogEnabled: true,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    commentsEnabled: true,
    createdAt: '2026-07-10T00:00:00Z',
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    hidePublicTimestamps: false,
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
    requestsEnabled: true,
    roadmapEnabled: true,
    searchIndexingEnabled: false,
    showCommentCount: true,
    showSubmitterDisplay: true,
    showVoteCount: true,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
    tenantId: 'tenant-public-visibility',
    updatedAt: '2026-07-10T00:00:00Z',
    updatedBy: 'user-public-visibility-admin',
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
  }
}

function moderationSubjects(): ModerationSubject[] {
  return [
    moderationSubject(
      'moderation-approve',
      'profile-approve',
      ModerationState.MODERATION_STATE_PENDING,
    ),
    moderationSubject(
      'moderation-reject',
      'profile-reject',
      ModerationState.MODERATION_STATE_PENDING,
    ),
    moderationSubject(
      'moderation-hide',
      'profile-hide',
      ModerationState.MODERATION_STATE_APPROVED,
      {
        reasonCode: 'seed.approved',
        reviewedBy: 'seed-admin',
        reviewedAt: '2026-07-10T00:05:00Z',
      },
    ),
    moderationSubject(
      'moderation-restore',
      'profile-restore',
      ModerationState.MODERATION_STATE_HIDDEN,
      {
        reasonCode: 'seed.hidden',
        reviewedBy: 'seed-admin',
        reviewedAt: '2026-07-10T00:06:00Z',
      },
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
    createdAt: '2026-07-10T00:00:00Z',
    id,
    reasonCode: '',
    reasonNote: '',
    reviewedBy: '',
    state,
    subjectId,
    submittedByDisplay: 'Ada Customer',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    tenantId: 'tenant-public-visibility',
    updatedAt: '2026-07-10T00:00:00Z',
    ...overrides,
  }
}

function publicationFixture(): PublicRequestPublication {
  return {
    moderation: moderationSubject(
      'moderation-loaded-profile',
      'profile-loaded',
      ModerationState.MODERATION_STATE_PENDING,
    ),
    profile: {
      createdAt: '2026-07-10T00:00:00Z',
      id: 'profile-loaded',
      includedInPortal: true,
      includedInRoadmap: true,
      publicSlug: 'billing-export',
      publicState: 'planned',
      publicSummary: 'Customers can export billing data.',
      publicTitle: 'Billing export',
      requestId: requestID,
      roadmapColumn: 'Next',
      tenantId: 'tenant-public-visibility',
      updatedAt: '2026-07-10T00:00:00Z',
      updatedBy: 'user-public-visibility-admin',
    },
  }
}

function savedPublication(
  id: string,
  body: {
    includedInPortal?: boolean
    includedInRoadmap?: boolean
    publicSlug?: string
    publicSummary?: string
    publicTitle?: string
    requestId?: string
    submittedByDisplay?: string
  },
): PublicRequestPublication {
  const subject = moderationSubject(
    'moderation-saved-profile',
    'profile-saved',
    ModerationState.MODERATION_STATE_PENDING,
    {
      submittedByDisplay: body.submittedByDisplay ?? '',
      updatedAt: now(),
    },
  )
  return {
    moderation: subject,
    profile: {
      createdAt: '2026-07-10T00:00:00Z',
      id: 'profile-saved',
      includedInPortal: body.includedInPortal === true,
      includedInRoadmap: body.includedInRoadmap === true,
      publicSlug: body.publicSlug ?? '',
      publicState: 'planned',
      publicSummary: body.publicSummary ?? '',
      publicTitle: body.publicTitle ?? '',
      requestId: id,
      roadmapColumn: 'Next',
      tenantId: 'tenant-public-visibility',
      updatedAt: now(),
      updatedBy: 'user-public-visibility-admin',
    },
  }
}

function upsertSubject(state: PublicVisibilityMockState, subject: ModerationSubject | undefined) {
  if (!subject) return
  const index = state.subjects.findIndex((candidate) => candidate.id === subject.id)
  if (index >= 0) {
    state.subjects[index] = subject
    return
  }
  state.subjects = [subject, ...state.subjects]
}

function updateSubjectForAction(
  state: PublicVisibilityMockState,
  id: string,
  action: string,
  body: unknown,
) {
  const subject = state.subjects.find((candidate) => candidate.id === id)
  if (!subject) {
    throw new Error(`unknown moderation subject ${id}`)
  }
  subject.state = stateForAction(action)
  subject.reasonCode = String((body as { reasonCode?: unknown } | null)?.reasonCode ?? '')
  subject.reasonNote = String((body as { reasonNote?: unknown } | null)?.reasonNote ?? '')
  subject.reviewedBy = 'user-public-visibility-admin'
  subject.reviewedAt = now()
  subject.updatedAt = now()
  return subject
}

function stateForAction(action: string): ModerationState {
  switch (action) {
    case 'approve':
    case 'restore':
      return ModerationState.MODERATION_STATE_APPROVED
    case 'reject':
      return ModerationState.MODERATION_STATE_REJECTED
    case 'hide':
      return ModerationState.MODERATION_STATE_HIDDEN
    case 'mark-spam':
      return ModerationState.MODERATION_STATE_SPAM
    default:
      throw new Error(`unknown moderation action ${action}`)
  }
}

function now() {
  return '2026-07-10T00:10:00Z'
}

function readJsonBody(route: Route): unknown {
  const body = route.request().postData()
  return body ? JSON.parse(body) : null
}

async function fulfillJson(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    body: JSON.stringify(body),
    contentType: 'application/json',
    status,
  })
}
