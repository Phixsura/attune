import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { chmod, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const scriptPath = path.join(scriptDir, 'runtime-smoke-build.sh')

test('runtime-smoke-build keeps the default production image build shape', () => {
  const result = runBuildScript()

  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trim().split('\n'), ['docker', 'build', '-t', 'attune:runtime-smoke', '.'])
})

test('runtime-smoke-build forwards configured base image overrides as build args', () => {
  const result = runBuildScript({
    ATTUNE_RUNTIME_SMOKE_DOCKER: 'podman',
    ATTUNE_RUNTIME_SMOKE_IMAGE: 'example.test/attune:smoke',
    ATTUNE_RUNTIME_SMOKE_NODE_IMAGE: 'mirror.test/node:22-alpine',
    ATTUNE_RUNTIME_SMOKE_GO_IMAGE: 'mirror.test/golang:1.26.5-alpine',
    ATTUNE_RUNTIME_SMOKE_RUNTIME_IMAGE: 'mirror.test/distroless:nonroot',
  })

  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trim().split('\n'), [
    'podman',
    'build',
    '-t',
    'example.test/attune:smoke',
    '--build-arg',
    'ATTUNE_NODE_IMAGE=mirror.test/node:22-alpine',
    '--build-arg',
    'ATTUNE_GO_IMAGE=mirror.test/golang:1.26.5-alpine',
    '--build-arg',
    'ATTUNE_RUNTIME_IMAGE=mirror.test/distroless:nonroot',
    '.',
  ])
})

test('runtime-smoke-build retries a transient registry failure', async (t) => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-runtime-smoke-build-bin-'))
  t.after(() => rm(binDir, { force: true, recursive: true }))
  const statePath = path.join(binDir, 'state')
  const argsPath = path.join(binDir, 'args.log')
  const dockerPath = path.join(binDir, 'docker')
  await writeExecutable(
    dockerPath,
    `#!/bin/sh
printf '%s\\n' "$@" >> "$RUNTIME_SMOKE_BUILD_ARGS_LOG"
if [ ! -f "$RUNTIME_SMOKE_BUILD_STATE" ]; then
  : > "$RUNTIME_SMOKE_BUILD_STATE"
  echo 'failed to fetch anonymous token: Get "https://auth.docker.io/token": EOF' >&2
  exit 75
fi
`,
  )

  const result = runBuildScriptWithoutDryRun({
    ATTUNE_RUNTIME_SMOKE_BUILD_MAX_ATTEMPTS: '2',
    ATTUNE_RUNTIME_SMOKE_BUILD_RETRY_DELAY_SECONDS: '0',
    ATTUNE_RUNTIME_SMOKE_DOCKER: dockerPath,
    RUNTIME_SMOKE_BUILD_ARGS_LOG: argsPath,
    RUNTIME_SMOKE_BUILD_STATE: statePath,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stderr, /transient registry error; retrying build \(1\/2\)/)
  assert.deepEqual((await readFile(argsPath, 'utf8')).trim().split('\n'), [
    'build',
    '-t',
    'attune:runtime-smoke',
    '.',
    'build',
    '-t',
    'attune:runtime-smoke',
    '.',
  ])
})

test('runtime-smoke-build does not retry a non-transient failure', async (t) => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-runtime-smoke-build-bin-'))
  t.after(() => rm(binDir, { force: true, recursive: true }))
  const argsPath = path.join(binDir, 'args.log')
  const dockerPath = path.join(binDir, 'docker')
  await writeExecutable(
    dockerPath,
    `#!/bin/sh
printf '%s\\n' "$@" >> "$RUNTIME_SMOKE_BUILD_ARGS_LOG"
echo 'compile error' >&2
exit 64
`,
  )

  const result = runBuildScriptWithoutDryRun({
    ATTUNE_RUNTIME_SMOKE_BUILD_MAX_ATTEMPTS: '3',
    ATTUNE_RUNTIME_SMOKE_BUILD_RETRY_DELAY_SECONDS: '0',
    ATTUNE_RUNTIME_SMOKE_DOCKER: dockerPath,
    RUNTIME_SMOKE_BUILD_ARGS_LOG: argsPath,
  })

  assert.equal(result.status, 64)
  assert.doesNotMatch(result.stderr, /retrying build/)
  assert.deepEqual((await readFile(argsPath, 'utf8')).trim().split('\n'), [
    'build',
    '-t',
    'attune:runtime-smoke',
    '.',
  ])
})

function runBuildScript(env = {}) {
  return spawnSync('/bin/bash', [scriptPath], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      PATH: '/usr/bin:/bin',
      ATTUNE_RUNTIME_SMOKE_DRY_RUN: '1',
      ...env,
    },
  })
}

function runBuildScriptWithoutDryRun(env = {}) {
  return spawnSync('/bin/bash', [scriptPath], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      PATH: '/usr/bin:/bin',
      ...env,
    },
  })
}

async function writeExecutable(target, contents) {
  await writeFile(target, contents)
  await chmod(target, 0o755)
}
