import { expect, type Locator, type Page, test } from '@playwright/test'
import {
  collectConsoleDiagnostics,
  expectNoAxeViolations,
  expectNoConsoleDiagnostics,
  expectNoDocumentOverflow,
  gotoConsoleRoute,
} from './helpers'
import { installConsoleApiMocks } from './route-mocks'

const zh = {
  account: '账号或组织',
  create: '新建',
  createConnection: '新建连接',
  createInstallation: '新建安装',
  connectionTitle: '新建外部连接',
  credential: '凭据',
  displayName: '显示名称',
  externalID: '外部安装 ID',
  heading: '外部同步',
  installationName: 'Acme GitHub App',
  installationTitle: '新建 Provider 安装',
  name: '名称',
  permissions: '权限 JSON',
  providerInstallation: 'Provider 安装',
  qualifyInstallation: '检查安装',
  resourceKey: '资源键',
  resourceName: '资源名称',
  resourceURL: '资源 URL',
  save: '保存',
}

test.describe('External sync provider installation browser behavior', () => {
  test('creates, qualifies, and saves provider resource selection through visible controls', async ({
    page,
  }) => {
    const diagnostics = collectConsoleDiagnostics(page)
    const apiMocks = await installConsoleApiMocks(page)

    await gotoConsoleRoute(page, '/integrations/external-sync')

    await expect(page.getByRole('heading', { level: 1, name: zh.heading })).toBeVisible()
    await mouseClick(page, page.getByRole('button', { name: zh.createInstallation }))

    const dialog = page.getByRole('dialog', { name: zh.installationTitle })
    await expect(dialog).toBeVisible()
    await dialog.getByLabel(zh.displayName).fill(zh.installationName)
    await dialog.getByLabel(zh.externalID).fill('12345678')
    await dialog.getByLabel(zh.account).fill('acme')
    await dialog.getByLabel(zh.permissions).fill('{"metadata":"read","issues":"write"}')
    await dialog.getByLabel(zh.resourceKey).fill('acme/attune')
    await dialog.getByLabel(zh.resourceName).fill('attune')
    await dialog.getByLabel(zh.resourceURL).fill('https://github.com/acme/attune')
    await mouseClick(page, dialog.getByRole('button', { name: zh.create }))

    await expect(dialog).toBeHidden()
    const installationCard = page.locator('button').filter({ hasText: zh.installationName })
    await expect(installationCard).toBeVisible()
    await mouseClick(page, installationCard)
    await expect(page.getByText('acme/attune')).toBeVisible()

    const resourceCheckbox = page.getByRole('checkbox', { name: /attune/ })
    await expect(resourceCheckbox).toBeChecked()
    await mouseClick(page, page.getByRole('button', { name: zh.qualifyInstallation }))
    await expect(installationCard).toContainText('full_app')
    await expect(installationCard).toContainText('ok')

    await resourceCheckbox.click()
    await expect(resourceCheckbox).not.toBeChecked()
    await mouseClick(page, page.getByRole('button', { name: zh.save }).first())

    await expect
      .poll(() => lastProviderResourceSelection(apiMocks.providerInstallationRequests))
      .toEqual({ resourceIds: [] })
    await expect(page.getByText('none').first()).toBeVisible()

    await resourceCheckbox.click()
    await expect(resourceCheckbox).toBeChecked()
    await mouseClick(page, page.getByRole('button', { name: zh.save }).first())

    await expect
      .poll(() => lastProviderResourceSelection(apiMocks.providerInstallationRequests))
      .toEqual({ resourceIds: ['external-sync-provider-installation-1-resource-1'] })
    await mouseClick(page, page.getByRole('button', { name: zh.createConnection }))

    const connectionDialog = page.getByRole('dialog', { name: zh.connectionTitle })
    await expect(connectionDialog).toBeVisible()
    await expect(connectionDialog.getByLabel(zh.name)).toHaveValue(zh.installationName)
    await expect(
      connectionDialog.getByRole('combobox', { name: zh.providerInstallation }),
    ).toContainText(zh.installationName)
    await connectionDialog.getByLabel(zh.credential).fill('gh-token')
    await mouseClick(page, connectionDialog.getByRole('button', { name: zh.create }))

    await expect
      .poll(() => lastExternalConnectionRequest(apiMocks.externalConnectionRequests))
      .toMatchObject({
        authType: 'token',
        credential: 'gh-token',
        name: zh.installationName,
        provider: 'github',
        providerConfigJson: '{"owner":"acme","repo":"attune"}',
        providerInstallationId: 'external-sync-provider-installation-1',
        scopes: ['issues'],
      })
    await expect(connectionDialog).toBeHidden()
    expect(apiMocks.providerInstallationRequests.slice(0, 2)).toMatchObject([
      {
        method: 'POST',
        path: '/external-sync/provider-installations',
        body: {
          accountLogin: 'acme',
          displayName: zh.installationName,
          externalInstallationId: '12345678',
          installationKind: 'github_app',
          permissionsJson: '{"metadata":"read","issues":"write"}',
          provider: 'github',
          resourceSelection: 'selected',
          resources: [
            {
              displayName: 'attune',
              htmlUrl: 'https://github.com/acme/attune',
              resourceKey: 'acme/attune',
              resourceType: 'repository',
              selected: true,
              status: 'active',
            },
          ],
        },
      },
      {
        method: 'POST',
        path: '/external-sync/provider-installations/external-sync-provider-installation-1:qualify',
        body: null,
      },
    ])
    expect(apiMocks.unhandledRequests).toEqual([])
    await expectNoDocumentOverflow(page)
    await expectNoAxeViolations(page)
    await expectNoConsoleDiagnostics(diagnostics)
  })
})

async function mouseClick(_page: Page, locator: Locator) {
  await expect(locator).toBeVisible()
  await locator.click()
}

function lastProviderResourceSelection(
  requests: Array<{ body: unknown; method: string; path: string }>,
) {
  return requests.filter((request) => request.path.endsWith('/resources:select')).at(-1)?.body
}

function lastExternalConnectionRequest(
  requests: Array<{ body: unknown; method: string; path: string }>,
) {
  return requests.filter((request) => request.path === '/external-sync/connections').at(-1)?.body
}
