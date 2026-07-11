import { expect, type Locator, type Page, test } from '@playwright/test'
import {
  collectConsoleDiagnostics,
  expectNoAxeViolations,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  expectOpaqueBackground,
  gotoConsoleRoute,
} from './helpers'
import { installConsoleApiMocks } from './route-mocks'

const zh = {
  apiKeys: '\u0041\u0050\u0049\u0020\u006b\u0065\u0079',
  apiKeyRevoked: '\u5df2\u64a4\u9500',
  auditLog: '\u5ba1\u8ba1\u65e5\u5fd7',
  currentPassword: '\u5f53\u524d\u5bc6\u7801',
  effectiveScopes: '\u751f\u6548\u6743\u9650',
  feedback: '\u53cd\u9988',
  feedbackRetryFailed: '\u91cd\u8bd5\u5931\u8d25\uff0c\u8bf7\u7a0d\u540e\u518d\u8bd5',
  feedbackRetryQueued: '\u5df2\u52a0\u5165\u91cd\u8bd5\u961f\u5217',
  gdpr: '\u0047\u0044\u0050\u0052\u0020\u6570\u636e\u8bf7\u6c42',
  gdprSubjectRequired:
    '\u8bf7\u8f93\u5165\u0020\u0073\u0075\u0062\u006a\u0065\u0063\u0074\u0020\u006b\u0065\u0079',
  mcpGrantsTable: '\u004d\u0043\u0050\u0020\u5237\u65b0\u6388\u6743\u5217\u8868',
  mcpGrantRevoked: '\u5237\u65b0\u6388\u6743\u5df2\u64a4\u9500',
  mcpClients: '\u004d\u0043\u0050\u0020\u5ba2\u6237\u7aef',
  mcpSessionsTable: '\u004d\u0043\u0050\u0020\u4f1a\u8bdd\u5217\u8868',
  mcpSessionRevoked: '\u4f1a\u8bdd\u5df2\u7ec8\u6b62',
  mcpToolsSaved: '\u5de5\u5177\u7b56\u7565\u5df2\u4fdd\u5b58',
  openNav: '\u6253\u5f00\u5bfc\u822a',
  outboxDead: '\u6b7b\u4fe1\u6295\u9012',
  outboxRetryQueued:
    '\u5df2\u91cd\u65b0\u6392\u961f\uff0c\u0077\u006f\u0072\u006b\u0065\u0072\u0020\u5c06\u5728\u4e0b\u6b21\u8f6e\u8be2\u65f6\u6295\u9012\u3002',
  publicVisibility: '\u516c\u5f00\u53ef\u89c1\u6027',
  retry: '\u91cd\u8bd5',
  retryDelivery: '\u91cd\u8bd5\u6295\u9012',
  retryEnrichment: '\u91cd\u8bd5\u5bcc\u5316',
  revoke: '\u64a4\u9500',
  revokeGrant: '\u64a4\u9500\u6388\u6743',
  revokeKeyDialog: '\u64a4\u9500\u8fd9\u628a\u0020\u006b\u0065\u0079\uff1f',
  revokeSession: '\u7ec8\u6b62',
  saveToolPolicies: '\u4fdd\u5b58\u5de5\u5177\u7b56\u7565',
  searchFeedback: '\u641c\u7d22\u53cd\u9988\u5185\u5bb9',
  semanticFallbackTitle: '\u8bed\u4e49\u641c\u7d22\u5df2\u964d\u7ea7',
  semanticSearch: '\u8bed\u4e49',
  runSemanticSearch: '\u8fd0\u884c\u8bed\u4e49\u641c\u7d22',
  evidenceLabel: '\u5339\u914d\u4f9d\u636e',
  externalSync: '\u5916\u90e8\u540c\u6b65',
  showScopes: '\u67e5\u770b\u751f\u6548\u6743\u9650',
  submitApiKey: '\u65b0\u5efa',
  secretDialog: '\u4f60\u7684\u65b0\u0020\u006b\u0065\u0079',
  signApiKey: '\u7b7e\u53d1\u65b0\u0020\u006b\u0065\u0079',
  signApiKeyDialog: '\u7b7e\u53d1\u65b0\u0020\u0041\u0050\u0049\u0020\u006b\u0065\u0079',
  stepUp: '\u5b8c\u6210\u4e8c\u6b21\u9a8c\u8bc1',
  stepUpConfirm: '\u9a8c\u8bc1\u5e76\u7ee7\u7eed',
  stepUpDialog: '\u786e\u8ba4\u654f\u611f\u64cd\u4f5c',
  stepUpSuccess: '\u4e8c\u6b21\u9a8c\u8bc1\u5df2\u5b8c\u6210',
  terminalFailures: '\u7ec8\u6001\u5931\u8d25\u5de5\u4f4d',
  useLabel: '\u7528\u9014\u5907\u6ce8',
  viewDetails: '\u67e5\u770b\u8be6\u60c5',
  denyTool: '\u963b\u6b62\u5de5\u5177',
  exportZip: '\u5bfc\u51fa\u0020\u005a\u0049\u0050',
  replyDraft: '\u56de\u590d\u8349\u7a3f',
  replyDraftAi: '\u0041\u0049\u0020\u5efa\u8bae',
  replyDraftHuman: '\u4eba\u5de5\u7f16\u8f91',
  replyDraftSentText: '\u5df2\u53d1\u9001\u6587\u672c',
  replyDraftEdited: '\u5df2\u7f16\u8f91',
  replyDraftApprovedStatus: '\u5df2\u6279\u51c6',
  replyDraftSentStatus: '\u5df2\u53d1\u9001',
  replyDraftEdit: '\u7f16\u8f91',
  replyDraftSave: '\u4fdd\u5b58',
  replyDraftSaved: '\u8349\u7a3f\u5df2\u4fdd\u5b58',
  replyDraftApprove: '\u6279\u51c6',
  replyDraftApproved: '\u8349\u7a3f\u5df2\u6279\u51c6',
  replyDraftSend: '\u53d1\u9001',
  replyDraftSent: '\u56de\u590d\u5df2\u53d1\u9001',
  replyDraftHistory: '\u7f16\u8f91\u5386\u53f2',
  replyDraftPreflight: '\u786e\u8ba4\u53d1\u9001\u56de\u590d',
  replyDraftConfirmSend: '\u786e\u8ba4\u53d1\u9001',
  replyDraftEvidence: '\u8bc1\u636e',
  replyDraftFinalText: '\u6700\u7ec8\u53d1\u9001\u6587\u672c',
  replyDraftDiff: '\u53d8\u66f4\u5bf9\u6bd4',
  replySendHook: '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b',
  replySendHookSaved:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u5df2\u4fdd\u5b58',
  replySendHookDisabled:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u5df2\u505c\u7528',
  replySendHookDisable: '\u505c\u7528\u0020\u0048\u006f\u006f\u006b',
  replySendHookDisableConfirm: '\u786e\u8ba4\u505c\u7528',
  replySendHookDisableDialog:
    '\u505c\u7528\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\uff1f',
  replySendHookName: '\u540d\u79f0',
  replySendHookContract: '\u6295\u9012\u5951\u7ea6',
  replySendHookDelivery: '\u6700\u8fd1\u6295\u9012',
  replySendHookHealthAttention: '\u6700\u8fd1\u6295\u9012\u9700\u8981\u5904\u7406',
  replySendHookSecurity: '\u5b89\u5168\u68c0\u67e5',
  replySendHookTest: '\u6d4b\u8bd5\u0020\u0048\u006f\u006f\u006b',
  replySendHookTestAccepted:
    '\u6d4b\u8bd5\u6295\u9012\u5df2\u88ab\u0020\u0048\u006f\u006f\u006b\u0020\u63a5\u6536',
  replySendHookRedeliver: '\u91cd\u653e\u6295\u9012',
  replySendHookRedelivered: '\u6295\u9012\u5df2\u91cd\u653e\u5e76\u88ab\u63a5\u6536',
  replySendHookPayloadLabel:
    '\u56de\u590d\u53d1\u9001\u0020\u0048\u006f\u006f\u006b\u0020\u793a\u4f8b\u0020\u0070\u0061\u0079\u006c\u006f\u0061\u0064',
  replySendHookOneTimeSecret: '\u4e00\u6b21\u6027\u0020\u0073\u0065\u0063\u0072\u0065\u0074',
  replySendHookURLHttpsError: '回复发送 Hook 只接受 HTTPS URL；本地测试可使用 loopback HTTP。',
  webhookUrl: '\u0057\u0065\u0062\u0068\u006f\u006f\u006b\u0020\u0055\u0052\u004c',
}

