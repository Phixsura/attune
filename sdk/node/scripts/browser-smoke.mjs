#!/usr/bin/env node

import http from 'node:http'
import { constants as fsConstants } from 'node:fs'
import { access, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { chromium } from 'playwright-core'

const consumerDir = path.resolve(requiredEnv('ATTUNE_BROWSER_E2E_CONSUMER_DIR'))
const baseURL = requiredEnv('ATTUNE_BROWSER_E2E_BASE_URL')
const apiKey = requiredEnv('ATTUNE_BROWSER_E2E_API_KEY')
const marker = process.env.ATTUNE_BROWSER_E2E_MARKER ?? 'sdk-e2e'
const expectedVersion = process.env.ATTUNE_BROWSER_E2E_API_VERSION ?? '2026-06-28'
const allowedPort = parsePort('ATTUNE_BROWSER_E2E_ALLOWED_PORT', 18098)
const blockedPort = parsePort('ATTUNE_BROWSER_E2E_BLOCKED_PORT', 18099)
const smokeHTML = path.join(consumerDir, 'browser-smoke.html')
const smokeJS = path.join(consumerDir, 'browser-smoke.js')

await assertReadable(path.join(consumerDir, 'node_modules/@phixsura/attune/dist/index.js'))

let allowedServer
let blockedServer
let browser

try {
  await writeFile(
    smokeHTML,
    `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>attune browser smoke</title>
  </head>
  <body>
    <main>
      <h1>Browser smoke</h1>
      <p id="page-origin"></p>
      <form id="feedback-form">
        <textarea id="feedback-text" rows="4" placeholder="feedback" required></textarea>
        <button type="submit">Send</button>
      </form>
      <p id="status" aria-live="polite"></p>
      <p id="api-version"></p>
    </main>
    <script>
      window.__ATTUNE_BROWSER_SMOKE__ = ${JSON.stringify({
        baseURL,
        apiKey,
        sourceUser: 'sdk-browser-smoke',
      })}
    </script>
    <script type="module" src="./browser-smoke.js"></script>
  </body>
</html>
`,
  )

  await writeFile(
    smokeJS,
    `import { AttuneError, Client } from './node_modules/@phixsura/attune/dist/index.js'

const config = globalThis.__ATTUNE_BROWSER_SMOKE__
const form = document.querySelector('#feedback-form')
const input = document.querySelector('#feedback-text')
const status = document.querySelector('#status')
const apiVersion = document.querySelector('#api-version')
const pageOrigin = document.querySelector('#page-origin')

if (pageOrigin) pageOrigin.textContent = window.location.origin

const client = new Client({
  baseURL: config.baseURL,
  apiKey: config.apiKey,
  fetch: async (input, init) => {
    const response = await fetch(input, init)
    if (apiVersion) {
      apiVersion.textContent = response.headers.get('x-attune-api-version') ?? ''
    }
    return response
  },
})

form?.addEventListener('submit', async (event) => {
  event.preventDefault()
  if (!input || !status) return
  status.textContent = 'sending...'
  if (apiVersion) apiVersion.textContent = ''
  try {
    const { id } = await client.ingest({
      content: input.value,
      source: 'web',
      sourceUser: config.sourceUser,
      pageUrl: window.location.href,
    })
    status.textContent = \`thanks! (id \${id})\`
  } catch (err) {
    const code = err instanceof AttuneError ? err.code : 'UNKNOWN'
    status.textContent = \`sorry, that failed (\${code})\`
  }
})
`,
  )

  allowedServer = await startStaticServer(consumerDir, allowedPort)
  blockedServer = await startStaticServer(consumerDir, blockedPort)
  browser = await launchBrowser()
  await runScenario(browser, {
    label: 'allowed',
    content: `${marker} browser-allowed`,
    expectedVersion,
    origin: `http://127.0.0.1:${allowedPort}`,
    shouldSucceed: true,
  })
  await runScenario(browser, {
    label: 'blocked',
    content: `${marker} browser-blocked`,
    expectedVersion,
    origin: `http://127.0.0.1:${blockedPort}`,
    shouldSucceed: false,
  })
  console.log(
    `  browser smoke OK (allowed=http://127.0.0.1:${allowedPort} blocked=http://127.0.0.1:${blockedPort})`,
  )
} finally {
  await browser?.close()
  await closeServerIfStarted(blockedServer)
  await closeServerIfStarted(allowedServer)
  await rm(smokeHTML, { force: true })
  await rm(smokeJS, { force: true })
}

async function runScenario(browser, scenario) {
  const page = await browser.newPage()
  const pageErrors = []
  page.on('pageerror', (error) => {
    pageErrors.push(String(error))
  })
  try {
    await page.goto(`${scenario.origin}/browser-smoke.html`, { waitUntil: 'domcontentloaded' })
    await page.locator('#feedback-text').fill(scenario.content)
    await page.getByRole('button', { name: 'Send' }).click()
    await page.waitForFunction(() => {
      const text = document.querySelector('#status')?.textContent ?? ''
      return text.startsWith('thanks!') || text.startsWith('sorry,')
    })

    if (pageErrors.length > 0) {
      throw new Error(`${scenario.label} page error: ${pageErrors.join('; ')}`)
    }

    const status = ((await page.locator('#status').textContent()) ?? '').trim()
    const version = ((await page.locator('#api-version').textContent()) ?? '').trim()
    const origin = ((await page.locator('#page-origin').textContent()) ?? '').trim()

    if (origin !== scenario.origin) {
      throw new Error(
        `${scenario.label} origin mismatch: got ${JSON.stringify(origin)} want ${JSON.stringify(
          scenario.origin,
        )}`,
      )
    }

    if (scenario.shouldSucceed) {
      if (!/^thanks! \(id \d+\)$/.test(status)) {
        throw new Error(`${scenario.label} status mismatch: ${JSON.stringify(status)}`)
      }
      if (version !== scenario.expectedVersion) {
        throw new Error(
          `${scenario.label} api version mismatch: got ${JSON.stringify(version)} want ${JSON.stringify(
            scenario.expectedVersion,
          )}`,
        )
      }
      return
    }

    if (!status.includes('(NETWORK)')) {
      throw new Error(`${scenario.label} expected NETWORK failure, got ${JSON.stringify(status)}`)
    }
    if (version !== '') {
      throw new Error(
        `${scenario.label} should not expose an api version on blocked CORS failure, got ${JSON.stringify(
          version,
        )}`,
      )
    }
  } finally {
    await page.close()
  }
}

async function launchBrowser() {
  const attempts = []
  for (const option of await browserLaunchOptions()) {
    try {
      return await chromium.launch(option)
    } catch (error) {
      attempts.push(
        `${describeLaunchOption(option)}: ${error instanceof Error ? error.message : String(error)}`,
      )
    }
  }
  throw new Error(
    [
      'failed to launch a Chromium-family browser for the browser smoke test.',
      'Set ATTUNE_BROWSER_E2E_EXECUTABLE=/absolute/path/to/browser if auto-detection misses your install.',
      ...attempts.map((attempt) => `- ${attempt}`),
    ].join('\n'),
  )
}

async function browserLaunchOptions() {
  const options = []
  const explicitExecutable = process.env.ATTUNE_BROWSER_E2E_EXECUTABLE
  if (explicitExecutable) options.push({ executablePath: explicitExecutable, headless: true })
  const explicitChannel = process.env.ATTUNE_BROWSER_E2E_CHANNEL
  if (explicitChannel) options.push({ channel: explicitChannel, headless: true })
  for (const executablePath of await cachedChromiumExecutables()) {
    options.push({ executablePath, headless: true })
  }
  options.push({ channel: 'chrome', headless: true })
  options.push({ channel: 'msedge', headless: true })
  return dedupeLaunchOptions(options)
}

async function cachedChromiumExecutables() {
  const home = process.env.HOME
  if (!home) return []
  const roots = [
    path.join(home, 'Library/Caches/ms-playwright'),
    path.join(home, '.cache/ms-playwright'),
  ]
  const out = []
  for (const root of roots) {
    let entries
    try {
      entries = await readdir(root, { withFileTypes: true })
    } catch {
      continue
    }
    const dirs = entries
      .filter((entry) => entry.isDirectory() && entry.name.startsWith('chromium-'))
      .map((entry) => entry.name)
      .sort((a, b) => chromiumRevision(b) - chromiumRevision(a) || b.localeCompare(a))
    for (const dir of dirs) {
      for (const candidate of chromiumExecutableCandidates(path.join(root, dir))) {
        if (await canRead(candidate)) out.push(candidate)
      }
    }
  }
  return [...new Set(out)]
}

function chromiumRevision(dirName) {
  const match = /^chromium-(\d+)$/.exec(dirName)
  return match ? Number(match[1]) : -1
}

function chromiumExecutableCandidates(root) {
  return [
    path.join(
      root,
      'chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing',
    ),
    path.join(root, 'chrome-mac/Chromium.app/Contents/MacOS/Chromium'),
    path.join(root, 'chrome-linux/chrome'),
  ]
}

function dedupeLaunchOptions(options) {
  const seen = new Set()
  const out = []
  for (const option of options) {
    const key =
      'channel' in option ? `channel:${option.channel}` : `executable:${option.executablePath}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(option)
  }
  return out
}

function describeLaunchOption(option) {
  return 'channel' in option ? `channel=${option.channel}` : `executable=${option.executablePath}`
}

async function startStaticServer(root, port) {
  const normalizedRoot = path.resolve(root)
  const server = http.createServer(async (req, res) => {
    try {
      const requestURL = new URL(req.url ?? '/', `http://127.0.0.1:${port}`)
      let pathname = decodeURIComponent(requestURL.pathname)
      if (pathname === '/') pathname = '/browser-smoke.html'
      const filePath = path.resolve(normalizedRoot, `.${pathname}`)
      if (filePath !== normalizedRoot && !filePath.startsWith(`${normalizedRoot}${path.sep}`)) {
        res.writeHead(403, { 'content-type': 'text/plain; charset=utf-8' })
        res.end('forbidden')
        return
      }
      if (!(await canRead(filePath))) {
        res.writeHead(404, { 'content-type': 'text/plain; charset=utf-8' })
        res.end('not found')
        return
      }
      const body = await readFile(filePath)
      res.writeHead(200, {
        'cache-control': 'no-store',
        'content-type': contentType(filePath),
      })
      res.end(body)
    } catch (error) {
      res.writeHead(500, { 'content-type': 'text/plain; charset=utf-8' })
      res.end(error instanceof Error ? error.message : String(error))
    }
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', () => {
      server.off('error', reject)
      resolve()
    })
  })
  return server
}

async function closeServerIfStarted(server) {
  if (!server) return
  await closeServer(server)
}

async function closeServer(server) {
  await new Promise((resolve, reject) => {
    server.close((error) => {
      if (error) reject(error)
      else resolve()
    })
  })
}

function contentType(filePath) {
  if (filePath.endsWith('.html')) return 'text/html; charset=utf-8'
  if (filePath.endsWith('.js')) return 'text/javascript; charset=utf-8'
  if (filePath.endsWith('.json')) return 'application/json; charset=utf-8'
  return 'application/octet-stream'
}

function requiredEnv(name) {
  const value = process.env[name]
  if (!value) throw new Error(`missing required environment variable ${name}`)
  return value
}

function parsePort(name, fallback) {
  const raw = process.env[name]
  if (!raw) return fallback
  const port = Number(raw)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`${name} must be an integer port`)
  }
  return port
}

async function assertReadable(filePath) {
  if (!(await canRead(filePath))) throw new Error(`missing required file: ${filePath}`)
}

async function canRead(filePath) {
  try {
    await access(filePath, fsConstants.R_OK)
    return true
  } catch {
    return false
  }
}
