#!/usr/bin/env node

import { execFileSync, spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { constants as fsConstants } from 'node:fs'
import { access, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium, expect } from '@playwright/test'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..', '..')
const consoleDir = path.join(repoRoot, 'console')
const pickFreePortScript = path.join(repoRoot, 'scripts', 'pick-free-port.mjs')
const workDir = await mkdtemp(path.join(os.tmpdir(), 'attune-public-board-smoke-'))
const pgDataDir = path.join(workDir, 'pgdata')
const configPath = path.join(workDir, 'config.yaml')
const serverLogPath = path.join(workDir, 'server.log')
const binaryPath = path.join(workDir, 'attune')
const dbUser = 'attune'
const dbName = 'attune'
const keepAlive = process.env.ATTUNE_PUBLIC_BOARD_E2E_KEEP_SERVER === '1'
const tenantA = { slug: 'acme', name: 'Acme' }
const tenantB = { slug: 'acme-b', name: 'Acme B' }
const consoleAdmin = {
  email: 'smoke-admin@example.com',
  password: 'Attune-Smoke-Console-1234',
}
const consoleSessionKey = `${randomUUID().replace(/-/g, '')}${randomUUID().replace(/-/g, '')}`

let pgCtlPath = ''
let initdbPath = ''
let psqlPath = ''
let pgIsReadyPath = ''
let serverPid = null
let browser = null
let serverLog = null

const tenantASeed = buildSeedData()
const tenantBSeed = buildSeedData()

try {
  log('build attune server binary')
  execFileSync('go', ['build', '-o', binaryPath, './cmd/attune'], {
    cwd: repoRoot,
    stdio: 'inherit',
  })
  log('build console dist')
  // The smoke script only needs the generated SPA bundle; the console
  // workflow already runs `pnpm tsc -b --noEmit` separately.
  execFileSync('pnpm', ['exec', 'vite', 'build'], {
    cwd: consoleDir,
    stdio: 'inherit',
  })

  initdbPath = await resolveCommand('initdb')
  pgCtlPath = await resolveCommand('pg_ctl')
  psqlPath = await resolveCommand('psql')
  pgIsReadyPath = await resolveCommand('pg_isready')

  const dbPort = await pickFreePort()
  const serverPort = await pickFreePort(dbPort)
  const baseURL = `http://127.0.0.1:${serverPort}`
  const dsn = `postgres://${dbUser}@127.0.0.1:${dbPort}/${dbName}?sslmode=disable`

  log('start temporary PostgreSQL cluster')
  execFileSync(initdbPath, ['-D', pgDataDir, '-U', dbUser, '-A', 'trust', '--no-instructions'], {
    stdio: 'inherit',
  })
  execFileSync(
    pgCtlPath,
    [
      '-D',
      pgDataDir,
      '-o',
      `-p ${dbPort} -c listen_addresses=127.0.0.1 -c unix_socket_directories=${workDir}`,
      '-w',
      'start',
    ],
    { stdio: 'inherit' },
  )
  await waitForPg(pgIsReadyPath, dbPort, 'postgres')
  execFileSync(
    psqlPath,
    [
      '-h',
      '127.0.0.1',
      '-p',
      String(dbPort),
      '-U',
      dbUser,
      '-d',
      'postgres',
      '-v',
      'ON_ERROR_STOP=1',
      '-c',
      `CREATE DATABASE ${dbName};`,
    ],
    { stdio: 'inherit' },
  )

  log('write temp config')
  const keyset = execFileSync(binaryPath, ['secrets', 'generate-keyset'], {
    cwd: repoRoot,
    encoding: 'utf8',
  }).trimEnd()
  await writeFile(
    configPath,
    `port: ${serverPort}
database:
  url: "${dsn}"
console:
  base_url: "${baseURL}"
  session_key: "${consoleSessionKey}"
  bootstrap_admin:
    email: "${consoleAdmin.email}"
    password: "${consoleAdmin.password}"
secrets:
  tink_keyset: |
${indent(keyset, 4)}
`,
  )

  log('boot attune server')
  serverLog = await openWriteHandle(serverLogPath)
  const child = spawn(binaryPath, ['--config', configPath, 'server'], {
    cwd: repoRoot,
    detached: true,
    stdio: ['ignore', serverLog.fd, serverLog.fd],
  })
  serverPid = child.pid ?? null
  await waitForHttpOk(`${baseURL}/healthz`, 'healthz')

  log('bootstrap smoke tenant')
  const tenantAId = await bootstrapDemoTenant(binaryPath, configPath, repoRoot, dsn, tenantA)
  const tenantBId = await bootstrapDemoTenant(binaryPath, configPath, repoRoot, dsn, tenantB)

  log('seed public board data')
  await execPsql(dsn, buildSeedSql(tenantAId, tenantASeed))
  await execPsql(dsn, buildSeedSql(tenantBId, tenantBSeed))

  log('launch browser')
  browser = await launchBrowser()
  await runDesktopSmoke(browser, baseURL, tenantASeed, tenantA)
  await runTenantIsolationSmoke(browser, baseURL, tenantASeed, tenantA, tenantB)
  await runMobileSmoke(browser, baseURL, tenantASeed, tenantA)
  await verifyRoadmapApi(baseURL, tenantASeed, tenantA)
  await runConsoleSmoke(browser, baseURL, tenantA, tenantASeed)

  console.log(`portal + console browser smoke: ok (base=${baseURL})`)
  if (keepAlive) {
    console.log(
      'portal + console browser smoke: keep-alive mode enabled; press Ctrl+C to stop the server',
    )
    await new Promise(() => {})
  }
} catch (error) {
  console.error(
    `portal + console browser smoke failed: ${error instanceof Error ? (error.stack ?? error.message) : String(error)}`,
  )
  if (serverLogPath) {
    await dumpTail(serverLogPath, 120)
  }
  process.exitCode = 1
} finally {
  if (!keepAlive) {
    await browser?.close().catch(() => {})
    try {
      await serverLog?.close()
    } catch {
      // ignore
    }
    if (serverPid) {
      try {
        process.kill(-serverPid, 'SIGTERM')
      } catch {
        try {
          process.kill(serverPid, 'SIGTERM')
        } catch {
          // ignore
        }
      }
    }
    if (pgCtlPath) {
      try {
        execFileSync(pgCtlPath, ['-D', pgDataDir, '-m', 'fast', '-w', 'stop'], {
          stdio: 'ignore',
        })
      } catch {
        // ignore
      }
    }
    await rm(workDir, { recursive: true, force: true })
  }
}

function buildSeedData() {
  const specialTitles = new Map([
    [1, 'Billing export timeouts'],
    [2, 'Audit log actor filter'],
    [3, 'Reply draft tone'],
    [4, 'SSO approval loop'],
    [7, 'Mobile detail layout shift'],
    [10, 'Search misses exact phrase'],
  ])
  const plannedIndices = new Set([1, 2, 4, 10, 16])
  const roadmapPublishedIndices = new Set([4, 10, 16, 22])
  const commentIndices = new Set([2, 3, 17, 19])
  const requests = []

  for (let index = 1; index <= 22; index++) {
    const comments = []
    if (commentIndices.has(index)) {
      comments.push({
        id: randomUUID(),
        subjectId: randomUUID(),
        body: `Seeded public comment for portal request ${index}.`,
        moderationState: 'approved',
      })
    }
    if (index === 2) {
      comments.push({
        id: randomUUID(),
        subjectId: randomUUID(),
        body: 'Pending public comment for portal request 2.',
        moderationState: 'pending',
      })
    }

    requests.push({
      index,
      id: randomUUID(),
      profileId: randomUUID(),
      slug: `portal-request-${String(index).padStart(2, '0')}`,
      title: specialTitles.get(index) ?? `Portal request ${index}`,
      summary: `Portal request ${index} about billing exports, public visibility filters, and roadmap triage.`,
      publicState: plannedIndices.has(index) ? 'planned' : 'open',
      roadmapColumn: plannedIndices.has(index) ? 'planned' : 'under consideration',
      includedInPortal: true,
      includedInRoadmap: roadmapPublishedIndices.has(index),
      comments,
    })
  }

  const hiddenRequests = [
    {
      index: 23,
      id: randomUUID(),
      profileId: randomUUID(),
      slug: 'internal-triage-note',
      title: 'Internal triage note',
      summary: 'This request stays private and should never appear on the public board.',
      publicState: 'open',
      roadmapColumn: 'under consideration',
      includedInPortal: false,
      includedInRoadmap: false,
      moderationState: 'pending',
    },
    {
      index: 24,
      id: randomUUID(),
      profileId: randomUUID(),
      slug: 'pending-public-request',
      title: 'Pending public request',
      summary: 'This request is still waiting on moderation and must stay hidden.',
      publicState: 'planned',
      roadmapColumn: 'planned',
      includedInPortal: true,
      includedInRoadmap: false,
      moderationState: 'pending',
    },
  ]

  const pendingRequest = hiddenRequests[1]
  const pendingCommentRequest = requests.find((request) =>
    request.comments.some((comment) => comment.moderationState === 'pending'),
  )
  const pendingComment = pendingCommentRequest?.comments.find(
    (comment) => comment.moderationState === 'pending',
  )

  return {
    requests,
    hiddenRequests,
    basePageSize: 20,
    searchTitle: 'Audit log actor filter',
    roadmapSearchTitle: 'Search misses exact phrase',
    detailSlug: 'portal-request-02',
    roadmapDetailSlug: 'portal-request-10',
    roadmapPlannedPortalCount: plannedIndices.size,
    roadmapPlannedPublishedCount: [...plannedIndices].filter((index) =>
      roadmapPublishedIndices.has(index),
    ).length,
    plannedCount: 5,
    commentCount: 4,
    expectedSearchCount: 1,
    hiddenTitles: hiddenRequests.map((request) => request.title),
    pendingRequestTitle: pendingRequest.title,
    pendingRequestSubjectId: pendingRequest.profileId,
    pendingRequestDetailSlug: pendingRequest.slug,
    pendingCommentTitle: pendingCommentRequest?.title ?? '',
    pendingCommentBody: pendingComment?.body ?? '',
    pendingCommentSubjectId: pendingComment?.subjectId ?? '',
    pendingCommentTargetId: pendingComment?.id ?? '',
    pendingCommentRequestSlug: pendingCommentRequest?.slug ?? '',
  }
}

function buildSeedSql(tenantId, data) {
  const allRequests = [...data.requests, ...(data.hiddenRequests ?? [])]

  const requestValues = allRequests
    .map((request) =>
      sqlTuple([
        request.id,
        tenantId,
        request.index,
        `CR-${request.index}`,
        request.title,
        request.summary,
        request.publicState,
        'none',
        'smoke',
        'smoke',
      ]),
    )
    .join(',\n  ')

  const profileValues = allRequests
    .map((request) =>
      sqlTuple([
        request.profileId,
        tenantId,
        request.id,
        request.slug,
        request.title,
        request.summary,
        request.publicState,
        request.roadmapColumn,
        request.includedInPortal,
        request.includedInRoadmap,
        raw('NOW()'),
        'smoke',
      ]),
    )
    .join(',\n  ')

  const requestSubjectValues = allRequests
    .map((request) =>
      sqlTuple([
        randomUUID(),
        tenantId,
        'request',
        request.profileId,
        request.moderationState ?? 'approved',
        '',
        '',
        'Portal visitor',
        'smoke-seed',
        'smoke',
        raw('NOW()'),
      ]),
    )
    .join(',\n  ')

  const commentValues = data.requests
    .flatMap((request) =>
      (request.comments ?? []).map((comment) =>
        sqlTuple([
          comment.id,
          tenantId,
          request.id,
          comment.body,
          'portal:smoke',
          'smoke-seed',
          'Portal visitor',
          'smoke',
        ]),
      ),
    )
    .join(',\n  ')

  const commentSubjectValues = data.requests
    .flatMap((request) =>
      (request.comments ?? []).map((comment) =>
        sqlTuple([
          comment.subjectId,
          tenantId,
          'request_comment',
          comment.id,
          comment.moderationState ?? 'approved',
          '',
          '',
          'Portal visitor',
          'smoke-seed',
          'smoke',
          raw('NOW()'),
        ]),
      ),
    )
    .join(',\n  ')

  return `
BEGIN;

INSERT INTO public_visibility_policies (
  tenant_id,
  portal_access_mode,
  search_indexing_enabled,
  requests_enabled,
  comments_enabled,
  roadmap_enabled,
  changelog_enabled,
  submission_write_mode,
  comment_write_mode,
  vote_write_mode,
  default_request_state,
  default_comment_state,
  submitter_identity_mode,
  show_vote_count,
  show_comment_count,
  show_submitter_display,
  hide_public_timestamps,
  updated_by
) VALUES (
  ${sqlValue(tenantId)},
  'public',
  TRUE,
  TRUE,
  TRUE,
  TRUE,
  FALSE,
  'anonymous',
  'anonymous',
  'anonymous',
  'pending',
  'pending',
  'display_name',
  TRUE,
  TRUE,
  TRUE,
  FALSE,
  'smoke'
) ON CONFLICT (tenant_id) DO UPDATE SET
  portal_access_mode = EXCLUDED.portal_access_mode,
  search_indexing_enabled = EXCLUDED.search_indexing_enabled,
  requests_enabled = EXCLUDED.requests_enabled,
  comments_enabled = EXCLUDED.comments_enabled,
  roadmap_enabled = EXCLUDED.roadmap_enabled,
  changelog_enabled = EXCLUDED.changelog_enabled,
  submission_write_mode = EXCLUDED.submission_write_mode,
  comment_write_mode = EXCLUDED.comment_write_mode,
  vote_write_mode = EXCLUDED.vote_write_mode,
  default_request_state = EXCLUDED.default_request_state,
  default_comment_state = EXCLUDED.default_comment_state,
  submitter_identity_mode = EXCLUDED.submitter_identity_mode,
  show_vote_count = EXCLUDED.show_vote_count,
  show_comment_count = EXCLUDED.show_comment_count,
  show_submitter_display = EXCLUDED.show_submitter_display,
  hide_public_timestamps = EXCLUDED.hide_public_timestamps,
  updated_by = EXCLUDED.updated_by;

INSERT INTO customer_requests (
  id,
  tenant_id,
  display_number,
  display_id,
  title,
  description,
  status,
  priority,
  created_by,
  updated_by
) VALUES
  ${requestValues};

INSERT INTO public_request_profiles (
  id,
  tenant_id,
  request_id,
  public_slug,
  public_title,
  public_summary,
  public_state,
  roadmap_column,
  included_in_portal,
  included_in_roadmap,
  published_at,
  updated_by
) VALUES
  ${profileValues};

INSERT INTO public_moderation_subjects (
  id,
  tenant_id,
  surface,
  subject_id,
  state,
  reason_code,
  reason_note,
  submitted_by_display,
  submitted_by_fingerprint,
  reviewed_by,
  reviewed_at
) VALUES
  ${requestSubjectValues};

INSERT INTO customer_request_comments (
  id,
  tenant_id,
  request_id,
  body,
  subject_key,
  subject_hash,
  subject_display,
  created_by
) VALUES
  ${commentValues};

INSERT INTO public_moderation_subjects (
  id,
  tenant_id,
  surface,
  subject_id,
  state,
  reason_code,
  reason_note,
  submitted_by_display,
  submitted_by_fingerprint,
  reviewed_by,
  reviewed_at
) VALUES
  ${commentSubjectValues};

COMMIT;
`
}

async function runDesktopSmoke(browserInstance, baseURL, data, tenant) {
  const context = await browserInstance.newContext()
  const page = await context.newPage()

  try {
    await gotoBoard(page, baseURL, tenant)
    await expect(page.locator('article.board-card')).toHaveCount(data.basePageSize)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(data.basePageSize)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await expect(page.getByRole('link', { name: 'Load more requests', exact: true })).toHaveCount(1)
    for (const hiddenTitle of data.hiddenTitles) {
      await expect(page.getByRole('link', { name: hiddenTitle, exact: true })).toHaveCount(0)
    }

    await page.getByRole('link', { name: 'Load more requests', exact: true }).click()
    await expect(page).toHaveURL(/cursor=20/)
    await expect(page.locator('article.board-card')).toHaveCount(2)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(2)

    await gotoBoard(page, baseURL, tenant)
    await expect(page.locator('article.board-card')).toHaveCount(data.basePageSize)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(data.basePageSize)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())

    const search = page.getByPlaceholder('Search requests or comments', { exact: true })
    await expect(search).toHaveCount(1)
    await search.fill('Audit log')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests\\?q=Audit\\+log`))
    await expect(page.locator('article.board-card')).toHaveCount(data.expectedSearchCount)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(
      data.expectedSearchCount,
    )
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await expect(page.getByRole('link', { name: data.searchTitle, exact: true })).toHaveCount(1)

    await clickCardSurface(page, 'article.board-card')
    await expect(page).toHaveURL(
      new RegExp(`/portal/${tenant.slug}/requests/${data.detailSlug}\\?q=Audit\\+log`),
    )
    await expect(page.getByRole('link', { name: 'Back to results', exact: true })).toHaveCount(1)
    await expect(page.getByLabel('Add a comment', { exact: true })).toHaveCount(1)
    const detailVote = page.locator('section.detail [data-vote-action]')
    await expect(detailVote).toHaveCount(1)
    const commentText = `Desktop browser smoke ${Date.now()}`
    await page.getByLabel('Add a comment', { exact: true }).fill(commentText)
    await page.getByRole('button', { name: 'Post comment', exact: true }).click()
    await expect(page.getByText(commentText, { exact: true })).toBeVisible()
    await detailVote.click()
    await expect(detailVote).toHaveText('Remove vote')

    const outsiderContext = await browserInstance.newContext()
    const outsiderPage = await outsiderContext.newPage()
    try {
      await outsiderPage.goto(
        `${baseURL}/portal/${tenant.slug}/requests/${data.detailSlug}?q=Audit+log`,
        { waitUntil: 'domcontentloaded' },
      )
      await expect(outsiderPage.getByRole('button', { name: 'Vote', exact: true })).toHaveCount(1)
      await expect(outsiderPage.getByText(commentText, { exact: true })).toHaveCount(0)
      await expect(outsiderPage.getByLabel('Add a comment', { exact: true })).toHaveCount(1)
    } finally {
      await outsiderContext.close()
    }

    await page.getByRole('link', { name: 'Back to results', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests(?:\\?|$)`))
    await expect(page).not.toHaveURL(
      new RegExp(`/portal/${tenant.slug}/requests/${data.detailSlug}`),
    )
    await expect(search).toHaveValue('Audit log')
    await expect(page.locator('article.board-card')).toHaveCount(1)
    await page.getByRole('link', { name: 'Clear filters', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests$`))
    await expect(page.locator('article.board-card')).toHaveCount(data.basePageSize)

    const myVotes = page.getByRole('checkbox', { name: 'My votes', exact: true })
    await expect(myVotes).toHaveCount(1)
    await myVotes.check()
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/voted=mine/)
    await expect(page.locator('article.board-card')).toHaveCount(1)
    await expect(page.getByRole('link', { name: data.searchTitle, exact: true })).toHaveCount(1)
    await expect(page.getByRole('button', { name: 'Remove vote', exact: true })).toHaveCount(1)

    await clickCardSurface(page, 'article.board-card')
    const removeVote = page.locator('section.detail [data-vote-action]')
    await expect(removeVote).toHaveText('Remove vote')
    await removeVote.click()
    await expect(removeVote).toHaveText('Vote')
    await page.getByRole('link', { name: 'Back to results', exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests(?:\\?|$)`))
    await expect(page).not.toHaveURL(
      new RegExp(`/portal/${tenant.slug}/requests/${data.detailSlug}`),
    )
    await expect(page.locator('article.board-card')).toHaveCount(0)
    await expect(
      page.getByText('No public requests matched the current filters.', { exact: false }),
    ).toBeVisible()
    await page
      .locator('section.empty')
      .getByRole('link', { name: 'Clear filters', exact: true })
      .click()
    await expect(page).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests$`))
    await expect(page.locator('article.board-card')).toHaveCount(data.basePageSize)

    await gotoBoard(page, baseURL, tenant)
    const stateInput = page.getByPlaceholder('Filter by state', { exact: true })
    await stateInput.fill('planned')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/state=planned/)
    await expect(page.locator('article.board-card')).toHaveCount(data.plannedCount)

    await gotoBoard(page, baseURL, tenant)
    const roadmapInput = page.getByPlaceholder('Filter by roadmap', { exact: true })
    await roadmapInput.fill('planned')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/roadmap=planned/)
    await expect(page.locator('article.board-card')).toHaveCount(data.roadmapPlannedPortalCount)

    await gotoBoard(page, baseURL, tenant)
    const commentsFilter = page.getByRole('checkbox', { name: 'With comments', exact: true })
    await commentsFilter.check()
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/comments=with/)
    await expect(page.locator('article.board-card')).toHaveCount(data.commentCount)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(data.commentCount)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    for (const title of [
      'Audit log actor filter',
      'Reply draft tone',
      'Portal request 17',
      'Portal request 19',
    ]) {
      await expect(page.getByRole('link', { name: title, exact: true })).toHaveCount(1)
    }

    await gotoBoard(page, baseURL, tenant)
    const searchAgain = page.getByPlaceholder('Search requests or comments', { exact: true })
    await searchAgain.fill('zzzzzz')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.board-card')).toHaveCount(0)
    await expect(
      page.getByText('No public requests matched the current filters.', { exact: false }),
    ).toBeVisible()
    await expect(
      page.locator('section.empty').getByRole('link', { name: 'Clear filters', exact: true }),
    ).toHaveCount(1)

    await gotoBoard(page, baseURL, tenant)
    const hiddenSearch = page.getByPlaceholder('Search requests or comments', { exact: true })
    await hiddenSearch.fill(data.hiddenTitles[0])
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.board-card')).toHaveCount(0)
    await expect(
      page.getByText('No public requests matched the current filters.', { exact: false }),
    ).toBeVisible()

    await gotoRoadmap(page, baseURL, tenant)
    await expect(page).toHaveTitle(`${tenant.name} | Public roadmap`)
    await expect(page.locator('article.roadmap-column')).toHaveCount(4)
    await expect(page.getByRole('link', { name: 'Browse requests', exact: true })).toHaveAttribute(
      'href',
      `/portal/${tenant.slug}/requests`,
    )
    await expect(
      page.getByRole('link', { name: 'Submit new feedback', exact: true }),
    ).toHaveAttribute('href', `/portal/${tenant.slug}`)

    const roadmapSearch = page.getByPlaceholder('Search requests or comments', { exact: true })
    await roadmapSearch.fill(data.roadmapSearchTitle)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(
      new RegExp(
        `/portal/${tenant.slug}/roadmap\\?q=${encodeURIComponent(
          data.roadmapSearchTitle,
        ).replaceAll('%20', '\\+')}`,
      ),
    )
    await expect(page.locator('article.roadmap-card')).toHaveCount(1)
    await expect(page.locator('article.roadmap-card [data-freshness]')).toHaveCount(1)
    await assertFreshnessTag(page.locator('article.roadmap-card [data-freshness]').first())
    await expect(
      page.getByRole('link', { name: data.roadmapSearchTitle, exact: true }),
    ).toHaveCount(1)

    await clickCardSurface(page, 'article.roadmap-card')
    await expect(page).toHaveURL(
      new RegExp(
        `/portal/${tenant.slug}/requests/${data.roadmapDetailSlug}\\?q=${encodeURIComponent(
          data.roadmapSearchTitle,
        ).replaceAll('%20', '\\+')}&back=%2Fportal%2F${tenant.slug}%2Froadmap`,
      ),
    )
    await expect(page.getByRole('link', { name: 'Back to results', exact: true })).toHaveCount(1)
    await page.getByRole('link', { name: 'Back to results', exact: true }).click()
    await expect(page).toHaveURL(
      new RegExp(
        `/portal/${tenant.slug}/roadmap\\?q=${encodeURIComponent(
          data.roadmapSearchTitle,
        ).replaceAll('%20', '\\+')}`,
      ),
    )
    await expect(
      page.getByRole('link', { name: data.roadmapSearchTitle, exact: true }),
    ).toHaveCount(1)
    await expect(page.getByRole('heading', { name: 'Public roadmap', exact: true })).toHaveCount(1)
  } finally {
    await context.close()
  }
}

async function runTenantIsolationSmoke(browserInstance, baseURL, data, tenantA, tenantB) {
  const context = await browserInstance.newContext()
  const page = await context.newPage()

  try {
    await gotoBoard(page, baseURL, tenantA)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(data.basePageSize)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await page.getByPlaceholder('Search requests or comments', { exact: true }).fill('Audit log')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(1)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await clickCardSurface(page, 'article.board-card')
    const tenantAVote = page.locator('section.detail [data-vote-action]')
    await expect(tenantAVote).toHaveText('Vote')
    await tenantAVote.click()
    await expect(tenantAVote).toHaveText('Remove vote')

    await gotoBoard(page, baseURL, tenantB)
    await page.getByPlaceholder('Search requests or comments', { exact: true }).fill('Audit log')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(1)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await clickCardSurface(page, 'article.board-card')
    const tenantBVote = page.locator('section.detail [data-vote-action]')
    await expect(tenantBVote).toHaveText('Vote')
    await tenantBVote.click()
    await expect(tenantBVote).toHaveText('Remove vote')

    await gotoBoard(page, baseURL, tenantA)
    await page.getByPlaceholder('Search requests or comments', { exact: true }).fill('Audit log')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await clickCardSurface(page, 'article.board-card')
    await expect(page.locator('section.detail [data-vote-action]')).toHaveText('Remove vote')
  } finally {
    await context.close()
  }
}

async function runMobileSmoke(browserInstance, baseURL, data, tenant) {
  const context = await browserInstance.newContext({ viewport: { width: 390, height: 844 } })
  const page = await context.newPage()

  try {
    await gotoBoard(page, baseURL, tenant)
    const metrics = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      innerWidth: window.innerWidth,
    }))
    if (
      metrics.scrollWidth > metrics.innerWidth + 1 ||
      metrics.scrollWidth > metrics.clientWidth + 1
    ) {
      throw new Error(`mobile viewport overflow: ${JSON.stringify(metrics)}`)
    }
    await expect(page.locator('article.board-card')).toHaveCount(data.basePageSize)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(data.basePageSize)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())

    const search = page.getByPlaceholder('Search requests or comments', { exact: true })
    await search.fill('Audit log')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(/q=Audit\+log/)
    await expect(page.locator('article.board-card')).toHaveCount(1)
    await expect(page.locator('article.board-card [data-freshness]')).toHaveCount(1)
    await assertFreshnessTag(page.locator('article.board-card [data-freshness]').first())
    await clickCardSurface(page, 'article.board-card')
    await expect(page.getByRole('link', { name: 'Back to results', exact: true })).toHaveCount(1)
    await expect(page.locator('section.detail [data-vote-action]')).toHaveCount(1)

    await gotoRoadmap(page, baseURL, tenant)
    const roadmapMetrics = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      innerWidth: window.innerWidth,
    }))
    if (
      roadmapMetrics.scrollWidth > roadmapMetrics.innerWidth + 1 ||
      roadmapMetrics.scrollWidth > roadmapMetrics.clientWidth + 1
    ) {
      throw new Error(`mobile roadmap overflow: ${JSON.stringify(roadmapMetrics)}`)
    }
    await expect(page.locator('article.roadmap-column')).toHaveCount(4)
    const roadmapSearch = page.getByPlaceholder('Search requests or comments', { exact: true })
    await roadmapSearch.fill(data.roadmapSearchTitle)
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page).toHaveURL(
      new RegExp(
        `/portal/${tenant.slug}/roadmap\\?q=${encodeURIComponent(
          data.roadmapSearchTitle,
        ).replaceAll('%20', '\\+')}`,
      ),
    )
    await expect(page.locator('article.roadmap-card')).toHaveCount(1)
    await expect(page.locator('article.roadmap-card [data-freshness]')).toHaveCount(1)
    await assertFreshnessTag(page.locator('article.roadmap-card [data-freshness]').first())
    await clickCardSurface(page, 'article.roadmap-card')
    await expect(page.getByRole('link', { name: 'Back to results', exact: true })).toHaveCount(1)
    await page.getByRole('link', { name: 'Back to results', exact: true }).click()
    await expect(page).toHaveURL(
      new RegExp(
        `/portal/${tenant.slug}/roadmap\\?q=${encodeURIComponent(
          data.roadmapSearchTitle,
        ).replaceAll('%20', '\\+')}`,
      ),
    )
  } finally {
    await context.close()
  }
}

async function runConsoleSmoke(browserInstance, baseURL, tenant, data) {
  const context = await browserInstance.newContext({ viewport: { width: 1440, height: 1200 } })
  const page = await context.newPage()

  try {
    await page.goto(`${baseURL}/console/login`, { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('heading', { name: 'Attune Console', exact: true })).toHaveCount(1)
    await expect(page.getByLabel('邮箱', { exact: true })).toHaveCount(1)
    await expect(page.getByLabel('密码', { exact: true })).toHaveCount(1)

    await page.getByLabel('邮箱', { exact: true }).fill(consoleAdmin.email)
    await page.getByLabel('密码', { exact: true }).fill(consoleAdmin.password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page).toHaveURL(/\/console\/control-tower(?:\?.*)?$/)
    await expect(page.getByRole('heading', { name: '控制塔', exact: true })).toHaveCount(1)

    await page.goto(`${baseURL}/console/integrations/public-visibility`, {
      waitUntil: 'domcontentloaded',
    })
    await expect(page).toHaveTitle('公开可见性 - Attune Console')
    await expect(page.getByRole('heading', { name: '公开可见性', exact: true })).toHaveCount(1)

    const commentBoard = await context.newPage()
    try {
      await gotoBoard(commentBoard, baseURL, tenant)
      await commentBoard
        .getByPlaceholder('Search requests or comments', { exact: true })
        .fill('Audit log')
      await commentBoard.getByRole('button', { name: 'Search', exact: true }).click()
      await clickCardSurface(commentBoard, 'article.board-card')
      await expect(commentBoard.getByText(data.pendingCommentBody, { exact: true })).toHaveCount(0)
      await expect(commentBoard.getByText('1 comments', { exact: true }).first()).toBeVisible()
    } finally {
      await commentBoard.close()
    }

    await approveModerationSubject(page, data.pendingRequestSubjectId)
    await approveModerationSubject(page, data.pendingCommentTargetId)

    const publicBoard = await context.newPage()
    try {
      await gotoBoard(publicBoard, baseURL, tenant)
      const publicSearch = publicBoard.getByPlaceholder('Search requests or comments', {
        exact: true,
      })
      await expect(publicSearch).toHaveCount(1)

      await publicSearch.fill(data.pendingRequestTitle)
      await publicBoard.getByRole('button', { name: 'Search', exact: true }).click()
      await expect(
        publicBoard.getByRole('link', { name: data.pendingRequestTitle, exact: true }),
      ).toHaveCount(1)
      await clickCardSurface(publicBoard, 'article.board-card')
      await expect(
        publicBoard.getByRole('heading', { name: data.pendingRequestTitle, exact: true }),
      ).toBeVisible()
      await publicBoard.getByRole('link', { name: 'Back to results', exact: true }).click()

      await publicSearch.fill('Audit log')
      await publicBoard.getByRole('button', { name: 'Search', exact: true }).click()
      await clickCardSurface(publicBoard, 'article.board-card')
      await expect(publicBoard.getByText(data.pendingCommentBody, { exact: true })).toBeVisible()
      await expect(publicBoard.getByText('2 comments', { exact: true }).first()).toBeVisible()
    } finally {
      await publicBoard.close()
    }

    const boardLink = page.locator(`a[href="/portal/${tenant.slug}/requests"]`)
    const roadmapLink = page.locator(`a[href="/portal/${tenant.slug}/roadmap"]`)
    const portalLink = page.locator(`a[href="/portal/${tenant.slug}"]`)
    await expect(boardLink).toHaveAttribute('href', `/portal/${tenant.slug}/requests`)
    await expect(roadmapLink).toHaveAttribute('href', `/portal/${tenant.slug}/roadmap`)
    await expect(portalLink).toHaveAttribute('href', `/portal/${tenant.slug}`)

    const boardPopupPromise = page.waitForEvent('popup')
    await boardLink.click()
    const boardPopup = await boardPopupPromise
    await boardPopup.waitForLoadState('domcontentloaded')
    await expect(boardPopup).toHaveURL(new RegExp(`/portal/${tenant.slug}/requests(?:\\?|$)`))
    await expect(
      boardPopup.getByRole('heading', { name: 'Public board', exact: true }),
    ).toHaveCount(1)

    const roadmapPopupPromise = page.waitForEvent('popup')
    await roadmapLink.click()
    const roadmapPopup = await roadmapPopupPromise
    await roadmapPopup.waitForLoadState('domcontentloaded')
    await expect(roadmapPopup).toHaveURL(new RegExp(`/portal/${tenant.slug}/roadmap(?:\\?|$)`))
    await expect(roadmapPopup).toHaveTitle(`${tenant.name} | Public roadmap`)

    const portalPopupPromise = page.waitForEvent('popup')
    await portalLink.click()
    const portalPopup = await portalPopupPromise
    await portalPopup.waitForLoadState('domcontentloaded')
    await expect(portalPopup).toHaveURL(new RegExp(`/portal/${tenant.slug}(?:\\?|$)`))
    await expect(portalPopup.getByRole('heading').first()).toBeVisible()
  } finally {
    await context.close()
  }
}

async function approveModerationSubject(page, subjectId) {
  if (!subjectId) {
    throw new Error('pending moderation subject id missing')
  }
  const row = page.locator('li').filter({ hasText: subjectId }).first()
  await expect(row).toHaveCount(1)
  await row.getByRole('button', { name: '批准', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText(subjectId, { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '提交审核', exact: true }).click()
  await expect(page.getByText('审核状态已更新', { exact: true }).first()).toBeVisible()
}

async function verifyRoadmapApi(baseURL, data, tenant) {
  const response = await fetch(
    `${baseURL}/v1/portal/${tenant.slug}/roadmap?roadmap=planned&limit=10`,
  )
  if (!response.ok) {
    throw new Error(`roadmap API failed with HTTP ${response.status}`)
  }
  const json = await response.json()
  if (!Array.isArray(json.columns) || json.columns.length !== 4) {
    throw new Error(`roadmap API columns mismatch: ${JSON.stringify(json)}`)
  }
  const columnNames = json.columns.map((column) => column.name)
  const wantColumnNames = ['under consideration', 'planned', 'in progress', 'shipped']
  if (JSON.stringify(columnNames) !== JSON.stringify(wantColumnNames)) {
    throw new Error(
      `roadmap API column names = ${JSON.stringify(columnNames)}, want ${JSON.stringify(wantColumnNames)}`,
    )
  }
  const column = json.columns.find((item) => item.name === 'planned')
  if (
    !column ||
    !Array.isArray(column.requests) ||
    column.requests.length !== data.roadmapPlannedPublishedCount
  ) {
    throw new Error(
      `roadmap API request count = ${column?.requests?.length ?? 'n/a'}, want ${data.roadmapPlannedPublishedCount}`,
    )
  }
}

async function gotoBoard(page, baseURL, tenant) {
  await page.goto(`${baseURL}/portal/${tenant.slug}/requests`, { waitUntil: 'domcontentloaded' })
  await expect(page).toHaveTitle(`${tenant.name} | Public board`)
}

async function gotoRoadmap(page, baseURL, tenant) {
  await page.goto(`${baseURL}/portal/${tenant.slug}/roadmap`, { waitUntil: 'domcontentloaded' })
  await expect(page).toHaveTitle(`${tenant.name} | Public roadmap`)
}

async function clickCardSurface(page, selector) {
  const card = page.locator(selector)
  const overlay = card.locator('a.card-overlay')
  if ((await overlay.count()) === 1) {
    await overlay.click()
    return
  }
  const box = await card.boundingBox()
  if (!box) {
    throw new Error(`unable to locate visible card for ${selector}`)
  }
  await page.mouse.click(Math.round(box.x + box.width * 0.5), Math.round(box.y + box.height * 0.45))
}

async function assertFreshnessTag(locator) {
  await expect(locator).toHaveText(/^(Updated|Published) [A-Z][a-z]{2} \d{1,2}$/)
  await expect(locator).toHaveAttribute('datetime', /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)
  await expect(locator).toHaveAttribute(
    'title',
    /^(Updated|Published) \d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/,
  )
  await expect(locator).toHaveAttribute(
    'aria-label',
    /^(Updated|Published) \d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC$/,
  )
}

async function bootstrapDemoTenant(binaryPath, configPath, repoRoot, dsn, tenant) {
  execFileSync(
    binaryPath,
    ['--config', configPath, 'tenant', 'create', '--slug', tenant.slug, '--name', tenant.name],
    {
      cwd: repoRoot,
      stdio: 'inherit',
    },
  )
  const tenantID = await psqlScalar(
    dsn,
    `SELECT id FROM tenants WHERE slug = ${sqlValue(tenant.slug)}`,
  )
  if (!tenantID) {
    throw new Error(`tenant bootstrap did not return an id for ${tenant.slug}`)
  }
  return tenantID
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
      'failed to launch a Chromium-family browser for the public board smoke test.',
      'Set ATTUNE_PUBLIC_BOARD_E2E_EXECUTABLE_PATH=/absolute/path/to/browser if auto-detection misses your install.',
      ...attempts.map((attempt) => `- ${attempt}`),
    ].join('\n'),
  )
}

async function browserLaunchOptions() {
  const options = []
  const explicitExecutable = process.env.ATTUNE_PUBLIC_BOARD_E2E_EXECUTABLE_PATH
  if (explicitExecutable) {
    options.push({ executablePath: explicitExecutable, headless: true })
  }
  const explicitChannel = process.env.ATTUNE_PUBLIC_BOARD_E2E_CHANNEL
  if (explicitChannel) {
    options.push({ channel: explicitChannel, headless: true })
  }
  for (const executablePath of await cachedChromiumExecutables()) {
    options.push({ executablePath, headless: true })
  }
  options.push({ channel: 'chrome', headless: true })
  options.push({ channel: 'msedge', headless: true })
  options.push({ headless: true })
  return dedupeLaunchOptions(options)
}

async function cachedChromiumExecutables() {
  const home = os.homedir()
  const roots = [
    path.join(home, 'Library/Caches/ms-playwright'),
    path.join(home, '.cache/ms-playwright'),
  ]
  const out = []
  for (const root of roots) {
    try {
      await access(root, fsConstants.R_OK)
    } catch {
      continue
    }
    const dirs = await readDir(root)
    const chromiumDirs = dirs
      .filter((entry) => entry.isDirectory() && entry.name.startsWith('chromium-'))
      .map((entry) => entry.name)
      .sort((a, b) => chromiumRevision(b) - chromiumRevision(a) || b.localeCompare(a))
    for (const dir of chromiumDirs) {
      for (const candidate of chromiumExecutableCandidates(path.join(root, dir))) {
        if (await canRead(candidate)) out.push(candidate)
      }
    }
  }
  return [...new Set(out)]
}

async function readDir(root) {
  try {
    const { readdir } = await import('node:fs/promises')
    return await readdir(root, { withFileTypes: true })
  } catch {
    return []
  }
}

function chromiumRevision(dirName) {
  const match = /^chromium-(\d+)$/.exec(dirName)
  return match ? Number(match[1]) : -1
}

function chromiumExecutableCandidates(root) {
  return [
    path.join(root, 'chrome-linux', 'chrome'),
    path.join(root, 'chrome-mac', 'Chromium.app', 'Contents', 'MacOS', 'Chromium'),
    path.join(root, 'chrome-win', 'chrome.exe'),
  ]
}

async function canRead(candidate) {
  try {
    await access(candidate, fsConstants.X_OK)
    return true
  } catch {
    return false
  }
}

function dedupeLaunchOptions(options) {
  const seen = new Set()
  return options.filter((option) => {
    const key = JSON.stringify({
      executablePath: option.executablePath ?? '',
      channel: option.channel ?? '',
      headless: option.headless ?? false,
    })
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function describeLaunchOption(option) {
  if (option.executablePath) return `executablePath=${option.executablePath}`
  if (option.channel) return `channel=${option.channel}`
  return 'default-launch'
}

function sqlValue(value) {
  if (value === null || value === undefined) return 'NULL'
  if (typeof value === 'boolean') return value ? 'TRUE' : 'FALSE'
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : 'NULL'
  if (typeof value === 'object' && value.__rawSql) return value.__rawSql
  return `'${String(value).replace(/'/g, "''")}'`
}