const apiKeyCreateFailure = 'Create key failed for accessibility gate'
const apiKeyRevokeFailure = 'Revoke key failed for accessibility gate'
const feedbackRetryFailure = 'Retry enrichment failed for accessibility gate'
const mcpToolPolicyFailure = 'Tool policy save failed for accessibility gate'
const outboxRetryFailure = 'Retry delivery conflict for accessibility gate'
const badRequestResourceConsole =
  'Failed to load resource: the server responded with a status of 400'
const conflictResourceConsole = 'Failed to load resource: the server responded with a status of 409'
const serverErrorResourceConsole =
  'Failed to load resource: the server responded with a status of 500'

const routes = [
  { path: '/feedback', title: zh.feedback, heading: zh.feedback },
  { path: '/feedback/terminal-failures', title: zh.terminalFailures, heading: zh.feedback },
  { path: '/integrations/api-keys', title: zh.apiKeys, heading: zh.apiKeys },
  { path: '/integrations/external-sync', title: zh.externalSync, heading: zh.externalSync },
  {
    path: '/integrations/reply-send-hook',
    title: zh.replySendHook,
    heading: zh.replySendHook,
  },
  {
    path: '/integrations/public-visibility',
    title: zh.publicVisibility,
    heading: zh.publicVisibility,
  },
  { path: '/mcp-clients', title: zh.mcpClients, heading: zh.mcpClients },
  { path: '/administration/gdpr', title: zh.gdpr, heading: zh.gdpr },
  { path: '/administration/dead-deliveries', title: zh.outboxDead, heading: zh.outboxDead },
  { path: '/administration/audit-log', title: zh.auditLog, heading: zh.auditLog },
] as const

