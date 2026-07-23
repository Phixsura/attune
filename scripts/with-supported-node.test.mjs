import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { chmod, mkdtemp, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const scriptPath = path.join(scriptDir, 'with-supported-node.sh')

test('with-supported-node uses supported node from PATH', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-path-'))
  await writeFakeNode(path.join(binDir, 'node'), 'v22.22.3')

  const result = runWithNode(['sh', '-c', 'node --version'], {
    PATH: `${binDir}:/usr/bin:/bin`,
    ATTUNE_NODE_SEARCH_PATHS: '',
  })

  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.stdout.trim(), 'v22.22.3')
})

test('with-supported-node requires a command', () => {
  const result = runWithNode([], {
    PATH: '/usr/bin:/bin',
  })

  assert.equal(result.status, 2)
  assert.match(result.stderr, /command is required/)
})

test('with-supported-node falls back to configured search paths', async () => {
  const unsupportedBinDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-unsupported-'))
  const supportedBinDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-supported-'))
  await writeFakeNode(path.join(unsupportedBinDir, 'node'), 'v23.11.0')
  await writeFakeNode(path.join(supportedBinDir, 'node'), 'v22.22.3')

  const result = runWithNode(['sh', '-c', 'node --version && command -v node'], {
    PATH: `${unsupportedBinDir}:/usr/bin:/bin`,
    ATTUNE_NODE_SEARCH_PATHS: supportedBinDir,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trim().split('\n'), ['v22.22.3', path.join(supportedBinDir, 'node')])
})

test('with-supported-node accepts supported explicit overrides', async () => {
  for (const version of ['v20.19.6', 'v22.22.3', 'v24.11.1']) {
    const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-override-supported-'))
    const nodeBin = path.join(binDir, 'node')
    await writeFakeNode(nodeBin, version)

    const result = runWithNode(['sh', '-c', 'node --version'], {
      ATTUNE_NODE_BIN: nodeBin,
      PATH: '/usr/bin:/bin',
    })

    assert.equal(result.status, 0, result.stderr)
    assert.equal(result.stdout.trim(), version)
  }
})

test('with-supported-node rejects an unsupported explicit override', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-override-'))
  const nodeBin = path.join(binDir, 'node')
  await writeFakeNode(nodeBin, 'v23.11.0')

  const result = runWithNode(['sh', '-c', 'true'], {
    PATH: '/usr/bin:/bin',
    ATTUNE_NODE_BIN: nodeBin,
  })

  assert.equal(result.status, 1)
  assert.match(result.stderr, /ATTUNE_NODE_BIN points to unsupported Node v23\.11\.0/)
  assert.match(result.stderr, /CI runs Node 22/)
})

test('with-supported-node skips duplicate configured candidates', async () => {
  const unsupportedBinDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-unsupported-'))
  const supportedBinDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-supported-'))
  await writeFakeNode(path.join(unsupportedBinDir, 'node'), 'v23.11.0')
  await writeFakeNode(path.join(supportedBinDir, 'node'), 'v24.11.1')

  const result = runWithNode(['sh', '-c', 'command -v node && node --version'], {
    ATTUNE_NODE_SEARCH_PATHS: `${supportedBinDir}:${supportedBinDir}`,
    PATH: `${unsupportedBinDir}:/usr/bin:/bin`,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.deepEqual(result.stdout.trim().split('\n'), [path.join(supportedBinDir, 'node'), 'v24.11.1'])
})

test('with-supported-node fails clearly when no supported node exists', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-node-none-'))
  await writeFakeNode(path.join(binDir, 'node'), 'v23.11.0')

  const result = runWithNode(['sh', '-c', 'true'], {
    PATH: `${binDir}:/usr/bin:/bin`,
    ATTUNE_NODE_SEARCH_PATHS: '',
  })

  assert.equal(result.status, 127)
  assert.match(result.stderr, /no supported Node runtime found/)
  assert.match(result.stderr, /Install Node 22/)
})

async function writeFakeNode(filePath, version) {
  await writeFile(
    filePath,
    `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\\n' '${version}'
  exit 0
fi
printf '%s\\n' 'fake node only supports --version' >&2
exit 64
`,
  )
  await chmod(filePath, 0o755)
}

function runWithNode(args, env = {}) {
  return spawnSync('/bin/bash', [scriptPath, ...args], {
    cwd: repoRoot,
    encoding: 'utf8',
    env,
  })
}
