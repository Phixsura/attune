import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { chmod, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const scriptPath = path.join(scriptDir, 'test-integration.sh')

test('test-integration starts local Docker Postgres with stable shared memory', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-test-integration-bin-'))
  const dockerLog = path.join(binDir, 'docker.log')
  const goLog = path.join(binDir, 'go.log')
  await writeFakeDocker(path.join(binDir, 'docker'))
  await writeFakeGo(path.join(binDir, 'go'))

  const result = runTestIntegration(binDir, {
    ATTUNE_TEST_POSTGRES_IMAGE: 'example/postgres:pg17',
    ATTUNE_TEST_POSTGRES_SHM_SIZE: '2g',
    ATTUNE_TEST_INTEGRATION_TIMEOUT: '9m',
    TEST_INTEGRATION_DOCKER_LOG: dockerLog,
    TEST_INTEGRATION_GO_LOG: goLog,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /test-integration: starting example\/postgres:pg17/)
  assert.match(result.stdout, /shm=2g timeout=9m/)

  const dockerCalls = await readCalls(dockerLog)
  assert.deepEqual(dockerCalls[0], ['info'])
  assert.deepEqual(dockerCalls[1].slice(0, 6), ['run', '-d', '--rm', '--name', dockerCalls[1][4], '--shm-size'])
  assert.equal(dockerCalls[1][6], '2g')
  assert.equal(dockerCalls[1].at(-1), 'example/postgres:pg17')

  const goCalls = await readCalls(goLog)
  assert.equal(goCalls.length, 1)
  assert.deepEqual(goCalls[0], ['test', '-tags=integration', '-count=1', '-p', '1', '-timeout=9m', './test/integration/postgres/...'])
})

function runTestIntegration(binDir, env = {}) {
  return spawnSync('/bin/bash', [scriptPath], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      PATH: `${binDir}:/usr/bin:/bin`,
      ...env,
    },
  })
}

async function writeFakeDocker(filePath) {
  await writeExecutable(
    filePath,
    `#!/bin/sh
{
  printf '%s\\n' '__CALL__'
  printf '%s\\n' "$@"
} >> "$TEST_INTEGRATION_DOCKER_LOG"

case "$1" in
  info)
    exit 0
    ;;
  run)
    exit 0
    ;;
  exec)
    exit 0
    ;;
  port)
    printf '%s\\n' '127.0.0.1:15433'
    exit 0
    ;;
  rm)
    exit 0
    ;;
esac

printf 'unexpected docker command: %s\\n' "$1" >&2
exit 64
`,
  )
}

async function writeFakeGo(filePath) {
  await writeExecutable(
    filePath,
    `#!/bin/sh
{
  printf '%s\\n' '__CALL__'
  printf '%s\\n' "$@"
} >> "$TEST_INTEGRATION_GO_LOG"
exit 0
`,
  )
}

async function writeExecutable(filePath, contents) {
  await writeFile(filePath, contents)
  await chmod(filePath, 0o755)
}

async function readCalls(filePath) {
  const contents = await readFile(filePath, 'utf8')
  return contents
    .split('__CALL__\n')
    .filter(Boolean)
    .map((call) => call.trimEnd().split('\n'))
}