const redirectAliases = [
  {
    path: '/api-keys',
    canonicalPath: '/integrations/api-keys',
    title: zh.apiKeys,
    heading: zh.apiKeys,
  },
  {
    path: '/outbox-dead',
    canonicalPath: '/administration/dead-deliveries',
    title: zh.outboxDead,
    heading: zh.outboxDead,
  },
] as const

const shellNavigationCases = [
  {
    linkName: zh.mcpClients,
    canonicalPath: '/mcp-clients',
    title: zh.mcpClients,
    heading: zh.mcpClients,
  },
  {
    linkName: zh.outboxDead,
    canonicalPath: '/administration/dead-deliveries',
    title: zh.outboxDead,
    heading: zh.outboxDead,
  },
] as const

const stressWidths = [320, 280] as const
const routeChurnCycles = 3
const sheetCycles = 3
const wcagTextSpacingStyle = `
  * {
    line-height: 1.5 !important;
    letter-spacing: 0.12em !important;
    word-spacing: 0.16em !important;
  }

  p {
    margin-bottom: 2em !important;
  }
`
const textZoomStyle = `
  html {
    font-size: 200% !important;
  }
`

test.describe('Console accessibility browser gate', () => {
  for (const routeCase of routes) {
    test(`${routeCase.path} has a clean accessible render`, async ({ page }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, routeCase.path)

      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  for (const routeCase of redirectAliases) {
    test(`${routeCase.path} redirects to its canonical accessible page`, async ({ page }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, routeCase.path)

      await expect(page).toHaveURL(new RegExp(`/console${escapeRegExp(routeCase.canonicalPath)}$`))
      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  for (const routeCase of shellNavigationCases) {
    test(`shell navigation reaches ${routeCase.canonicalPath} with accessible state intact`, async ({
      page,
    }) => {
      const diagnostics = collectConsoleDiagnostics(page)
      const apiMocks = await installConsoleApiMocks(page)

      await gotoConsoleRoute(page, '/feedback')
      await clickShellNav(page, routeCase.linkName)

      await expect(page).toHaveURL(new RegExp(`/console${escapeRegExp(routeCase.canonicalPath)}$`))
      await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
      await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
      expect(apiMocks.unhandledRequests).toEqual([])
      await expectNoConsoleDiagnostics(diagnostics)
    })
  }

  test('critical routes stay contained at narrow mobile widths', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    for (const width of stressWidths) {
      await page.setViewportSize({ width, height: 568 })

      for (const routeCase of routes) {
        await gotoConsoleRoute(page, routeCase.path)
        await expectRouteShellState(page, routeCase)
        await expectNoDocumentOverflow(page)
        await expectNoAxeViolations(page)
      }
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes preserve accessible structure in forced-colors mode', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.emulateMedia({ forcedColors: 'active' })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes tolerate WCAG text-spacing overrides', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await page.addStyleTag({ content: wcagTextSpacingStyle })
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
      await expectNoAxeViolations(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('critical routes preserve reflow with 200 percent text sizing', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1280, height: 720 })

    for (const routeCase of routes) {
      await gotoConsoleRoute(page, routeCase.path)
      await page.addStyleTag({ content: textZoomStyle })
      await expectRouteShellState(page, routeCase)
      await expectNoDocumentOverflow(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('route churn keeps shell landmarks, titles, and dialogs clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    for (let cycle = 0; cycle < routeChurnCycles; cycle += 1) {
      for (const routeCase of routes) {
        await gotoConsoleRoute(page, `${routeCase.path}?routeChurn=${cycle}`)
        await expectRouteShellState(page, routeCase)
        await expectNoDocumentOverflow(page)
      }
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('GDPR subject input contains long identifiers at narrow width', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/administration/gdpr')

    const subjectKey = `edge-${'x'.repeat(480)}@example.com`
    const subjectInput = page.getByTestId('gdpr-subject-key')
    await subjectInput.fill(subjectKey)

    await expect(subjectInput).toHaveValue(subjectKey)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key dialog cycles restore focus without leaking dialogs', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/integrations/api-keys')

    const createButton = page.getByRole('button', {
      name: new RegExp(escapeRegExp(zh.signApiKey)),
    })

    for (let cycle = 0; cycle < 6; cycle += 1) {
      await createButton.click()
      await expect(
        page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.signApiKeyDialog)) }),
      ).toBeVisible()
      if (cycle === 0) {
        await expectNoAxeViolations(page)
      }
      await expectNoDocumentOverflow(page)

      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog')).toHaveCount(0)
      await expectDomFocus(createButton)
      await expectNoDocumentOverflow(page)
    }

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback detail sheet survives repeated open-close cycles', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    await gotoConsoleRoute(page, '/feedback')
    const feedbackOpeners = page.getByRole('button', { name: /#feedback-101/ })
    expect(await feedbackOpeners.count()).toBeGreaterThan(0)
    await cycleSheet(page, feedbackOpeners.nth(0), sheetCycles)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback tags do not offer duplicate create CTA for assigned names', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')

    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()
    const sheet = page.getByRole('dialog')
    await expect(sheet).toContainText('Focus lost after detail close')
    await expect(sheet.getByRole('button', { name: '移除标签 accessibility' })).toBeVisible()

    await sheet.getByRole('button', { name: '添加' }).click()
    const tagSearch = page.getByPlaceholder('搜索标签…')
    await expect(tagSearch).toBeVisible()
    await tagSearch.fill('accessibility')
    const listbox = page.getByRole('listbox')
    await expect(listbox).toContainText('标签「accessibility」已添加到当前反馈')
    await expect(page.getByRole('option', { name: '创建「accessibility」' })).toHaveCount(0)

    await tagSearch.press('Escape')
    await expect(page.getByRole('listbox')).toHaveCount(0)

    await sheet.getByRole('button', { name: '移除标签 accessibility' }).click()
    await expect(sheet.getByText('暂无标签')).toBeVisible()

    await sheet.getByRole('button', { name: '添加' }).click()
    await page.getByPlaceholder('搜索标签…').fill('accessibility')
    await expect(page.getByRole('option', { name: 'accessibility' })).toBeVisible()
    await expect(page.getByRole('option', { name: '创建「accessibility」' })).toHaveCount(0)
    await page.getByRole('option', { name: 'accessibility' }).click()
    await expect(sheet.getByRole('button', { name: '移除标签 accessibility' })).toBeVisible()

    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('reply draft review workflow edits, approves, and sends a guarded revision', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')
    await page
      .getByRole('button', { name: /#feedback-101/ })
      .first()
      .click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: zh.replyDraft })).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftAi)).toBeVisible()
    const evidencePanel = page.getByTestId('reply-draft-evidence-panel')
    await expect(evidencePanel).toBeVisible()
    await expect(evidencePanel.getByText(zh.replyDraftEvidence, { exact: true })).toBeVisible()
    await expectOpaqueBackground(page, '[data-slot="sheet-content"]', 'feedback detail sheet')
    await expectOpaqueBackground(page, '[data-testid="reply-draft-surface"]', 'reply draft surface')

    await dialog.getByRole('button', { name: zh.replyDraftEdit }).click()
    const editedReply = 'Human edited reply from the browser gate before approval.'
    await dialog.locator('textarea').first().fill(editedReply)
    await dialog.getByRole('button', { name: zh.replyDraftSave }).click()
    await expectToastStatus(page, zh.replyDraftSaved)
    await expect(dialog.getByText(zh.replyDraftEdited)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftHuman)).toBeVisible()
    await expect(dialog.getByText(editedReply).first()).toBeVisible()

    await dialog.getByRole('button', { name: zh.replyDraftApprove }).click()
    await expectToastStatus(page, zh.replyDraftApproved)
    await expect(dialog.getByText(zh.replyDraftApprovedStatus, { exact: true })).toBeVisible()

    await dialog.getByRole('button', { name: zh.replyDraftSend }).click()
    const preflight = page.getByRole('dialog', { name: zh.replyDraftPreflight })
    await expect(preflight).toBeVisible()
    await expect(preflight.getByText(zh.replyDraftFinalText)).toBeVisible()
    await expect(preflight.getByText(editedReply)).toBeVisible()
    await preflight.getByRole('button', { name: zh.replyDraftConfirmSend }).click()
    await expectToastStatus(page, zh.replyDraftSent)
    await expect(dialog.getByText(zh.replyDraftSentStatus, { exact: true }).first()).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftSentText)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftHistory)).toBeVisible()
    await expect(dialog.getByText(zh.replyDraftDiff)).toBeVisible()

    expect(apiMocks.replyDraftRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/edit',
        body: { content: editedReply, expectedRevision: '1' },
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/approve',
        body: { expectedRevision: '2' },
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/feedback/feedback-101/reply-draft/send',
        body: { expectedRevision: '3' },
        idempotencyKey: expect.stringMatching(/.+/),
      }),
    ])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('reply send hook page saves and disables the controlled send endpoint', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/integrations/reply-send-hook')

    await expectOpaqueBackground(page, 'body', 'document body')
    await expectOpaqueBackground(page, '#root', 'app root')
    await expectOpaqueBackground(page, 'main', 'console main')
    await expect(page.getByRole('heading', { level: 1, name: zh.replySendHook })).toBeVisible()
    await expect(page.getByText('hooks.example.com').first()).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-health')).toBeVisible()
    await expect(page.getByText(zh.replySendHookHealthAttention)).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-contract')).toBeVisible()
    await expect(page.getByText(zh.replySendHookContract)).toBeVisible()
    await expect(page.getByTestId('reply-send-hook-deliveries')).toBeVisible()
    await expect(page.getByText(zh.replySendHookDelivery).first()).toBeVisible()
    await expect(
      page.getByTestId('reply-send-hook-deliveries').getByText('receiver returned 500'),
    ).toBeVisible()
    await expect(page.getByText('X-Attune-Signature')).toBeVisible()
    await expect(page.getByText('X-Attune-Timestamp')).toBeVisible()
    await expect(page.getByLabel(zh.replySendHookPayloadLabel)).toHaveValue(
      /"event_type": "reply\.send"/,
    )
    await expect(page.getByText(zh.replySendHookSecurity)).toBeVisible()
    await page.getByRole('button', { name: zh.replySendHookTest }).first().click()
    await expectToastStatus(page, zh.replySendHookTestAccepted)
    await page.getByRole('button', { name: new RegExp(zh.replySendHookRedeliver) }).click()
    await expectToastStatus(page, zh.replySendHookRedelivered)
    await page.getByLabel(zh.replySendHookName).fill('Browser reply bridge')
    await page.getByLabel(zh.webhookUrl).fill('http://support.example.com/attune/replies')
    await expect(page.getByText(zh.replySendHookURLHttpsError)).toBeVisible()
    await expect(page.getByRole('button', { name: zh.replyDraftSave })).toBeDisabled()
    await page.getByLabel(zh.webhookUrl).fill('http://127.0.0.1:4174/attune/replies')
    await expect(page.getByText(zh.replySendHookURLHttpsError)).toBeHidden()
    await expect(page.getByRole('button', { name: zh.replyDraftSave })).toBeEnabled()
    await page.getByLabel(zh.webhookUrl).fill('https://support.example.com/attune/replies')
    await page.getByRole('button', { name: zh.replyDraftSave }).click()
    await expectToastStatus(page, zh.replySendHookSaved)
    await expect(page.getByText(zh.replySendHookOneTimeSecret, { exact: true })).toBeVisible()
    await expect(page.getByText('generated_reply_secret_a11y_123456')).toBeVisible()

    await page.getByRole('button', { name: zh.replySendHookDisable }).click()
    await expect(
      page.getByRole('alertdialog', { name: zh.replySendHookDisableDialog }),
    ).toBeVisible()
    await page.getByRole('button', { name: zh.replySendHookDisableConfirm }).click()
    await expectToastStatus(page, zh.replySendHookDisabled)
    await expect(
      page.getByRole('alertdialog', { name: zh.replySendHookDisableDialog }),
    ).toHaveCount(0)
    await expect(page.locator('.min-h-screen[aria-hidden="true"]')).toHaveCount(0)
    await expect(page.getByText(zh.replySendHookDisabled)).toBeVisible()

    expect(apiMocks.replySendHookRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/reply-send-hook/test',
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/reply-send-hook/deliveries/reply-delivery-a11y-failed/redeliver',
      }),
      expect.objectContaining({
        method: 'PUT',
        path: '/reply-send-hook',
        body: {
          enabled: true,
          name: 'Browser reply bridge',
          url: 'https://support.example.com/attune/replies',
        },
      }),
      expect.objectContaining({
        method: 'DELETE',
        path: '/reply-send-hook',
        body: null,
      }),
    ])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('audit detail sheet survives repeated open-close cycles', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })

    await gotoConsoleRoute(page, '/administration/audit-log')
    const auditOpeners = page.getByRole('button', { name: zh.viewDetails })
    expect(await auditOpeners.count()).toBeGreaterThan(0)
    await cycleSheet(page, auditOpeners.nth(0), sheetCycles)

    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('terminal workbench jump links target every failure dimension', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 320, height: 568 })
    await gotoConsoleRoute(page, '/feedback/terminal-failures')

    const anchorRail = page.locator('a[href^="#terminal-workbench-"]')
    await expect(anchorRail).toHaveCount(4)

    for (let index = 0; index < 4; index += 1) {
      const anchor = anchorRail.nth(index)
      const href = await anchor.getAttribute('href')
      expect(href).toBeTruthy()

      await anchor.click()
      await expect(page).toHaveURL(new RegExp(`${escapeRegExp(href ?? '')}$`))
      await expect(page.locator(`[id="${(href ?? '').replace(/^#/, '')}"]`)).toBeVisible()
      await expectNoDocumentOverflow(page)
    }

    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('semantic feedback search keeps terminal scope and opens returned details', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 390, height: 844 })
    await gotoConsoleRoute(page, '/feedback/terminal-failures')

    await page.getByRole('button', { name: zh.semanticSearch, exact: true }).click()
    await page.getByRole('searchbox', { name: zh.searchFeedback }).fill('terminal upstream retries')
    await page.getByRole('button', { name: zh.runSemanticSearch }).click()

    await expect(page.getByText('rrf.pgfts.v1.k60')).toBeVisible()
    await expect(page.getByText(zh.evidenceLabel)).toBeVisible()
    await expect(page.getByRole('button', { name: /#feedback-201/ }).first()).toBeVisible()

    const request = apiMocks.semanticSearchRequests.at(-1) as {
      q?: string
      limit?: number
      filter?: { enrichmentStatus?: string; terminalFailedOnly?: boolean }
    }
    expect(request.q).toBe('terminal upstream retries')
    expect(request.limit).toBe(50)
    expect(request.filter?.enrichmentStatus).toBe('failed')
    expect(request.filter?.terminalFailedOnly).toBe(true)

    await page
      .getByRole('button', { name: /#feedback-201/ })
      .first()
      .click()
    await expect(page.getByRole('dialog')).toContainText('Terminal enrichment failure')
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('semantic feedback search fallback state stays visible and contained', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await page.setViewportSize({ width: 1365, height: 768 })
    await gotoConsoleRoute(page, '/feedback')

    await page.getByRole('button', { name: zh.semanticSearch, exact: true }).click()
    await page.getByRole('searchbox', { name: zh.searchFeedback }).fill('fallback coverage check')
    await page.getByRole('button', { name: zh.runSemanticSearch }).click()

    await expect(page.getByText(zh.semanticFallbackTitle)).toBeVisible()
    await expect(page.getByText(zh.evidenceLabel)).toBeVisible()
    const request = apiMocks.semanticSearchRequests.at(-1) as { q?: string; limit?: number }
    expect(request.q).toBe('fallback coverage check')
    expect(request.limit).toBe(50)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key creation dialogs remain accessible through the secret reveal', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: new RegExp(escapeRegExp(zh.signApiKey)) }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.signApiKeyDialog)) }),
    ).toBeVisible()
    const scopeDisclosure = page.getByRole('button', { name: zh.showScopes })
    const effectiveScopes = page.locator('#api-key-effective-scopes')
    await expect(scopeDisclosure).toHaveAttribute('aria-expanded', 'false')
    await expect(scopeDisclosure).toHaveAttribute('aria-controls', 'api-key-effective-scopes')
    await expect(effectiveScopes).toBeHidden()

    await scopeDisclosure.click()
    await expect(scopeDisclosure).toHaveAttribute('aria-expanded', 'true')
    await expect(effectiveScopes).toBeVisible()
    await expect(effectiveScopes).toHaveAccessibleName(zh.effectiveScopes)
    await expectNoAxeViolations(page)

    await page.getByRole('textbox', { name: zh.useLabel }).fill('browser-a11y')
    await page.getByRole('button', { name: zh.submitApiKey }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.secretDialog)) }),
    ).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key creation errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { apiKeyCreate: apiKeyCreateFailure },
    })

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: new RegExp(escapeRegExp(zh.signApiKey)) }).click()
    await page.getByRole('textbox', { name: zh.useLabel }).fill('browser-a11y')
    await page.getByRole('button', { name: zh.submitApiKey }).click()
    await expectToastStatus(page, apiKeyCreateFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/api-keys'],
      [badRequestResourceConsole, apiKeyCreateFailure],
    )
  })

  test('API key revoke dialog reports success through an accessible status', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/api-keys')
    const revokeButton = page.getByRole('button', { name: /ci-accessibility/ })
    await revokeButton.click()
    const revokeDialog = page.getByRole('dialog', { name: zh.revokeKeyDialog })
    await expect(revokeDialog).toBeVisible()
    await expectNoAxeViolations(page)

    await revokeDialog.getByRole('button', { name: zh.revoke }).click()
    await expect(revokeDialog).toBeHidden()
    await expectToastStatus(page, zh.apiKeyRevoked)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('API key revoke errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { apiKeyRevoke: apiKeyRevokeFailure },
    })

    await gotoConsoleRoute(page, '/integrations/api-keys')
    await page.getByRole('button', { name: /ci-accessibility/ }).click()
    const revokeDialog = page.getByRole('dialog', { name: zh.revokeKeyDialog })
    await expect(revokeDialog).toBeVisible()
    await revokeDialog.getByRole('button', { name: zh.revoke }).click()
    await expectToastStatus(page, apiKeyRevokeFailure)
    await expect(revokeDialog).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/api-keys/key-a11y'],
      [conflictResourceConsole, apiKeyRevokeFailure],
    )
  })

  test('feedback retry-enrichment status messages are accessible', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/feedback/terminal-failures')
    await page.getByRole('button', { name: `${zh.retryEnrichment} #feedback-201` }).click()
    await expectToastStatus(page, zh.feedbackRetryQueued)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('feedback retry-enrichment errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { feedbackRetryEnrichment: feedbackRetryFailure },
    })

    await gotoConsoleRoute(page, '/feedback/terminal-failures')
    await page.getByRole('button', { name: `${zh.retryEnrichment} #feedback-201` }).click()
    await expectToastStatus(page, zh.feedbackRetryFailed)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/feedback/feedback-201'],
      [serverErrorResourceConsole],
    )
  })

  test('MCP tool policy controls are keyboard-addressable and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/mcp-clients')
    await page
      .getByRole('checkbox', {
        name: new RegExp(`${escapeRegExp(zh.denyTool)}\\s+list_feedback`),
      })
      .click()
    await page.getByRole('button', { name: zh.saveToolPolicies }).click()
    await expectToastStatus(page, zh.mcpToolsSaved)
    await expect(page.getByRole('table', { name: zh.mcpSessionsTable })).toBeVisible()
    await expect(page.getByRole('table', { name: zh.mcpGrantsTable })).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('MCP session and refresh grant revoke status messages are accessible', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/mcp-clients')
    await expect(page.getByRole('table', { name: zh.mcpSessionsTable })).toBeVisible()
    await page.getByRole('button', { name: zh.revokeSession }).click()
    await expectToastStatus(page, zh.mcpSessionRevoked)
    await page.getByRole('button', { name: zh.revokeGrant }).click()
    await expectToastStatus(page, zh.mcpGrantRevoked)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('MCP tool-policy errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { mcpToolPolicies: mcpToolPolicyFailure },
    })

    await gotoConsoleRoute(page, '/mcp-clients')
    await page
      .getByRole('checkbox', {
        name: new RegExp(`${escapeRegExp(zh.denyTool)}\\s+list_feedback`),
      })
      .click()
    await page.getByRole('button', { name: zh.saveToolPolicies }).click()
    await expectToastStatus(page, mcpToolPolicyFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/mcp/clients/client-a11y/tool-policies'],
      [conflictResourceConsole, mcpToolPolicyFailure],
    )
  })

  test('GDPR step-up dialog keeps focus and accessible labels intact', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/gdpr')
    await page.getByRole('button', { name: zh.stepUp }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.stepUpDialog)) }),
    ).toBeVisible()
    await page.getByLabel(zh.currentPassword).fill('correct horse battery staple')
    await expectNoAxeViolations(page)
    await page.getByRole('button', { name: zh.stepUpConfirm }).click()
    await expect(
      page.getByRole('dialog', { name: new RegExp(escapeRegExp(zh.stepUpDialog)) }),
    ).toBeHidden()
    await expectToastStatus(page, zh.stepUpSuccess)
    await expectNoDocumentOverflow(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('GDPR validation errors are exposed without a network request', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/gdpr')
    await page.getByRole('button', { name: zh.exportZip }).click()
    await expectToastStatus(page, zh.gdprSubjectRequired)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('dead delivery retry action remains accessible after mutation', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/administration/dead-deliveries')
    await page
      .getByRole('button', { name: new RegExp(`${escapeRegExp(zh.retryDelivery)}.*example`) })
      .click()
    await expectToastStatus(page, zh.outboxRetryQueued)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('dead delivery retry errors remain visible and axe-clean', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page, {
      fail: { outboxRetry: outboxRetryFailure },
    })

    await gotoConsoleRoute(page, '/administration/dead-deliveries')
    await page
      .getByRole('button', { name: new RegExp(`${escapeRegExp(zh.retryDelivery)}.*example`) })
      .click()
    await expectToastStatus(page, outboxRetryFailure)
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(
      diagnostics,
      ['/fb/v1/console/outbox/delivery-a11y/retry'],
      [conflictResourceConsole],
    )
  })
})

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

