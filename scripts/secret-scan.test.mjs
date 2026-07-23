import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { chmod, mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..')
const scriptPath = path.join(scriptDir, 'secret-scan.sh')

test('secret-scan uses local trufflehog when available', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-bin-'))
  const argsLog = path.join(binDir, 'args.log')
  const changedPaths = path.join(binDir, 'changed-paths')
  const stagedPaths = path.join(binDir, 'staged-paths')
  await writeFile(changedPaths, 'scripts/secret-scan.sh\n')
  await writeFile(stagedPaths, '')
  await writeExecutable(
    path.join(binDir, 'trufflehog'),
    `#!/bin/sh
{
  printf '%s\\n' '__CALL__'
  printf '%s\\n' "$@"
} >> "$SECRET_SCAN_ARGS_LOG"
`,
  )

  const result = runSecretScan(withSystemPath(binDir), {
    SECRET_SCAN_ARGS_LOG: argsLog,
    TRUFFLEHOG_CHANGED_PATHS_FILE: changedPaths,
    TRUFFLEHOG_STAGED_PATHS_FILE: stagedPaths,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /using trufflehog from PATH/)
  assert.match(result.stdout, /secret-scan: scanning current checkout files/)
  assert.match(result.stdout, /secret-scan: no staged index files to scan/)
  const calls = await readCalls(argsLog)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0], [
    'git',
    '--results=verified,unknown',
    `--exclude-paths=${path.join(repoRoot, '.trufflehogignore')}`,
    '--no-update',
    '--fail',
    ...expectedGitRangeArgs(),
    ...expectedGitBranchArgs(),
    `file://${repoRoot}`,
  ])
  assert.equal(calls[1][0], 'filesystem')
  assert.deepEqual(calls[1].slice(1, 2), ['--results=verified,unknown'])
  assert.equal(calls[1][2], `--exclude-paths=${path.join(repoRoot, '.trufflehogignore')}`)
  assert.deepEqual(calls[1].slice(3, 5), ['--no-update', '--fail'])
  assert.match(calls[1][5], /^\/.*\/checkout$/)
})

test('secret-scan falls back to Docker when trufflehog is absent', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-bin-'))
  const argsLog = path.join(binDir, 'args.log')
  const changedPaths = path.join(binDir, 'changed-paths')
  const stagedPaths = path.join(binDir, 'staged-paths')
  await writeFile(changedPaths, 'scripts/secret-scan.sh\n')
  await writeFile(stagedPaths, '')
  await writeExecutable(
    path.join(binDir, 'docker'),
    `#!/bin/sh
{
  printf '%s\\n' '__CALL__'
  printf '%s\\n' "$@"
} >> "$SECRET_SCAN_ARGS_LOG"
`,
  )

  const result = runSecretScan(withSystemPath(binDir), {
    SECRET_SCAN_ARGS_LOG: argsLog,
    TRUFFLEHOG_CHANGED_PATHS_FILE: changedPaths,
    TRUFFLEHOG_STAGED_PATHS_FILE: stagedPaths,
    TRUFFLEHOG_IMAGE: 'example/trufflehog:test',
  })

  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /using Docker image example\/trufflehog:test/)
  assert.match(result.stdout, /secret-scan: scanning current checkout files/)
  assert.match(result.stdout, /secret-scan: no staged index files to scan/)
  const calls = await readCalls(argsLog)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0], [
    'run',
    '--rm',
    '-v',
    `${repoRoot}:/repo:ro`,
    'example/trufflehog:test',
    'git',
    '--results=verified,unknown',
    '--exclude-paths=/repo/.trufflehogignore',
    '--no-update',
    '--fail',
    ...expectedGitRangeArgs(),
    ...expectedGitBranchArgs(),
    'file:///repo',
  ])
  assert.deepEqual(calls[1].slice(0, 4), ['run', '--rm', '-v', `${repoRoot}:/repo:ro`])
  assert.equal(calls[1][4], '-v')
  assert.match(calls[1][5], /^\/.*\/checkout:\/snapshot:ro$/)
  assert.deepEqual(calls[1].slice(6), [
    'example/trufflehog:test',
    'filesystem',
    '--results=verified,unknown',
    '--exclude-paths=/repo/.trufflehogignore',
    '--no-update',
    '--fail',
    '/snapshot',
  ])
})

