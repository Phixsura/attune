import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { expect, type Page } from '@playwright/test'

const require = createRequire(import.meta.url)
const axeSource = readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8')

type AxeViolation = {
  id: string
  impact: string | null
  help: string
  nodes: Array<{
    target: string[]
    html: string
    failureSummary?: string
  }>
}

type AxeResults = {
  violations: AxeViolation[]
}

export type ConsoleDiagnostics = {
  messages: string[]
  failedApiResponses: string[]
}

export function collectConsoleDiagnostics(page: Page): ConsoleDiagnostics {
  const diagnostics: ConsoleDiagnostics = {
    messages: [],
    failedApiResponses: [],
  }

  page.on('console', (message) => {
    if (message.type() === 'error') {
      diagnostics.messages.push(message.text())
    }
  })

  page.on('pageerror', (error) => {
    diagnostics.messages.push(error.message)
  })

  page.on('response', (response) => {
    const url = response.url()
    if (!url.includes('/fb/v1/console/')) return
    if (response.status() < 400) return
    diagnostics.failedApiResponses.push(
      `${response.status()} ${response.request().method()} ${url}`,
    )
  })

  return diagnostics
}

export async function gotoConsoleRoute(page: Page, path: string) {
  await page.goto(`/console${path}`)
  await expect(page.locator('main')).toBeVisible()
}

export async function expectNoConsoleDiagnostics(
  diagnostics: ConsoleDiagnostics,
  allowedApiFailures: string[] = [],
  allowedConsoleMessages: string[] = [],
) {
  const disallowedApiFailures = diagnostics.failedApiResponses.filter((failure) =>
    allowedApiFailures.every((allowed) => !failure.includes(allowed)),
  )
  const disallowedMessages = diagnostics.messages.filter((message) =>
    allowedConsoleMessages.every((allowed) => !message.includes(allowed)),
  )

  expect(disallowedMessages).toEqual([])
  expect(disallowedApiFailures).toEqual([])
}

export async function expectNoDocumentOverflow(page: Page) {
  const result = await page.evaluate(() => {
    const root = document.documentElement
    const body = document.body
    const overflow = Math.max(root.scrollWidth, body.scrollWidth) - window.innerWidth
    const offenders = Array.from(document.body.querySelectorAll('*'))
      .map((element) => {
        const rect = element.getBoundingClientRect()
        return {
          tag: element.tagName.toLowerCase(),
          className: element.getAttribute('class') ?? '',
          text: element.textContent?.trim().slice(0, 80) ?? '',
          left: Math.round(rect.left),
          right: Math.round(rect.right),
          width: Math.round(rect.width),
        }
      })
      .filter((item) => item.right > window.innerWidth + 1 || item.left < -1)
      .slice(0, 8)
    return { offenders, overflow }
  })

  expect(result.overflow, JSON.stringify(result.offenders, null, 2)).toBeLessThanOrEqual(1)
}

export async function expectOpaqueBackground(page: Page, selector: string, label: string) {
  const color = await page
    .locator(selector)
    .evaluate((element) => window.getComputedStyle(element).backgroundColor)
  const alpha = cssColorAlpha(color)

  expect(alpha, `${label} background must be opaque; got ${color}`).toBe(1)
}

export async function expectNoAxeViolations(page: Page) {
  await page.addScriptTag({ content: axeSource })
  const results = await page.evaluate<AxeResults>(async () => {
    const axe = (
      window as unknown as {
        axe: {
          run: (context: Document, options: object) => Promise<AxeResults>
        }
      }
    ).axe
    return axe.run(document, {
      resultTypes: ['violations'],
      rules: {
        region: { enabled: true },
      },
    })
  })

  const blockingViolations = results.violations.filter((violation) => violation.impact !== 'minor')

  expect(formatAxeViolations(blockingViolations)).toEqual([])
}

function cssColorAlpha(value: string) {
  if (value === 'transparent') return 0
  const commaParts = value
    .match(/rgba?\(([^)]+)\)/)?.[1]
    .split(',')
    .map((part) => part.trim())
  if (commaParts && commaParts.length === 4) return normalizedAlpha(commaParts[3])

  const slashAlpha = value.match(/\/\s*([0-9.]+%?)/)?.[1]
  if (slashAlpha) return normalizedAlpha(slashAlpha)

  return 1
}

function normalizedAlpha(value: string) {
  if (value.endsWith('%')) return Number(value.slice(0, -1)) / 100
  return Number(value)
}

function formatAxeViolations(violations: AxeViolation[]) {
  return violations.map((violation) => {
    const firstNode = violation.nodes[0]
    return {
      id: violation.id,
      impact: violation.impact,
      help: violation.help,
      target: firstNode?.target.join(' '),
      summary: firstNode?.failureSummary,
    }
  })
}