function raw(sql) {
  return { __rawSql: sql }
}

function sqlTuple(values) {
  return `(${values.map(sqlValue).join(', ')})`
}

function indent(text, spaces) {
  return text
    .split('\n')
    .map((line) => `${' '.repeat(spaces)}${line}`)
    .join('\n')
}

async function resolveCommand(command) {
  const envKey = `ATTUNE_PUBLIC_BOARD_E2E_${command.replace(/[^a-z]/gi, '_').toUpperCase()}`
  if (process.env[envKey]) {
    await ensureExecutable(process.env[envKey], command)
    return process.env[envKey]
  }
  const homebrew =
    command === 'initdb' || command === 'pg_ctl' || command === 'psql' || command === 'pg_isready'
      ? `/opt/homebrew/bin/${command}`
      : ''
  if (homebrew && (await canRead(homebrew))) return homebrew
  try {
    const resolved = execFileSync('which', [command], { encoding: 'utf8' }).trim()
    if (resolved) return resolved
  } catch {
    // fall through
  }
  throw new Error(`missing required command: ${command}`)
}

async function ensureExecutable(candidate, label) {
  try {
    await access(candidate, fsConstants.X_OK)
  } catch {
    throw new Error(`configured ${label} path is not executable: ${candidate}`)
  }
}

