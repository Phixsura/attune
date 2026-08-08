import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.ATTUNE_CONSOLE_E2E_PORT ?? 4173)
const host = '127.0.0.1'
const origin = `http://${host}:${port}`
const browserChannel = process.env.ATTUNE_CONSOLE_E2E_CHANNEL
const executablePath = process.env.ATTUNE_CONSOLE_E2E_EXECUTABLE_PATH
const usePrebuiltConsole = process.env.ATTUNE_CONSOLE_E2E_PREBUILT === '1'
const reuseExistingServer = process.env.ATTUNE_CONSOLE_E2E_REUSE_SERVER === '1'
const includeSupplementalProjects = process.env.ATTUNE_CONSOLE_E2E_SUPPLEMENTAL === '1'
const supplementalProjectFilter = new Set(
  (process.env.ATTUNE_CONSOLE_E2E_SUPPLEMENTAL_PROJECTS ?? 'edge,firefox,webkit')
    .split(',')
    .map((project) => project.trim().toLowerCase())
    .filter(Boolean),
)

const chromiumProjects = [
  {
    name: 'chromium-desktop',
    use: {
      ...devices['Desktop Chrome'],
      channel: executablePath ? undefined : browserChannel,
      launchOptions: executablePath ? { executablePath } : undefined,
      viewport: { width: 1365, height: 768 },
    },
  },
  {
    name: 'chromium-mobile',
    use: {
      ...devices['Pixel 7'],
      channel: executablePath ? undefined : browserChannel,
      launchOptions: executablePath ? { executablePath } : undefined,
      viewport: { width: 390, height: 844 },
    },
  },
]

const allSupplementalProjects = [
  {
    key: 'edge',
    name: 'edge-desktop',
    use: {
      ...devices['Desktop Edge'],
      channel: 'msedge',
      viewport: { width: 1365, height: 768 },
    },
  },
  {
    key: 'firefox',
    name: 'firefox-desktop',
    use: {
      ...devices['Desktop Firefox'],
      viewport: { width: 1365, height: 768 },
    },
  },
  {
    key: 'webkit',
    name: 'webkit-desktop',
    use: {
      ...devices['Desktop Safari'],
      viewport: { width: 1365, height: 768 },
    },
  },
]

const supplementalProjects = includeSupplementalProjects
  ? allSupplementalProjects
      .filter((project) => supplementalProjectFilter.has(project.key))
      .map((project) => ({
        name: project.name,
        use: project.use,
      }))
  : []

export default defineConfig({
  testDir: './e2e/accessibility',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  timeout: 60_000,
  expect: {
    timeout: 10_000,
  },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  webServer: {
    command: usePrebuiltConsole
      ? `pnpm exec vite preview --host ${host} --port ${port} --strictPort`
      : `pnpm exec vite build && pnpm exec vite preview --host ${host} --port ${port} --strictPort`,
    url: `${origin}/console/`,
    reuseExistingServer,
    timeout: 60_000,
  },
  use: {
    baseURL: `${origin}/console`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [...chromiumProjects, ...supplementalProjects],
})