test('secret-scan scans staged index snapshot', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-bin-'))
  const argsLog = path.join(binDir, 'args.log')
  const changedPaths = path.join(binDir, 'changed-paths')
  const stagedPaths = path.join(binDir, 'staged-paths')
  const stagedRoot = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-staged-'))
  await writeFile(changedPaths, '')
  await writeFile(stagedPaths, 'scripts/secret-scan.sh\n')
  await mkdir(path.join(stagedRoot, 'scripts'), { recursive: true })
  await writeFile(path.join(stagedRoot, 'scripts', 'secret-scan.sh'), '# staged copy\n')
  await writeExecutable(
    path.join(binDir, 'trufflehog'),
    `#!/bin/sh
{
  printf '%s\\n' '__CALL__'
  printf '%s\\n' "$@"
} >> "$SECRET_SCAN_ARGS_LOG"
`,
  )

  const result = runSecretScan(withSystemPath(binDir), {
    SECRET_SCAN_ARGS_LOG: argsLog,
    TRUFFLEHOG_CHANGED_PATHS_FILE: changedPaths,
    TRUFFLEHOG_STAGED_PATHS_FILE: stagedPaths,
    TRUFFLEHOG_STAGED_CONTENT_ROOT: stagedRoot,
  })

  assert.equal(result.status, 0, result.stderr)
  assert.match(result.stdout, /secret-scan: no current checkout files to scan/)
  assert.match(result.stdout, /secret-scan: scanning staged index files/)
  const calls = await readCalls(argsLog)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0], [
    'git',
    '--results=verified,unknown',
    `--exclude-paths=${path.join(repoRoot, '.trufflehogignore')}`,
    '--no-update',
    '--fail',
    ...expectedGitRangeArgs(),
    ...expectedGitBranchArgs(),
    `file://${repoRoot}`,
  ])
  assert.equal(calls[1][0], 'filesystem')
  assert.deepEqual(calls[1].slice(1, 5), [
    '--results=verified,unknown',
    `--exclude-paths=${path.join(repoRoot, '.trufflehogignore')}`,
    '--no-update',
    '--fail',
  ])
  assert.match(calls[1][5], /^\/.*\/staged$/)
})

test('secret-scan fails when Docker fallback fails', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-bin-'))
  await writeExecutable(
    path.join(binDir, 'docker'),
    `#!/bin/sh
printf '%s\\n' 'docker unavailable' >&2
exit 42
`,
  )

  const result = runSecretScan(withSystemPath(binDir))

  assert.equal(result.status, 42)
  assert.match(result.stdout, /using Docker image/)
  assert.match(result.stderr, /docker unavailable/)
})

test('secret-scan fails clearly when no runner is available', async () => {
  const binDir = await mkdtemp(path.join(os.tmpdir(), 'attune-secret-scan-bin-'))

  const result = runSecretScan(binDir)

  assert.equal(result.status, 127)
  assert.match(result.stderr, /TruffleHog is required/)
  assert.match(result.stderr, /brew install trufflehog/)
})

async function writeExecutable(filePath, body) {
  await writeFile(filePath, body)
  await chmod(filePath, 0o755)
}

function runSecretScan(pathValue, env = {}) {
  return spawnSync('/bin/bash', [scriptPath], {
    cwd: repoRoot,
    encoding: 'utf8',
    env: {
      PATH: pathValue,
      ...env,
    },
  })
}

function withSystemPath(binDir) {
  return `${binDir}:/usr/bin:/bin`
}

function expectedGitRangeArgs() {
  const head = gitOutput(['rev-parse', 'HEAD'])
  const base = gitOutput(['merge-base', 'HEAD', 'origin/main'])
  if (base && head && base !== head) {
    return ['--since-commit', base]
  }
  return ['--max-depth=1']
}

function expectedGitBranchArgs() {
  const branch = gitOutput(['rev-parse', '--abbrev-ref', 'HEAD'])
  return branch && branch !== 'HEAD' ? ['--branch', branch] : []
}

function gitOutput(args) {
  const result = spawnSync('git', args, { cwd: repoRoot, encoding: 'utf8' })
  return result.status === 0 ? result.stdout.trim() : ''
}

async function readCalls(filePath) {
  const text = await readFile(filePath, 'utf8')
  return text
    .trimEnd()
    .split('__CALL__\n')
    .filter(Boolean)
    .map((block) => block.trimEnd().split('\n'))
}
