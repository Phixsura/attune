#!/usr/bin/env node

import { execFile, execFileSync, spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { mkdtemp, open, readFile, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const consoleDir = path.join(repoRoot, 'console')
const pickFreePortScript = path.join(scriptDir, 'pick-free-port.mjs')

export const DEFAULTS = Object.freeze({
  adminEmail: 'local-admin@example.com',
  adminPassword: 'Attune-Local-Dev-1234',
  dbName: 'attune',
  dbUser: 'attune',
  preferredDbPort: 15432,
  preferredServerPort: undefined,
  tenantName: 'Attune Demo',
  tenantSlug: 'attune-demo',
})

export function parseArgs(argv) {
  const options = {
    adminEmail: process.env.ATTUNE_LOCAL_DEV_ADMIN_EMAIL ?? DEFAULTS.adminEmail,
    adminPassword: process.env.ATTUNE_LOCAL_DEV_ADMIN_PASSWORD ?? DEFAULTS.adminPassword,
    buildConsole: true,
    bootstrapDemo: true,
    dbPort: DEFAULTS.preferredDbPort,
    keepWorkdir: false,
    serverPort: DEFAULTS.preferredServerPort,
    tenantName: process.env.ATTUNE_LOCAL_DEV_TENANT_NAME ?? DEFAULTS.tenantName,
    tenantSlug: process.env.ATTUNE_LOCAL_DEV_TENANT_SLUG ?? DEFAULTS.tenantSlug,
  }

  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index]
    switch (arg) {
      case '-h':
      case '--help':
        options.help = true
        break
      case '--keep-workdir':
        options.keepWorkdir = true
        break
      case '--no-console-build':
        options.buildConsole = false
        break
      case '--no-demo-bootstrap':
        options.bootstrapDemo = false
        break
      case '--admin-email':
        options.adminEmail = requiredValue(argv, ++index, arg)
        break
      case '--admin-password':
        options.adminPassword = requiredValue(argv, ++index, arg)
        break
      case '--db-port':
        options.dbPort = parsePort(requiredValue(argv, ++index, arg), arg)
        break
      case '--server-port':
        options.serverPort = parsePort(requiredValue(argv, ++index, arg), arg)
        break
      case '--tenant':
        options.tenantSlug = requiredValue(argv, ++index, arg)
        break
      case '--tenant-name':
        options.tenantName = requiredValue(argv, ++index, arg)
        break
      case '--intercom-stub':
        options.intercomStubURL = requiredValue(argv, ++index, arg)
        break
      default:
        throw new Error(`unknown option ${arg}`)
    }
  }

  return options
}

export function renderConfig({
  baseURL,
  consoleAdmin,
  consoleSessionKey,
  dsn,
  intercomStubURL,
  keyset,
  serverPort,
}) {
  const intercomBlock = intercomStubURL
    ? `
intercom:
  api_base_url: "${intercomStubURL}"

security:
  allow_loopback_egress: true
`
    : ''
  return `profile: dev
port: ${serverPort}

database:
  url: "${dsn}"

console:
  base_url: "${baseURL}"
  session_key: "${consoleSessionKey}"
  bootstrap_admin:
    email: "${consoleAdmin.email}"
    password: "${consoleAdmin.password}"
${intercomBlock}
secrets:
  tink_keyset: |
${indent(keyset.trimEnd(), 4)}
`
}

export function usage() {
  return `Usage: node scripts/dev-local-stack.mjs [options]

Starts a disposable local Attune stack from the current source tree:
  - temporary PostgreSQL on localhost
  - current Go server binary
  - built Console bundle served from /console
  - optional demo workspace bootstrap

Options:
  --server-port <port>      Preferred Attune port, default random
  --db-port <port>          Preferred PostgreSQL port, default ${DEFAULTS.preferredDbPort}
  --tenant <slug>           Demo tenant slug, default ${DEFAULTS.tenantSlug}
  --tenant-name <name>      Demo tenant name, default "${DEFAULTS.tenantName}"
  --admin-email <email>     Console admin email, default ${DEFAULTS.adminEmail}
  --admin-password <pass>   Console admin password, default ${DEFAULTS.adminPassword}
  --no-console-build        Reuse the current console/dist bundle
  --no-demo-bootstrap       Start without running "attune demo bootstrap"
  --intercom-stub <url>     Point the Intercom adapter at a local stub API
                            (also enables loopback egress for the dial)
  --keep-workdir            Keep temp config, data, and logs after exit
  -h, --help                Print this help
`
}

