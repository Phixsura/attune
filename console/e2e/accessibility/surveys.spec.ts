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
  blockedDelivery: '\u963b\u65ad',
  campaignHealth: '\u6d3b\u52a8\u5065\u5eb7\u8bca\u65ad',
  eligible: '\u53ef\u53d1\u9001',
  feedbackID: '\u53cd\u9988\u0020\u0049\u0044',
  lowScoreRecovery: '\u4f4e\u5206\u6062\u590d\u961f\u5217',
  needsAttention: '\u9700\u5173\u6ce8',
  recipientPreview: '\u6536\u4ef6\u4eba\u9884\u68c0',
  recipientPreviewSubmit: '\u9884\u68c0\u6536\u4ef6\u4eba',
  sendTestEmail: '\u53d1\u9001\u6d4b\u8bd5\u90ae\u4ef6',
  sentByPostmark:
    '\u6d4b\u8bd5\u90ae\u4ef6\u5df2\u901a\u8fc7\u0020\u0070\u006f\u0073\u0074\u006d\u0061\u0072\u006b\u0020\u53d1\u9001',
  suppressionRate: '\u6291\u5236\u7387',
  surveys: '\u6ee1\u610f\u5ea6\u8c03\u67e5',
  testEmail: '\u6d4b\u8bd5\u90ae\u4ef6',
  toEmail: '\u6536\u4ef6\u90ae\u7bb1',
} as const

test.describe('Survey browser behavior', () => {
  test('campaign health and delivery drills stay verifiable from the console', async ({ page }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/surveys')

    await expect(page.getByRole('heading', { level: 1, name: zh.surveys })).toBeVisible()
    await expect(page.getByRole('button', { name: 'A11y CSAT' })).toBeVisible()

    const healthCard = page.getByTestId('survey-campaign-health')
    await expect(healthCard.getByText(zh.campaignHealth)).toBeVisible()
    await expect(healthCard.getByText(zh.needsAttention)).toBeVisible()
    await expect(healthCard.getByText('80/100')).toBeVisible()
    await expect(healthCard.getByText(zh.suppressionRate, { exact: true })).toHaveCount(2)
    await expect(healthCard.getByText('25%')).toBeVisible()
    await expect(healthCard.getByText(zh.lowScoreRecovery, { exact: true })).toBeVisible()
    await expect(healthCard.getByText(/suppression_rate=0\.25/)).toBeVisible()

    await page.getByLabel(zh.feedbackID).fill('feedback-101')
    await page.getByTestId('survey-preview-run').click()

    const preview = page.getByTestId('survey-preview-result')
    await expect(preview.getByText(zh.eligible, { exact: true })).toHaveCount(2)
    await expect(preview.getByText('A11y Customer')).toBeVisible()
    await expect(preview.getByText('workflow_transition \u00b7 feedback-101')).toBeVisible()
    await expect(preview.getByText(zh.blockedDelivery)).toHaveCount(0)

    await page.getByLabel(zh.toEmail).fill('qa-recipient@example.com')
    await page.getByTestId('survey-test-email-send').click()
    await expect(page.getByText(zh.sentByPostmark)).toBeVisible()

    expect(apiMocks.surveyRequests).toEqual([
      expect.objectContaining({
        method: 'POST',
        path: '/surveys/campaigns/survey-a11y-csat/recipients:preview',
        body: expect.objectContaining({
          campaignId: 'survey-a11y-csat',
          context: { workflow_category: 'closed' },
          limit: 10,
          sourceId: 'feedback-101',
          sourceType: 'feedback',
        }),
      }),
      expect.objectContaining({
        method: 'POST',
        path: '/surveys/campaigns/survey-a11y-csat:sendTestEmail',
        body: {
          campaignId: 'survey-a11y-csat',
          toEmail: 'qa-recipient@example.com',
        },
      }),
    ])
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })
})