async function expectRouteShellState(page: Page, routeCase: (typeof routes)[number]) {
  await expect(page).toHaveTitle(new RegExp(`${escapeRegExp(routeCase.title)}.*Attune Console`))
  await expect(page.getByRole('heading', { level: 1, name: routeCase.heading })).toBeVisible()
  await expect(page.locator('main#main-content')).toBeVisible()
  await expect(page.locator('a[href="#main-content"]')).toHaveCount(1)
  await expect(page.locator('[role="dialog"]:visible')).toHaveCount(0)
}

async function cycleSheet(page: Page, opener: Locator, cycles: number) {
  await expect(opener).toBeVisible()

  for (let cycle = 0; cycle < cycles; cycle += 1) {
    await opener.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    if (cycle === 0) {
      await expectNoAxeViolations(page)
    }
    await expectNoDocumentOverflow(page)

    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expectDomFocus(opener)
    await expectNoDocumentOverflow(page)
  }
}

async function expectDomFocus(locator: Locator) {
  await expect
    .poll(
      async () =>
        locator.evaluate((element) => {
          const active = document.activeElement
          if (active === element) return 'target'
          const activeText = active instanceof HTMLElement ? active.innerText.slice(0, 120) : ''
          const targetText = element instanceof HTMLElement ? element.innerText.slice(0, 120) : ''
          return JSON.stringify({
            activeConnected: active instanceof HTMLElement ? active.isConnected : null,
            activeRole: active?.getAttribute('role') ?? null,
            activeTag: active?.tagName ?? null,
            activeText,
            documentHasFocus: document.hasFocus(),
            targetConnected: element.isConnected,
            targetText,
          })
        }),
      {
        message: 'expected the opener to be document.activeElement after close',
      },
    )
    .toBe('target')
}

async function expectToastStatus(page: Page, message: string) {
  const liveRegion = page.getByRole('region', { name: /^Notifications alt\+T$/ })
  await expect(liveRegion).toHaveAttribute('aria-live', 'polite')
  await expect(liveRegion).toHaveAttribute('aria-relevant', 'additions text')
  await expect(liveRegion.getByText(message)).toBeVisible()
}

async function clickShellNav(page: Parameters<typeof gotoConsoleRoute>[0], name: string) {
  const links = page.getByRole('link', { name: new RegExp(`^${escapeRegExp(name)}$`) })
  const visibleLinks = links.filter({ visible: true })

  if ((await visibleLinks.count()) === 0) {
    await page.getByRole('button', { name: zh.openNav }).click()
  }

  await expect(visibleLinks).toHaveCount(1)
  await visibleLinks.click()
}