async function main(argv) {
  const options = parseArgs(argv)
  if (options.help) {
    process.stdout.write(usage())
    return
  }

  const state = {
    browserURL: null,
    keepWorkdir: options.keepWorkdir,
    pgCtlPath: null,
    pgDataDir: null,
    serverLog: null,
    serverLogPath: null,
    serverPid: null,
    stopping: false,
    workDir: await mkdtemp(path.join(os.tmpdir(), 'attune-local-dev-')),
  }

  installSignalHandlers(state)

  try {
    const binaryPath = path.join(state.workDir, 'attune')
    const configPath = path.join(state.workDir, 'config.yaml')
    state.pgDataDir = path.join(state.workDir, 'pgdata')
    state.serverLogPath = path.join(state.workDir, 'attune-server.log')

    const dbPort = await pickFreePort(options.dbPort)
    const serverPort = await pickFreePort(options.serverPort, [dbPort])
    const baseURL = `http://127.0.0.1:${serverPort}`
    const dsn = `postgres://${DEFAULTS.dbUser}@127.0.0.1:${dbPort}/${DEFAULTS.dbName}?sslmode=disable`

    log('build Attune server binary')
    execFileSync('go', ['build', '-o', binaryPath, './cmd/attune'], {
      cwd: repoRoot,
      stdio: 'inherit',
    })

    if (options.buildConsole) {
      log('build Console bundle')
      execFileSync('corepack', ['pnpm', 'exec', 'vite', 'build'], {
        cwd: consoleDir,
        stdio: 'inherit',
      })
    } else {
      log('reuse Console bundle from console/dist')
    }

    const initdbPath = await resolveCommand('initdb')
    state.pgCtlPath = await resolveCommand('pg_ctl')
    const pgIsReadyPath = await resolveCommand('pg_isready')
    const psqlPath = await resolveCommand('psql')

    log(`start temporary PostgreSQL on 127.0.0.1:${dbPort}`)
    execFileSync(initdbPath, ['-D', state.pgDataDir, '-U', DEFAULTS.dbUser, '-A', 'trust', '--no-instructions'], {
      stdio: 'inherit',
    })
    execFileSync(
      state.pgCtlPath,
      [
        '-D',
        state.pgDataDir,
        '-o',
        `-p ${dbPort} -c listen_addresses=127.0.0.1 -c unix_socket_directories=${state.workDir}`,
        '-w',
        'start',
      ],
      { stdio: 'inherit' },
    )
    await waitForPostgres(pgIsReadyPath, dbPort)
    execFileSync(
      psqlPath,
      [
        '-h',
        '127.0.0.1',
        '-p',
        String(dbPort),
        '-U',
        DEFAULTS.dbUser,
        '-d',
        'postgres',
        '-v',
        'ON_ERROR_STOP=1',
        '-c',
        `CREATE DATABASE ${DEFAULTS.dbName};`,
      ],
      { stdio: 'inherit' },
    )

    log('write temporary runtime config')
    const keyset = execFileSync(binaryPath, ['secrets', 'generate-keyset'], {
      cwd: repoRoot,
      encoding: 'utf8',
    })
    const consoleSessionKey = `${randomUUID().replaceAll('-', '')}${randomUUID().replaceAll('-', '')}`
    await writeFile(
      configPath,
      renderConfig({
        baseURL,
        consoleAdmin: { email: options.adminEmail, password: options.adminPassword },
        consoleSessionKey,
        dsn,
        intercomStubURL: options.intercomStubURL,
        keyset,
        serverPort,
      }),
    )

    log(`boot Attune server on ${baseURL}`)
    state.serverLog = await open(state.serverLogPath, 'a')
    const child = spawn(binaryPath, ['--config', configPath, 'server'], {
      cwd: repoRoot,
      detached: true,
      stdio: ['ignore', state.serverLog.fd, state.serverLog.fd],
    })
    state.serverPid = child.pid ?? null
    await waitForHttpOk(`${baseURL}/healthz`, 'Attune healthz')
    await waitForLogLine(state.serverLogPath, 'attune server listening', 'Attune listener')

    if (options.bootstrapDemo) {
      log(`bootstrap demo workspace ${options.tenantSlug}`)
      execFileSync(
        binaryPath,
        ['--config', configPath, 'demo', 'bootstrap', '--tenant', options.tenantSlug, '--name', options.tenantName],
        {
          cwd: repoRoot,
          stdio: 'inherit',
        },
      )
    }

    state.browserURL = `${baseURL}/console/feedback/customer-requests`
    printReady({
      adminEmail: options.adminEmail,
      adminPassword: options.adminPassword,
      baseURL,
      browserURL: state.browserURL,
      configPath,
      keepWorkdir: options.keepWorkdir,
      serverLogPath: state.serverLogPath,
      workDir: state.workDir,
    })

    await waitForServerExit(child)
  } catch (error) {
    process.stderr.write(`local dev stack failed: ${errorMessage(error)}\n`)
    if (state.serverLogPath) {
      await dumpTail(state.serverLogPath, 120)
    }
    process.exitCode = 1
  } finally {
    await cleanup(state)
  }
}

function printReady({
  adminEmail,
  adminPassword,
  baseURL,
  browserURL,
  configPath,
  keepWorkdir,
  serverLogPath,
  workDir,
}) {
  const cleanupLine = keepWorkdir
    ? 'Press Ctrl+C to stop the stack. The temporary workdir will be kept.'
    : 'Press Ctrl+C to stop the stack and remove the temporary workdir.'

  process.stdout.write(`
local dev stack ready

  Console: ${browserURL}
  Health:  ${baseURL}/healthz
  Admin:   ${adminEmail}
  Password: ${adminPassword}

  Workdir: ${workDir}
  Config:  ${configPath}
  Log:     ${serverLogPath}

${cleanupLine}
`)
}

