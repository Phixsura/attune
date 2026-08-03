import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
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