function pickFreePort(excludedPorts = []) {
  const blocked = Array.isArray(excludedPorts) ? excludedPorts : [excludedPorts]
  const args = ['node', pickFreePortScript, 'random', ...blocked.map((port) => String(port))]
  const port = Number(execFileSync(args[0], args.slice(1), { encoding: 'utf8' }).trim())
  if (!Number.isInteger(port) || port < 1) {
    throw new Error(`could not allocate a port from ${args.join(' ')}`)
  }
  return port
}

async function waitForPg(pgIsReady, port, dbName) {
  for (let attempt = 1; attempt <= 60; attempt++) {
    try {
      execFileSync(pgIsReady, ['-h', '127.0.0.1', '-p', String(port), '-U', dbUser, '-d', dbName], {
        stdio: 'ignore',
      })
      return
    } catch {
      await sleep(1000)
    }
  }
  throw new Error('postgres did not become ready in time')
}

async function waitForHttpOk(url, label) {
  for (let attempt = 1; attempt <= 60; attempt++) {
    try {
      const response = await fetch(url)
      if (response.ok) return
    } catch {
      // retry
    }
    await sleep(1000)
  }
  throw new Error(`${label} did not become ready in time`)
}

async function psqlScalar(dsn, sql) {
  const output = execFileSync(psqlPath, ['-v', 'ON_ERROR_STOP=1', '-d', dsn, '-tAc', sql], {
    encoding: 'utf8',
  }).trim()
  return output || ''
}

async function execPsql(dsn, sql) {
  execFileSync(psqlPath, ['-v', 'ON_ERROR_STOP=1', '-d', dsn], {
    input: sql,
    encoding: 'utf8',
  })
}

async function openWriteHandle(filePath) {
  const { open } = await import('node:fs/promises')
  return open(filePath, 'w')
}

async function dumpTail(filePath, lines) {
  try {
    const content = await readFile(filePath, 'utf8')
    const tail = content.split('\n').slice(-lines).join('\n')
    console.error(`\n--- ${path.basename(filePath)} tail ---\n${tail}\n--- end tail ---`)
  } catch {
    // ignore
  }
}

function log(message) {
  process.stdout.write(`portal-smoke: ${message}\n`)
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