function installSignalHandlers(state) {
  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.once(signal, async () => {
      state.stopping = true
      await cleanup(state)
      process.exit(signal === 'SIGINT' ? 130 : 143)
    })
  }
}

async function waitForServerExit(child) {
  await new Promise((resolve, reject) => {
    child.once('exit', (code, signal) => {
      reject(new Error(`Attune server exited unexpectedly, code=${code ?? 'null'}, signal=${signal ?? 'null'}`))
    })
    child.once('error', reject)
  })
}

async function cleanup(state) {
  if (state.cleaned) return
  state.cleaned = true
  state.stopping = true

  if (state.serverPid) {
    try {
      process.kill(-state.serverPid, 'SIGTERM')
    } catch {
      try {
        process.kill(state.serverPid, 'SIGTERM')
      } catch {
        // ignore
      }
    }
  }

  try {
    await state.serverLog?.close()
  } catch {
    // ignore
  }

  if (state.pgCtlPath && state.pgDataDir) {
    try {
      execFileSync(state.pgCtlPath, ['-D', state.pgDataDir, '-m', 'fast', '-w', 'stop'], {
        stdio: 'ignore',
      })
    } catch {
      // ignore
    }
  }

  if (!state.keepWorkdir && state.workDir) {
    await rm(state.workDir, { recursive: true, force: true })
  } else if (state.workDir) {
    process.stdout.write(`temporary workdir kept: ${state.workDir}\n`)
  }
}

async function resolveCommand(name) {
  try {
    const result = await execFilePromise('sh', ['-lc', `command -v ${shellQuote(name)}`])
    return result.stdout.trim()
  } catch {
    throw new Error(
      `${name} was not found on PATH. Install PostgreSQL client/server tools and retry.`,
    )
  }
}

async function waitForPostgres(pgIsReadyPath, port) {
  for (let attempt = 0; attempt < 60; attempt++) {
    const result = await execFilePromise(
      pgIsReadyPath,
      ['-h', '127.0.0.1', '-p', String(port), '-U', DEFAULTS.dbUser, '-d', 'postgres'],
      { reject: false },
    )
    if (result.code === 0) return
    await delay(500)
  }
  throw new Error(`PostgreSQL did not become ready on 127.0.0.1:${port}`)
}

async function waitForHttpOk(url, label) {
  for (let attempt = 0; attempt < 120; attempt++) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // retry until timeout
    }
    await delay(500)
  }
  throw new Error(`${label} did not become ready at ${url}`)
}

async function waitForLogLine(filePath, needle, label) {
  for (let attempt = 0; attempt < 120; attempt++) {
    try {
      const text = await readFile(filePath, 'utf8')
      if (text.includes(needle)) return
    } catch {
      // retry until the log file exists and contains the line
    }
    await delay(500)
  }
  throw new Error(`${label} did not become ready; missing log line: ${needle}`)
}

async function pickFreePort(preferredPort, excludedPorts = []) {
  const args = [
    preferredPort === undefined ? 'random' : String(preferredPort),
    ...excludedPorts.map(String),
  ]
  const result = await execFilePromise('node', [pickFreePortScript, ...args])
  return parsePort(result.stdout.trim(), 'selected port')
}

async function execFilePromise(command, args, options = {}) {
  const rejectOnError = options.reject !== false
  return await new Promise((resolve, reject) => {
    execFile(command, args, { cwd: options.cwd, encoding: 'utf8' }, (error, stdout, stderr) => {
      const code = typeof error?.code === 'number' ? error.code : error ? 1 : 0
      const result = { code, stderr, stdout }
      if (error && rejectOnError) {
        reject(new Error(`${command} ${args.join(' ')} failed: ${stderr || error.message}`))
      } else {
        resolve(result)
      }
    })
  })
}

async function dumpTail(filePath, lineCount) {
  try {
    const text = await readFile(filePath, 'utf8')
    const lines = text.trimEnd().split('\n').slice(-lineCount)
    if (lines.length > 0) {
      process.stderr.write(`\nlast ${lines.length} server log lines:\n${lines.join('\n')}\n`)
    }
  } catch {
    // ignore
  }
}

function requiredValue(argv, index, flag) {
  const value = argv[index]
  if (!value || value.startsWith('--')) {
    throw new Error(`${flag} requires a value`)
  }
  return value
}

function parsePort(raw, label) {
  const value = Number(raw)
  if (!Number.isInteger(value) || value < 1 || value > 65535) {
    throw new Error(`${label} must be an integer port`)
  }
  return value
}

function indent(text, spaces) {
  const prefix = ' '.repeat(spaces)
  return text
    .split('\n')
    .map((line) => `${prefix}${line}`)
    .join('\n')
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", "'\\''")}'`
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function log(message) {
  process.stdout.write(`[attune-dev] ${message}\n`)
}

function errorMessage(error) {
  return error instanceof Error ? (error.stack ?? error.message) : String(error)
}

function isMain() {
  return process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href
}

if (isMain()) {
  await main(process.argv.slice(2))
}
