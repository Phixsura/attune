import { expect, test } from '@playwright/test'
import {
  collectConsoleDiagnostics,
  expectNoAxeViolations,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  gotoConsoleRoute,
} from './helpers'
import { installConsoleApiMocks } from './route-mocks'

const zh = {
  addButton: '+ 添加入站源',
  channel: '频道',
  create: '新建',
  discover: '发现频道',
  discoverNote: '已发现 3 个可读频道',
  error: '异常',
  healthy: '正常',
  inboundSources: '入站源',
  detail: '源详情',
  name: '名称',
  paused: '已暂停',
  rowSlack: 'Slack Mock Source',
  rowWebhook: 'Website Feedback',
  rowEmail: 'Support Mailbox',
  slack: 'Slack',
  slackChannel: '#product-feedback · 共享',
  slackLive: 'Slack Live Feed',
  test: '测试连接',
  testOk: '连接成功 · 2 ms',
  token: 'Slack Bot Token',
  webhook: 'Webhook',
}

test.describe('Inbound sources browser behavior', () => {
  test('renders the registry with healthy, paused, and error rows', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/inbound-sources')

    await expect(page.getByRole('heading', { level: 1, name: zh.inboundSources })).toBeVisible()
    await expect(page.getByRole('button', { name: zh.rowWebhook })).toBeVisible()
    await expect(page.getByRole('button', { name: zh.rowEmail })).toBeVisible()
    await expect(page.getByRole('button', { name: zh.rowSlack })).toBeVisible()
    await expect(page.getByText(zh.detail, { exact: true })).toBeVisible()

    const webhookRow = page.getByRole('row').filter({ hasText: zh.rowWebhook })
    const emailRow = page.getByRole('row').filter({ hasText: zh.rowEmail })
    const slackRow = page.getByRole('row').filter({ hasText: zh.rowSlack })

    await expect(webhookRow).toContainText(zh.webhook)
    await expect(webhookRow).toContainText(zh.healthy)
    await expect(emailRow).toContainText(zh.paused)
    await expect(slackRow).toContainText(zh.slack)
    await expect(slackRow).toContainText(zh.error)

    await page.getByRole('button', { name: zh.rowSlack }).click()
    await expect(page.getByText('src-slack-a11y')).toBeVisible()
    await expect(page.getByText('slack auth.test: invalid_auth')).toBeVisible()
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })

  test('creates a Slack source after discover and test connection', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/inbound-sources')

    await page.getByRole('button', { name: zh.addButton }).click()
    await page.getByRole('button', { name: zh.slack }).click()
    await page.getByRole('textbox', { name: zh.name }).fill(zh.slackLive)
    await page.getByLabel(zh.token).fill('xoxb-a11y-token')

    await page.getByRole('button', { name: zh.discover }).click()
    await expect(page.getByText(zh.discoverNote, { exact: true })).toBeVisible()

    await page.getByRole('combobox', { name: zh.channel }).click()
    await page.getByRole('option', { name: zh.slackChannel }).click()

    await page.getByRole('button', { name: zh.test }).click()
    await expect(page.getByText(zh.testOk)).toBeVisible()

    await page.getByRole('button', { name: zh.create }).click()

    const createdRow = page.getByRole('row').filter({ hasText: zh.slackLive })
    await expect(createdRow).toContainText(zh.slack)
    await expect(createdRow).toContainText(zh.healthy)
    await expect(createdRow).toBeVisible()

    expect(apiMocks.inboundSourceRequests).toEqual([
      {
        method: 'POST',
        path: '/inbound/sources/slack/discover',
        body: {
          slackConfig: {
            botToken: 'xoxb-a11y-token',
            channelId: '',
          },
        },
      },
      {
        method: 'POST',
        path: '/inbound/sources/test-connection',
        body: {
          channel: 'slack',
          slackConfig: {
            botToken: 'xoxb-a11y-token',
            channelId: 'C-PRODUCT',
          },
        },
      },
      {
        method: 'POST',
        path: '/inbound/sources',
        body: {
          channel: 'slack',
          name: zh.slackLive,
          slackConfig: {
            botToken: 'xoxb-a11y-token',
            channelId: 'C-PRODUCT',
          },
        },
      },
    ])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoConsoleDiagnostics(diagnostics)
  })
})
