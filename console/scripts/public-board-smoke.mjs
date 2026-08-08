#!/usr/bin/env node

import { execFileSync, spawn } from 'node:child_process'
import { createHash, randomUUID } from 'node:crypto'
import { constants as fsConstants, readFileSync } from 'node:fs'
import { access, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { createRequire } from 'node:module'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium, expect } from '@playwright/test'

const scriptDir = path.dirname(fileURLToPath(import.meta.url))
const repoRoot = path.resolve(scriptDir, '..', '..')
const consoleDir = path.join(repoRoot, 'console')
const pickFreePortScript = path.join(repoRoot, 'scripts', 'pick-free-port.mjs')
const require = createRequire(import.meta.url)
const axeSource = readFileSync(require.resolve('axe-core/axe.min.js'), 'utf8')
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
let mailProvider = null

const tenantASeed = buildSeedData()
const tenantBSeed = buildSeedData()
const npsSmoke = buildNPSSmokeData()

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
  log('check console bundle budget')
  execFileSync('pnpm', ['run', 'check:bundle'], {
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
  mailProvider = await startMailProvider()

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
security:
  allow_loopback_egress: true
  allow_private_egress: true
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
  await seedNPSAudience(baseURL, dsn, tenantAId, tenantA, tenantASeed, npsSmoke)

  log('launch browser')
  browser = await launchBrowser()
  await runDesktopSmoke(browser, baseURL, tenantASeed, tenantA)
  await runTenantIsolationSmoke(browser, baseURL, tenantASeed, tenantA, tenantB)
  await runMobileSmoke(browser, baseURL, tenantASeed, tenantA)
  const tenantAFingerprints = await runPublicSurveySmoke(
    browser,
    baseURL,
    dsn,
    tenantAId,
    tenantASeed,
  )
  const tenantBFingerprints = await runPublicSurveySmoke(
    browser,
    baseURL,
    dsn,
    tenantBId,
    tenantBSeed,
  )
  if (tenantAFingerprints === tenantBFingerprints) {
    throw new Error('public survey fingerprints must be scoped to the invitation tenant')
  }
  await verifyRoadmapApi(baseURL, tenantASeed, tenantA)
  await runConsoleSmoke(browser, baseURL, tenantA, tenantASeed)
  await runNPSCampaignSmoke(browser, baseURL, dsn, tenantAId, npsSmoke, mailProvider)

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
    await mailProvider?.close().catch(() => {})
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
    survey: buildSurveySeedData(),
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

function buildSurveySeedData() {
  return {
    campaignId: randomUUID(),
    invitationId: randomUUID(),
    token: `survey-smoke-${randomUUID().replace(/-/g, '')}`,
    sourceId: 'public-survey-smoke-source',
    content: {
      title: 'Resolution feedback',
      intro: 'Help us understand whether the customer loop was actually closed.',
      question: 'How satisfied are you with this resolution?',
      comment_prompt: 'What should we know?',
      thank_you: 'Thanks for closing the loop.',
    },
  }
}

function buildNPSSmokeData() {
  const key = randomUUID().replace(/-/g, '')
  return {
    cohortSourceId: randomUUID(),
    cohortId: randomUUID(),
    cohortMembershipId: randomUUID(),
    unlinkedCohortMembershipId: randomUUID(),
    ownerMemberId: randomUUID(),
    contactId: '',
    cohortName: `NPS browser cohort ${key.slice(0, 8)}`,
    campaignName: `NPS browser campaign ${key.slice(0, 8)}`,
    subjectKey: `nps-browser-subject-${key}`,
    unlinkedSubjectKey: `nps-browser-unlinked-${key}`,
    subjectHash: '',
    recipientEmail: `nps-browser-recipient-${key.slice(0, 16)}@example.test`,
    ownerEmail: `nps-browser-owner-${key.slice(0, 16)}@example.test`,
    comment: `NPS browser detractor feedback ${key.slice(0, 12)}`,
    promotedRequestTitle: `NPS browser request ${key.slice(0, 12)}`,
    promotedRequestDescription: 'Owner-reviewed request created from NPS feedback.',
  }
}

async function seedNPSAudience(baseURL, dsn, tenantId, tenant, data, nps) {
  const publicSlug = data.requests[0]?.slug
  if (!publicSlug) throw new Error('NPS smoke requires a seeded public request')

  const subscribe = await fetch(
    `${baseURL}/v1/portal/${tenant.slug}/requests/${publicSlug}/subscribe`,
    {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        email: nps.recipientEmail,
        notifyMe: true,
        notificationConsentTextVersion: 'nps-browser-smoke',
        displayName: 'NPS browser recipient',
        organization: 'Attune browser smoke',
        locale: 'en',
        timezone: 'UTC',
      }),
    },
  )
  if (!subscribe.ok) {
    throw new Error(`NPS smoke contact subscription failed with HTTP ${subscribe.status}`)
  }

  nps.subjectHash = subjectKeyHash(tenantId, nps.subjectKey)
  const emailHash = emailHashForSmoke(nps.recipientEmail)
  const ownerUserID = `nps-browser-owner-${nps.ownerMemberId}`
  await execPsql(
    dsn,
    `
BEGIN;

INSERT INTO tenant_members (
  id,
  tenant_id,
  member_type,
  user_id,
  role,
  role_source,
  email,
  accepted_at
) VALUES (
  ${sqlValue(nps.ownerMemberId)},
  ${sqlValue(tenantId)},
  'tenant_user',
  ${sqlValue(ownerUserID)},
  'member',
  'manual',
  ${sqlValue(nps.ownerEmail)},
  NOW()
);

INSERT INTO cohort_sources (
  id,
  tenant_id,
  provider,
  name,
  created_by,
  updated_by
) VALUES (
  ${sqlValue(nps.cohortSourceId)},
  ${sqlValue(tenantId)},
  'amplitude',
  'NPS browser smoke source',
  'smoke',
  'smoke'
);

INSERT INTO cohorts (
  id,
  tenant_id,
  cohort_source_id,
  external_cohort_id,
  name,
  member_count,
  last_synced_at
) VALUES (
  ${sqlValue(nps.cohortId)},
  ${sqlValue(tenantId)},
  ${sqlValue(nps.cohortSourceId)},
  'nps-browser-smoke',
  ${sqlValue(nps.cohortName)},
  2,
  NOW()
);

INSERT INTO cohort_memberships (
  id,
  tenant_id,
  cohort_id,
  external_user_id,
  email,
  display_name
) VALUES (
  ${sqlValue(nps.cohortMembershipId)},
  ${sqlValue(tenantId)},
  ${sqlValue(nps.cohortId)},
  ${sqlValue(nps.subjectKey)},
  ${sqlValue(nps.recipientEmail)},
  'NPS browser recipient'
), (
  ${sqlValue(nps.unlinkedCohortMembershipId)},
  ${sqlValue(tenantId)},
  ${sqlValue(nps.cohortId)},
  ${sqlValue(nps.unlinkedSubjectKey)},
  '',
  'NPS browser excluded member'
);

UPDATE customer_notification_contacts
SET subject_key = ${sqlValue(nps.subjectKey)},
    subject_hash = ${sqlValue(nps.subjectHash)},
    display_name = 'NPS browser recipient'
WHERE tenant_id = ${sqlValue(tenantId)}
  AND email_hash = ${sqlValue(emailHash)};

COMMIT;
`,
  )

  nps.contactId = await psqlScalar(
    dsn,
    `SELECT id
       FROM customer_notification_contacts
      WHERE tenant_id = ${sqlValue(tenantId)}
        AND email_hash = ${sqlValue(emailHash)}
        AND subject_key = ${sqlValue(nps.subjectKey)}`,
  )
  if (!nps.contactId) throw new Error('NPS smoke contact identity bridge was not seeded')
}

function buildSeedSql(tenantId, data) {
  const allRequests = [...data.requests, ...(data.hiddenRequests ?? [])]
  const survey = data.survey
  const nextCustomerRequestNumber = Math.max(0, ...allRequests.map((request) => request.index)) + 1

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

INSERT INTO customer_request_counters (tenant_id, next_number)
VALUES (${sqlValue(tenantId)}, ${sqlValue(nextCustomerRequestNumber)})
ON CONFLICT (tenant_id) DO UPDATE
SET next_number = GREATEST(customer_request_counters.next_number, EXCLUDED.next_number);

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

INSERT INTO survey_campaigns (
  id,
  tenant_id,
  name,
  survey_type,
  status,
  trigger_event,
  distribution_mode,
  dedupe_policy,
  trigger_filter,
  content,
  locale,
  content_version,
  sampling_percent,
  min_days_between_contact,
  expires_after_days,
  max_daily_invitations,
  low_score_threshold,
  require_recent_customer_activity,
  recent_activity_days,
  suppress_auto_resolved,
  created_by,
  updated_by
) VALUES (
  ${sqlValue(survey.campaignId)},
  ${sqlValue(tenantId)},
  'Public survey smoke',
  'csat',
  'active',
  'manual_link',
  'source_link',
  'one_per_source',
  ${sqlJsonb({})},
  ${sqlJsonb(survey.content)},
  'en',
  1,
  100,
  0,
  14,
  0,
  3,
  FALSE,
  30,
  FALSE,
  'smoke',
  'smoke'
);

INSERT INTO survey_invitations (
  id,
  tenant_id,
  campaign_id,
  campaign_content_version,
  campaign_snapshot,
  dedupe_key,
  source_type,
  source_id,
  distribution_mode,
  token_hash,
  delivery_status,
  response_status,
  suppression_status,
  recipient_snapshot,
  expires_at,
  created_by
) VALUES (
  ${sqlValue(survey.invitationId)},
  ${sqlValue(tenantId)},
  ${sqlValue(survey.campaignId)},
  1,
  ${sqlJsonb({ campaign_id: survey.campaignId, content: survey.content })},
  ${sqlValue(`manual:${survey.sourceId}`)},
  'manual',
  ${sqlValue(survey.sourceId)},
  'source_link',
  ${sqlValue(surveyTokenHash(survey.token))},
  'not_applicable',
  'not_started',
  'not_suppressed',
  ${sqlJsonb({ display_name: 'Browser smoke recipient' })},
  NOW() + INTERVAL '14 days',
  'smoke'
);

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
      .getByRole('link', { name: 'Show all requests', exact: true })
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
      page.locator('section.empty').getByRole('link', { name: 'Show all requests', exact: true }),
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

    await gotoRoadmap(page, baseURL, tenant)
    const roadmapEmptySearch = page.getByPlaceholder('Search requests or comments', {
      exact: true,
    })
    await roadmapEmptySearch.fill('zzzzzz')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.roadmap-card')).toHaveCount(0)
    await expect(
      page.getByText('No public roadmap items matched the current filters.', { exact: false }),
    ).toBeVisible()
    await expect(
      page
        .locator('section.empty')
        .getByRole('link', { name: 'Show all roadmap items', exact: true }),
    ).toHaveCount(1)
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
    await gotoRoadmap(page, baseURL, tenant)
    const roadmapEmptySearch = page.getByPlaceholder('Search requests or comments', {
      exact: true,
    })
    await roadmapEmptySearch.fill('zzzzzz')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.locator('article.roadmap-card')).toHaveCount(0)
    await expect(
      page.getByText('No public roadmap items matched the current filters.', { exact: false }),
    ).toBeVisible()
    await expect(
      page
        .locator('section.empty')
        .getByRole('link', { name: 'Show all roadmap items', exact: true }),
    ).toHaveCount(1)
  } finally {
    await context.close()
  }
}

async function runPublicSurveySmoke(browserInstance, baseURL, dsn, tenantId, data) {
  const survey = data.survey
  const context = await browserInstance.newContext({
    bypassCSP: true,
    viewport: { width: 1365, height: 768 },
  })
  const page = await context.newPage()
  const comment = `Browser low-score response ${Date.now()}`

  try {
    const response = await page.goto(`${baseURL}/surveys/${survey.token}?score=2`, {
      waitUntil: 'domcontentloaded',
    })
    assertPublicSurveyHeaders(response)
    await expect(page).toHaveTitle('Resolution feedback | Attune survey')
    await expect(
      page.getByRole('heading', { name: 'Resolution feedback', exact: true }),
    ).toBeVisible()
    await expect(page.getByText(survey.content.intro, { exact: true })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Score 2', exact: true })).toBeChecked()
    await assertNoDocumentOverflow(page, 'public survey desktop')
    await expectNoAxeViolations(page)

    await assertPublicSurveyMobileRender(browserInstance, baseURL, survey)

    await page.getByLabel(survey.content.comment_prompt, { exact: true }).fill(comment)
    await page.getByRole('button', { name: 'Submit feedback', exact: true }).click()
    const status = page.getByRole('status')
    await expect(status).toContainText(survey.content.thank_you)
    await expect(status).toContainText('Your response has been flagged for review.')
    await expect(page.getByRole('button', { name: 'Submit feedback', exact: true })).toHaveCount(0)
    await assertNoDocumentOverflow(page, 'public survey submitted')
    await expectNoAxeViolations(page)

    await page.goto(`${baseURL}/surveys/${survey.token}`, { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('status')).toHaveText('This survey has already been submitted.')
    await expect(page.getByRole('button', { name: 'Submit feedback', exact: true })).toHaveCount(0)

    const responseRecord = await psqlScalar(
      dsn,
      `SELECT score::text || '|' || comment
         FROM survey_responses
        WHERE tenant_id = ${sqlValue(tenantId)}
          AND invitation_id = ${sqlValue(survey.invitationId)}`,
    )
    if (responseRecord !== `2|${comment}`) {
      throw new Error(`public survey response row mismatch: ${responseRecord}`)
    }
    const reviewCount = await psqlScalar(
      dsn,
      `SELECT COUNT(*)
         FROM survey_low_score_reviews
        WHERE tenant_id = ${sqlValue(tenantId)}
          AND campaign_id = ${sqlValue(survey.campaignId)}`,
    )
    if (reviewCount !== '1') {
      throw new Error(`public survey low-score review count = ${reviewCount}, want 1`)
    }
    const fingerprints = await psqlScalar(
      dsn,
      `SELECT user_agent_hash || '|' || ip_hash
         FROM survey_responses
        WHERE tenant_id = ${sqlValue(tenantId)}
          AND invitation_id = ${sqlValue(survey.invitationId)}`,
    )
    if (!/^hmac-sha256:v1:[a-f0-9]{64}\|hmac-sha256:v1:[a-f0-9]{64}$/.test(fingerprints)) {
      throw new Error(`public survey fingerprints are not keyed HMAC pseudonyms: ${fingerprints}`)
    }
    return fingerprints
  } finally {
    await context.close()
  }
}

async function assertPublicSurveyMobileRender(browserInstance, baseURL, survey) {
  const context = await browserInstance.newContext({
    bypassCSP: true,
    viewport: { width: 390, height: 844 },
  })
  const page = await context.newPage()
  try {
    const response = await page.goto(`${baseURL}/surveys/${survey.token}?score=5`, {
      waitUntil: 'domcontentloaded',
    })
    assertPublicSurveyHeaders(response)
    await expect(page.getByRole('radio', { name: 'Score 5', exact: true })).toBeChecked()
    await expect(page.getByRole('button', { name: 'Submit feedback', exact: true })).toBeVisible()
    await assertNoDocumentOverflow(page, 'public survey mobile')
    await expectNoAxeViolations(page)
  } finally {
    await context.close()
  }
}

async function runNPSCampaignSmoke(browserInstance, baseURL, dsn, tenantId, nps, provider) {
  const context = await browserInstance.newContext({ viewport: { width: 1440, height: 1200 } })
  const page = await context.newPage()

  try {
    await loginConsole(page, baseURL)
    await configureNPSSender(page, baseURL, provider.url)

    await page.goto(`${baseURL}/console/integrations/surveys`, { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('heading', { name: '满意度调查', exact: true })).toBeVisible()
    await page.getByTestId('survey-name').fill(nps.campaignName)
    await page.getByTestId('survey-type').click()
    await page.getByRole('option', { name: 'NPS', exact: true }).click()

    const cohortSelect = page.getByTestId('survey-nps-cohort')
    const ownerSelect = page.getByTestId('survey-nps-owner')
    await expect(page.getByTestId('survey-nps-contact-cooldown')).toHaveValue('90')
    await expect(page.getByTestId('survey-nps-minimum-completed-responses')).toHaveValue('30')
    await expect(page.getByTestId('survey-nps-minimum-response-rate')).toHaveValue('10')
    await expect(cohortSelect).toBeEnabled()
    await expect(ownerSelect).toBeEnabled()
    await cohortSelect.click()
    await page.getByRole('option', { name: nps.cohortName, exact: true }).click()
    await ownerSelect.click()
    await page.getByRole('option', { name: nps.ownerEmail, exact: true }).click()
    await page.getByTestId('survey-nps-collection-days').fill('7')
    await page.getByTestId('survey-nps-recipient-cap').fill('29')
    await expect(page.getByTestId('survey-create')).toBeDisabled()
    await expect(page.getByTestId('survey-nps-measurement-validation')).toBeVisible()
    await page.getByTestId('survey-nps-recipient-cap').fill('30')
    await expect(page.getByTestId('survey-create')).toBeEnabled()
    await page.getByTestId('survey-create').click()

    const campaign = page.getByRole('button', { name: nps.campaignName, exact: true })
    await expect(campaign).toHaveCount(1)
    await expect(campaign.locator('..')).toContainText('计划运行')
    await campaign.click()
    const preflight = page.getByTestId('nps-launch-preflight')
    await expect(preflight).toContainText('投递就绪')
    await expect(preflight).toContainText('单次上限 30')
    await expect(page.getByTestId('nps-preflight-measurement-warning')).toContainText('1')
    await expect(page.getByTestId('nps-preflight-measurement-warning')).toContainText('30')
    await expect(page.getByTestId('nps-preflight-exclusion-contact_missing')).toContainText(
      '联系人缺失 · 1',
    )
    await expect(page.getByTestId('nps-schedule-run')).toBeEnabled()

    // Exercise the cancellable boundary before the worker can materialize a
    // ledger. The immediate run below then proves cancellation releases the
    // campaign's single-open-run guard without leaving invitations behind.
    const scheduledAt = dateTimeLocalOneHourFromNow()
    const scheduleAt = page.getByTestId('nps-schedule-at')
    await scheduleAt.fill(scheduledAt)
    await expect(scheduleAt).toHaveAttribute('aria-describedby', 'nps-schedule-time-zone')
    const browserTimeZone = await page.evaluate(
      () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
    )
    await expect(page.getByTestId('nps-schedule-time-zone')).toContainText(
      `当前浏览器时区（${browserTimeZone}）`,
    )
    const scheduledAtUTC = await scheduleAt.evaluate((input) => new Date(input.value).toISOString())
    const scheduledAtYear = await scheduleAt.evaluate((input) =>
      String(new Date(input.value).getFullYear()),
    )
    await expect(page.getByTestId('nps-schedule-utc-preview')).toContainText(scheduledAtUTC)
    await page.getByTestId('nps-schedule-run').click()
    await expect(page.getByText('已安排 NPS 运行。', { exact: true })).toBeVisible()
    await expect(page.getByTestId('nps-campaign-runs')).toContainText(scheduledAtYear)
    const cancelRun = page.locator('[data-testid^="nps-cancel-run-"]')
    await expect(cancelRun).toHaveCount(1)
    await cancelRun.click()
    await expect(page.getByTestId('nps-cancel-run-confirm')).toBeVisible()
    await page.getByTestId('nps-cancel-run-confirm').click()
    await expect(page.getByText('已取消 NPS 运行。', { exact: true })).toBeVisible()
    await expect(page.getByTestId('nps-campaign-runs')).toContainText('已取消')
    await verifyNPSCancellation(dsn, tenantId, nps)

    await page.locator('#nps-schedule-at').fill('')
    await expect(page.getByTestId('nps-schedule-utc-preview')).toHaveCount(0)
    await expect(page.getByTestId('nps-schedule-run')).toBeEnabled()
    await page.getByTestId('nps-schedule-run').click()
    await expect(page.getByTestId('nps-campaign-runs')).toContainText('第 2 次运行')

    const invitation = await waitForProviderEmail(
      provider,
      (message) =>
        message.payload.event_type === 'survey.invitation' &&
        message.payload.to_email === nps.recipientEmail &&
        message.payload.metadata?.survey?.survey_type === 'nps',
      'NPS invitation',
    )
    const publicURL = invitation.payload.metadata?.survey?.public_url
    if (typeof publicURL !== 'string' || !publicURL.startsWith(`${baseURL}/surveys/`)) {
      throw new Error('NPS invitation did not include a valid hosted survey URL')
    }

    await runNPSPublicSurveySmoke(browserInstance, publicURL, nps)
    await waitForProviderEmail(
      provider,
      (message) =>
        message.payload.event_type === 'survey.recovery_opened' &&
        message.payload.to_email === nps.ownerEmail,
      'NPS detractor recovery',
    )
    const { feedbackID, responseID } = await verifyNPSPersistence(dsn, tenantId, nps)
    await verifyNPSMaterializationMetric(baseURL, tenantId)

    // Close the loop through the same Console action an owner uses. The later
    // assertions prove both the durable operational facts and their run-scoped
    // measurement evidence.
    await page.reload({ waitUntil: 'domcontentloaded' })
    const activeCampaign = page.getByRole('button', { name: nps.campaignName, exact: true })
    await expect(activeCampaign).toHaveCount(1)
    await activeCampaign.click()
    const recovery = await resolveNPSRecovery(page, responseID)
    await verifyNPSRecoveryResolution(dsn, tenantId, responseID, recovery)

    await expireNPSCampaignRun(dsn, tenantId, nps, 2)
    await waitForNPSRunStatus(dsn, tenantId, nps, 2, 'closed')

    await page.reload({ waitUntil: 'domcontentloaded' })
    const refreshedCampaign = page.getByRole('button', { name: nps.campaignName, exact: true })
    await expect(refreshedCampaign).toHaveCount(1)
    await refreshedCampaign.click()
    const trend = page.getByTestId('nps-run-measurement-trend')
    await expect(trend).toContainText('NPS -100')
    await expect(trend).toContainText('1 / 1 已提交')
    await expect(trend).toContainText('页面访问率 100% · 访问后完成率 100%')
    const scoreDistribution = page.getByRole('list', { name: '分数分布', exact: true })
    await expect(scoreDistribution).toBeVisible()
    await expect(scoreDistribution.locator('li')).toHaveCount(11)
    await expect(scoreDistribution.locator('li').first()).toContainText('0')
    await expect(scoreDistribution.locator('li').first()).toContainText('1')
    await expect(scoreDistribution.locator('li').last()).toContainText('10')
    await expect(scoreDistribution.locator('li').last()).toContainText('0')
    const measurementEvidence = page.getByTestId('survey-analytics-nps-measurement-evidence')
    await expect(measurementEvidence).toContainText('固定规则')
    await expect(measurementEvidence).toContainText('收集窗口7 天')
    await expect(measurementEvidence).toContainText('单次收件人上限30')
    await expect(measurementEvidence).toContainText('联系间隔90 天')
    await expect(measurementEvidence).toContainText('最少已提交回复')
    await expect(measurementEvidence).toContainText('最小收件人回复率')
    await expect(measurementEvidence).toContainText('实际邀请 / 规划目标')
    await expect(page.getByTestId('nps-analytics-sample-plan-shortfall')).toContainText(
      '低于规划目标',
    )
    const evidenceDownloadPromise = page.waitForEvent('download')
    await page.getByTestId('nps-export-evidence').click()
    const evidenceDownload = await evidenceDownloadPromise
    const evidencePath = await evidenceDownload.path()
    if (!evidencePath) throw new Error('NPS evidence export did not produce a file')
    const evidenceBytes = await readFile(evidencePath)
    const evidenceCSV = evidenceBytes.toString('utf8')
    if (
      !evidenceCSV.includes('report_version') ||
      !evidenceCSV.includes('score_10_count') ||
      evidenceCSV.includes('email') ||
      evidenceCSV.includes(nps.comment)
    ) {
      throw new Error('NPS evidence export is not aggregate-only')
    }
    const repeatedEvidenceDownloadPromise = page.waitForEvent('download')
    await page.getByTestId('nps-export-evidence').click()
    const repeatedEvidenceDownload = await repeatedEvidenceDownloadPromise
    if (!(await repeatedEvidenceDownload.path())) {
      throw new Error('repeated NPS evidence export did not produce a file')
    }
    await page.reload({ waitUntil: 'domcontentloaded' })
    const historyCampaign = page.getByRole('button', { name: nps.campaignName, exact: true })
    await expect(historyCampaign).toHaveCount(1)
    await historyCampaign.click()
    const history = page.getByTestId('nps-evidence-export-history')
    await expect(history).toBeVisible()
    await expect(history).toContainText('保留至')
    const historyLinks = history.locator('a[download]')
    await expect(historyLinks).toHaveCount(2)
    const historyHref = await historyLinks.first().getAttribute('href')
    if (!historyHref) throw new Error('NPS evidence export history link is missing its URL')
    const historyURL = new URL(historyHref, baseURL).href
    const historyHTTP = await page.evaluate(async (url) => {
      const response = await fetch(url, { credentials: 'include' })
      const body = Array.from(new Uint8Array(await response.arrayBuffer()))
      return {
        status: response.status,
        digest: response.headers.get('digest'),
        etag: response.headers.get('etag'),
        body,
      }
    }, historyURL)
    if (historyHTTP.status !== 200) {
      throw new Error(`NPS evidence history download failed with status ${historyHTTP.status}`)
    }
    const historyDownloadPromise = page.waitForEvent('download')
    await historyLinks.first().click()
    const historyDownload = await historyDownloadPromise
    const historyPath = await historyDownload.path()
    if (!historyPath) throw new Error('NPS evidence history did not produce a file')
    const historyBytes = await readFile(historyPath)
    const historyResponseBody = Buffer.from(historyHTTP.body)
    if (Buffer.compare(historyResponseBody, historyBytes) !== 0) {
      throw new Error('NPS evidence history response and downloaded bytes differ')
    }
    const historyDigest = `sha-256=${createHash('sha256').update(historyBytes).digest('base64')}`
    if (historyHTTP.digest !== historyDigest || !historyHTTP.etag) {
      throw new Error(`NPS evidence history integrity headers mismatch: ${historyHTTP.digest}`)
    }
    const qualification = page.locator('[data-testid^="nps-analytics-measurement-qualification-"]')
    await expect(qualification).toHaveCount(1)
    await expect(qualification).toContainText('方向性结果')
    await expect(qualification).toContainText('1 / 30')
    await expect(qualification).toContainText('100% / 10%')
    const recoveryOutcomes = page.getByTestId('survey-analytics-nps-recovery-outcomes')
    await expect(recoveryOutcomes).toBeVisible()
    await expectNPSRecoveryValue(recoveryOutcomes, '明确解决', '1 / 1')
    await expectNPSRecoveryValue(recoveryOutcomes, '已联系客户', '1 / 1')
    await expectNPSRecoveryValue(recoveryOutcomes, '已记录根因', '1 / 1')
    await expectNPSRecoveryValue(recoveryOutcomes, '已记录行动', '1 / 1')
    const recoveryTimeliness = page.getByTestId('survey-analytics-nps-recovery-timeliness')
    await expect(recoveryTimeliness).toBeVisible()
    await expectNPSRecoveryValue(recoveryTimeliness, '首次联系按时', '1 / 1')
    await expectNPSRecoveryValue(recoveryTimeliness, '首次终态按时', '1 / 1')
    await expect(page.getByTestId('nps-campaign-runs')).toContainText('贬损者')
    await assertNPSConsoleMobileRender(browserInstance, baseURL, nps)
    const followUpConsent = page.getByText('客户允许就此反馈跟进', { exact: true })
    await expect(followUpConsent).toHaveCount(1)
    await expect(followUpConsent).toContainText('客户允许就此反馈跟进')
    const feedbackLink = page.getByRole('link', { name: '查看反馈信号', exact: true })
    await expect(feedbackLink).toHaveAttribute('href', `/console/feedback?ids=${feedbackID}`)
    const triageResponse = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === '/fb/v1/console/feedback/triage-command-center',
    )
    await feedbackLink.click()
    const triage = await triageResponse
    if (!triage.ok()) {
      throw new Error(`feedback triage command center returned ${triage.status()}`)
    }
    await expect(page).toHaveURL(`${baseURL}/console/feedback?ids=${feedbackID}`)
    const feedbackCard = page.getByRole('button', { name: new RegExp(`新反馈 #${feedbackID}`) })
    await expect(feedbackCard).toHaveCount(1)
    await expect(feedbackCard).toContainText(nps.comment)

    // NPS feedback stays an owner-reviewed input. Exercise the exact Console
    // promotion journey and then prove the request, evidence link, and audit
    // event all committed together.
    await page.getByLabel(`选择 #${feedbackID}`, { exact: true }).click()
    await page.getByRole('button', { name: '从反馈提升', exact: true }).click()
    await expect(page).toHaveURL(
      new RegExp(`/console/feedback/customer-requests\\?promote_feedback_ids=%22${feedbackID}%22$`),
    )
    const promotionDialog = page.getByRole('dialog', { name: '从反馈提升为客户需求' })
    await expect(promotionDialog).toBeVisible()
    await expect(promotionDialog.locator('#customer-request-feedback-ids')).toHaveValue(feedbackID)
    await promotionDialog.locator('#customer-request-title').fill(nps.promotedRequestTitle)
    await promotionDialog
      .locator('#customer-request-description')
      .fill(nps.promotedRequestDescription)
    await promotionDialog.getByRole('button', { name: '提升', exact: true }).click()
    await expect(promotionDialog).toHaveCount(0)
    await expect(page).toHaveURL(`${baseURL}/console/feedback/customer-requests`)
    await verifyNPSFeedbackPromotion(dsn, tenantId, nps, feedbackID)
    await expect(page.getByText(nps.promotedRequestTitle, { exact: true }).first()).toBeVisible()
  } finally {
    await context.close()
  }
}

async function loginConsole(page, baseURL) {
  await page.goto(`${baseURL}/console/login`, { waitUntil: 'domcontentloaded' })
  await expect(page.getByRole('heading', { name: 'Attune Console', exact: true })).toBeVisible()
  await page.getByLabel('邮箱', { exact: true }).fill(consoleAdmin.email)
  await page.getByLabel('密码', { exact: true }).fill(consoleAdmin.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/console\/control-tower(?:\?.*)?$/)
}

async function configureNPSSender(page, baseURL, providerURL) {
  await page.goto(`${baseURL}/console/integrations/request-notifications`, {
    waitUntil: 'domcontentloaded',
  })
  await expect(page.getByTestId('rn-sender-empty')).toBeVisible()
  await page.getByTestId('rn-sender-from-name').fill('Attune NPS browser smoke')
  await page.getByTestId('rn-sender-from-email').fill('nps-browser-smoke@example.test')
  await page.getByTestId('rn-sender-reply-to').fill('nps-browser-replies@example.test')
  await page.getByTestId('rn-sender-provider').fill('nps-browser-smoke')
  await page.getByTestId('rn-sender-provider-url').fill(providerURL)
  await page.getByTestId('rn-sender-save').click()
  await expect(page.getByText('发件人已保存', { exact: true })).toBeVisible()
  await expect(page.getByTestId('rn-sender-verify')).toBeEnabled()
  await page.getByTestId('rn-sender-verify').click()
  await expect(page.getByText('发件人已验证', { exact: true })).toBeVisible()
  await expect(page.getByText('启用', { exact: true })).toBeVisible()
}

async function runNPSPublicSurveySmoke(browserInstance, publicURL, nps) {
  const context = await browserInstance.newContext({
    bypassCSP: true,
    viewport: { width: 1365, height: 768 },
  })
  const page = await context.newPage()

  try {
    const response = await page.goto(publicURL, { waitUntil: 'domcontentloaded' })
    assertPublicSurveyHeaders(response)
    await expect(page).toHaveTitle('产品反馈 | Attune 调查')
    await expect(page.getByRole('heading', { name: '产品反馈', exact: true })).toBeVisible()
    await expect(page.getByText('您向同事推荐我们的可能性有多大？', { exact: true })).toBeVisible()
    await expect(page.getByLabel('您给出这个评分的主要原因是什么？', { exact: true })).toBeVisible()
    const followUpConsent = page.getByLabel('可以就这条反馈联系我', { exact: true })
    await expect(followUpConsent).not.toBeChecked()
    await expect(page.getByRole('radio', { name: '评分 0', exact: true })).not.toBeChecked()
    await assertNoDocumentOverflow(page, 'NPS public survey desktop')
    await expectNoAxeViolations(page)

    await assertPublicNPSSurveyMobileRender(browserInstance, publicURL)

    await page.getByRole('radio', { name: '评分 0', exact: true }).click()
    await expect(page.getByRole('radio', { name: '评分 0', exact: true })).toBeChecked()
    await followUpConsent.check()
    await expect(followUpConsent).toBeChecked()
    await page.locator('textarea').fill(nps.comment)
    await page.getByRole('button', { name: '提交反馈', exact: true }).click()
    const status = page.getByRole('status')
    await expect(status).toContainText('感谢您的反馈。')
    await expect(status).toContainText('您的回复已标记为跟进。')
    await expect(page.getByRole('button', { name: '提交反馈', exact: true })).toHaveCount(0)
    await assertNoDocumentOverflow(page, 'NPS public survey submitted')
    await expectNoAxeViolations(page)

    await page.goto(publicURL, { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('status')).toHaveText('此调查已提交。')
  } finally {
    await context.close()
  }
}

async function assertPublicNPSSurveyMobileRender(browserInstance, publicURL) {
  const context = await browserInstance.newContext({
    bypassCSP: true,
    viewport: { width: 390, height: 844 },
  })
  const page = await context.newPage()
  const scoredURL = new URL(publicURL)
  scoredURL.searchParams.set('score', '10')
  try {
    const response = await page.goto(scoredURL.toString(), { waitUntil: 'domcontentloaded' })
    assertPublicSurveyHeaders(response)
    await expect(page.getByRole('radio', { name: '评分 10', exact: true })).toBeChecked()
    await expect(page.getByRole('button', { name: '提交反馈', exact: true })).toBeVisible()
    await assertNoDocumentOverflow(page, 'NPS public survey mobile')
    await expectNoAxeViolations(page)
  } finally {
    await context.close()
  }
}

async function assertNPSConsoleMobileRender(browserInstance, baseURL, nps) {
  const context = await browserInstance.newContext({ viewport: { width: 390, height: 844 } })
  const page = await context.newPage()

  try {
    await loginConsole(page, baseURL)
    await page.goto(`${baseURL}/console/integrations/surveys`, { waitUntil: 'domcontentloaded' })

    const campaign = page.getByRole('button', { name: nps.campaignName, exact: true })
    await expect(campaign).toHaveCount(1)
    await campaign.click()
    await expect(page.getByTestId('nps-run-measurement-trend')).toContainText('NPS -100')
    await expect(
      page.getByRole('list', { name: '分数分布', exact: true }).locator('li'),
    ).toHaveCount(11)
    await expect(page.getByTestId('nps-schedule-time-zone')).toContainText('当前浏览器时区')
    await assertNoDocumentOverflow(page, 'NPS Console mobile')

    const layout = await page.locator('h1').evaluate((heading) => {
      const content = heading.parentElement
      const hero = heading.closest('section')
      const metrics = Array.from(hero?.children ?? []).find((element) =>
        element.classList.contains('border-t'),
      )
      if (!content || !metrics) return null
      const contentBounds = content.getBoundingClientRect()
      const metricsBounds = metrics.getBoundingClientRect()
      return {
        contentHeight: Math.round(contentBounds.height),
        metricsGap: Math.round(metricsBounds.top - contentBounds.bottom),
      }
    })
    if (!layout || layout.contentHeight > 160 || layout.metricsGap > 36) {
      throw new Error(`NPS Console mobile hero layout = ${JSON.stringify(layout)}`)
    }
  } finally {
    await context.close()
  }
}

async function verifyNPSPersistence(dsn, tenantId, nps) {
  const persisted = await psqlScalar(
    dsn,
    `SELECT sr.score::text || '|' || sr.nps_bucket || '|' || sr.follow_up_consent::text || '|' || sr.contact_id::text || '|' ||
            lsr.owner_member_id::text || '|' || uf.source || '|' || uf.type || '|' ||
            uf.enrichment_status || '|' || uf.content || '|' || uf.subject_key
       FROM survey_responses sr
       JOIN survey_response_feedback_links link
         ON link.tenant_id = sr.tenant_id AND link.response_id = sr.id
       JOIN user_feedback uf
         ON uf.tenant_id = link.tenant_id AND uf.id = link.feedback_id
       JOIN survey_low_score_reviews lsr
         ON lsr.tenant_id = sr.tenant_id AND lsr.response_id = sr.id
      WHERE sr.tenant_id = ${sqlValue(tenantId)}
        AND sr.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )`,
  )
  const expected = [
    '0',
    'detractor',
    'true',
    nps.contactId,
    nps.ownerMemberId,
    'survey',
    'nps',
    'pending',
    nps.comment,
    nps.subjectKey,
  ].join('|')
  if (persisted !== expected) {
    throw new Error(`NPS response/feedback persistence mismatch: ${persisted}`)
  }

  const fingerprints = await psqlScalar(
    dsn,
    `SELECT sr.user_agent_hash || '|' || sr.ip_hash
       FROM survey_responses sr
      WHERE sr.tenant_id = ${sqlValue(tenantId)}
        AND sr.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )`,
  )
  if (!/^hmac-sha256:v1:[a-f0-9]{64}\|hmac-sha256:v1:[a-f0-9]{64}$/.test(fingerprints)) {
    throw new Error(`NPS response fingerprints are not keyed HMAC pseudonyms: ${fingerprints}`)
  }

  const responseID = await psqlScalar(
    dsn,
    `SELECT sr.id::text
       FROM survey_responses sr
      WHERE sr.tenant_id = ${sqlValue(tenantId)}
        AND sr.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )`,
  )
  if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(responseID)) {
    throw new Error(`NPS response ID missing: ${responseID}`)
  }

  const recovery = await psqlScalar(
    dsn,
    `SELECT notification.status || '|' || notification.reason || '|' || notification.owner_member_id::text || '|' ||
            (notification.payload->'survey'->>'follow_up_consent')
       FROM survey_recovery_notifications notification
       JOIN survey_responses response
         ON response.tenant_id = notification.tenant_id AND response.id = notification.response_id
      WHERE notification.tenant_id = ${sqlValue(tenantId)}
        AND response.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )`,
  )
  const expectedRecovery = `delivered|nps_detractor_response|${nps.ownerMemberId}|true`
  if (recovery !== expectedRecovery) {
    throw new Error(`NPS recovery notification mismatch: ${recovery}`)
  }

  const feedbackID = await psqlScalar(
    dsn,
    `SELECT link.feedback_id::text
       FROM survey_response_feedback_links link
       JOIN survey_responses response
         ON response.tenant_id = link.tenant_id AND response.id = link.response_id
      WHERE link.tenant_id = ${sqlValue(tenantId)}
        AND response.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )`,
  )
  if (!/^\d+$/.test(feedbackID)) {
    throw new Error(`NPS feedback bridge ID missing: ${feedbackID}`)
  }
  return { feedbackID, responseID }
}

async function resolveNPSRecovery(page, responseID) {
  const rootCause = 'Onboarding workflow gap'
  const actionTaken = 'Recovery call completed and onboarding fix scheduled.'
  const review = page.getByTestId(`survey-low-score-${responseID}`)
  await expect(review).toBeVisible()
  await review.getByTestId(`survey-low-score-status-${responseID}`).click()
  await page.getByRole('option', { name: '已解决', exact: true }).click()
  await review.getByTestId(`survey-low-score-root-cause-${responseID}`).fill(rootCause)
  await review.getByTestId(`survey-low-score-action-${responseID}`).fill(actionTaken)
  await review.getByLabel('已联系客户', { exact: true }).click()
  await review.getByTestId(`survey-low-score-save-${responseID}`).click()
  await expect(page.getByText('低分跟进已更新', { exact: true })).toBeVisible()
  return { rootCause, actionTaken }
}

async function verifyNPSRecoveryResolution(dsn, tenantId, responseID, recovery) {
  const persisted = await psqlScalar(
    dsn,
    `SELECT status || '|' || customer_contacted::text || '|' ||
            (customer_contacted_at IS NOT NULL)::text || '|' ||
            (first_terminal_at IS NOT NULL)::text || '|' ||
            (reviewed_at IS NOT NULL)::text || '|' ||
            root_cause || '|' || action_taken
       FROM survey_low_score_reviews
      WHERE tenant_id = ${sqlValue(tenantId)}
        AND response_id = ${sqlValue(responseID)}`,
  )
  const expected = [
    'resolved',
    'true',
    'true',
    'true',
    'true',
    recovery.rootCause,
    recovery.actionTaken,
  ].join('|')
  if (persisted !== expected) {
    throw new Error(`NPS recovery resolution mismatch: ${persisted}`)
  }
}

async function verifyNPSFeedbackPromotion(dsn, tenantId, nps, feedbackID) {
  const persisted = await psqlScalar(
    dsn,
    `SELECT request.title || '|' || request.description || '|' ||
            (SELECT COUNT(*)::text
               FROM customer_request_feedback_links link
              WHERE link.tenant_id = request.tenant_id
                AND link.request_id = request.id
                AND link.feedback_id = ${sqlValue(feedbackID)}) || '|' ||
            (SELECT COUNT(*)::text
               FROM audit_log audit
              WHERE audit.tenant_id = request.tenant_id
                AND audit.action = 'customer_request.promote_feedback'
                AND audit.target_type = 'customer_request'
                AND audit.target_id = request.id::text
                AND audit.after_json @> jsonb_build_object(
                  'feedback_ids', jsonb_build_array(${sqlValue(feedbackID)}::bigint),
                  'feedback_count', 1
                ))
       FROM customer_requests request
      WHERE request.tenant_id = ${sqlValue(tenantId)}
        AND request.title = ${sqlValue(nps.promotedRequestTitle)}
      ORDER BY request.created_at DESC
      LIMIT 1`,
  )
  const expected = `${nps.promotedRequestTitle}|${nps.promotedRequestDescription}|1|1`
  if (persisted !== expected) {
    throw new Error(`NPS feedback promotion persistence mismatch: ${persisted}`)
  }
}

async function expectNPSRecoveryValue(container, label, value) {
  const term = container.locator('dt', { hasText: label })
  await expect(term).toHaveCount(1)
  await expect(term.locator('xpath=following-sibling::dd')).toHaveText(value)
}

async function verifyNPSMaterializationMetric(baseURL, tenantId) {
  const response = await fetch(`${baseURL}/metrics`)
  if (!response.ok) {
    throw new Error(`NPS materialization metric scrape failed with HTTP ${response.status}`)
  }
  const exposition = await response.text()
  const line = exposition
    .split('\n')
    .find(
      (candidate) =>
        candidate.startsWith('attune_survey_nps_run_materialization_total{') &&
        candidate.includes(`tenant="${tenantId}"`) &&
        candidate.includes('result="materialized"') &&
        candidate.includes('reason="ok"'),
    )
  const value = Number(line?.slice((line?.lastIndexOf(' ') ?? -1) + 1))
  if (!Number.isFinite(value) || value < 1) {
    throw new Error(`NPS materialization metric missing or invalid: ${line ?? 'not found'}`)
  }
}

async function expireNPSCampaignRun(dsn, tenantId, nps, sequence) {
  const runID = await psqlScalar(
    dsn,
    `UPDATE survey_campaign_runs run
        SET closes_at = NOW() - INTERVAL '1 second'
      WHERE run.tenant_id = ${sqlValue(tenantId)}
        AND run.sequence = ${sqlValue(sequence)}
        AND run.status = 'collecting'
        AND run.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )
      RETURNING run.id`,
  )
  if (!runID)
    throw new Error(`NPS run ${sequence} was not collecting when advancing its test clock`)
}

async function waitForNPSRunStatus(dsn, tenantId, nps, sequence, expectedStatus) {
  for (let attempt = 1; attempt <= 40; attempt++) {
    const status = await psqlScalar(
      dsn,
      `SELECT run.status
         FROM survey_campaign_runs run
        WHERE run.tenant_id = ${sqlValue(tenantId)}
          AND run.sequence = ${sqlValue(sequence)}
          AND run.campaign_id = (
            SELECT id FROM survey_campaigns
             WHERE tenant_id = ${sqlValue(tenantId)}
               AND name = ${sqlValue(nps.campaignName)}
          )`,
    )
    if (status === expectedStatus) return
    await sleep(250)
  }
  throw new Error(`NPS run ${sequence} did not transition to ${expectedStatus}`)
}

async function verifyNPSCancellation(dsn, tenantId, nps) {
  const result = await psqlScalar(
    dsn,
    `SELECT run.status || '|' || run.invitation_count::text || '|' ||
            (SELECT COUNT(*)::text
               FROM survey_invitations invitation
              WHERE invitation.tenant_id = run.tenant_id AND invitation.run_id = run.id) || '|' ||
            (SELECT COUNT(*)::text
               FROM audit_log audit
              WHERE audit.tenant_id = run.tenant_id
                AND audit.action = 'survey.nps_run_cancel'
                AND audit.target_id = run.id::text)
       FROM survey_campaign_runs run
      WHERE run.tenant_id = ${sqlValue(tenantId)}
        AND run.campaign_id = (
          SELECT id FROM survey_campaigns
           WHERE tenant_id = ${sqlValue(tenantId)}
             AND name = ${sqlValue(nps.campaignName)}
        )
        AND run.sequence = 1`,
  )
  if (result !== 'cancelled|0|0|1') {
    throw new Error(`NPS cancellation persistence mismatch: ${result}`)
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
    await verifyPortalSubmissionForm(portalPopup)
  } finally {
    await context.close()
  }
}

async function verifyPortalSubmissionForm(page) {
  const acknowledgement = 'Thanks. We will review your submission.'
  await expect(page.getByText(acknowledgement, { exact: true })).toHaveCount(0)
  await expect(page.locator('#portal-status')).toHaveText('')

  const title = `Browser smoke portal submission ${Date.now()}`
  await page.getByPlaceholder('Summarize the problem or idea', { exact: true }).fill(title)
  await page
    .getByPlaceholder('Tell us what happened, what you expected, and any helpful context.', {
      exact: true,
    })
    .fill('Browser smoke verifies the acknowledgement is only rendered after submit.')
  await page.getByRole('button', { name: 'Submit feedback', exact: true }).click()
  await expect(page.locator('#portal-status')).toHaveText(acknowledgement)
  await expect(page.getByText(acknowledgement, { exact: true })).toHaveCount(1)
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

function assertPublicSurveyHeaders(response) {
  if (!response?.ok()) {
    throw new Error(`public survey response status = ${response?.status() ?? 'missing'}`)
  }
  const headers = response.headers()
  if (headers['cache-control'] !== 'no-store') {
    throw new Error(`public survey Cache-Control = ${headers['cache-control']}`)
  }
  if (headers['x-robots-tag'] !== 'noindex, nofollow') {
    throw new Error(`public survey X-Robots-Tag = ${headers['x-robots-tag']}`)
  }
  if (headers['referrer-policy'] !== 'no-referrer') {
    throw new Error(`public survey Referrer-Policy = ${headers['referrer-policy']}`)
  }
  if (headers['x-frame-options'] !== 'DENY') {
    throw new Error(`public survey X-Frame-Options = ${headers['x-frame-options']}`)
  }
  if (!headers['content-security-policy']?.includes("frame-ancestors 'none'")) {
    throw new Error(`public survey CSP = ${headers['content-security-policy']}`)
  }
}

async function assertNoDocumentOverflow(page, label) {
  const metrics = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
    innerWidth: window.innerWidth,
  }))
  if (
    metrics.scrollWidth > metrics.innerWidth + 1 ||
    metrics.scrollWidth > metrics.clientWidth + 1
  ) {
    throw new Error(`${label} overflow: ${JSON.stringify(metrics)}`)
  }
}

async function expectNoAxeViolations(page) {
  await page.addScriptTag({ content: axeSource })
  const violations = await page.evaluate(async () => {
    const results = await window.axe.run(document, {
      resultTypes: ['violations'],
      rules: {
        region: { enabled: true },
      },
    })
    return results.violations
      .filter((violation) => violation.impact !== 'minor')
      .map((violation) => {
        const node = violation.nodes[0]
        return {
          id: violation.id,
          impact: violation.impact,
          help: violation.help,
          target: node?.target.join(' '),
          summary: node?.failureSummary,
        }
      })
  })
  expect(violations).toEqual([])
}

async function startMailProvider() {
  const messages = []
  const server = createServer(async (request, response) => {
    if (request.method !== 'POST' || request.url !== '/delivery') {
      response.statusCode = 404
      response.end()
      return
    }
    let body = ''
    for await (const chunk of request) {
      body += chunk
      if (body.length > 1024 * 1024) {
        response.statusCode = 413
        response.end()
        return
      }
    }
    try {
      messages.push({ payload: JSON.parse(body) })
      response.statusCode = 204
      response.end()
    } catch {
      response.statusCode = 400
      response.end()
    }
  })
  await new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  const address = server.address()
  if (!address || typeof address === 'string') {
    await new Promise((resolve) => server.close(resolve))
    throw new Error('NPS mail provider did not bind a TCP port')
  }
  return {
    messages,
    url: `http://127.0.0.1:${address.port}/delivery`,
    close: () =>
      new Promise((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      ),
  }
}

async function waitForProviderEmail(provider, predicate, label) {
  for (let attempt = 1; attempt <= 160; attempt++) {
    const message = provider.messages.find(predicate)
    if (message) return message
    await sleep(250)
  }
  const received = provider.messages.map((message) => ({
    eventType: message.payload?.event_type,
    toEmail: message.payload?.to_email,
  }))
  throw new Error(`${label} email was not delivered: ${JSON.stringify(received)}`)
}

function sqlValue(value) {
  if (value === null || value === undefined) return 'NULL'
  if (typeof value === 'boolean') return value ? 'TRUE' : 'FALSE'
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : 'NULL'
  if (typeof value === 'object' && value.__rawSql) return value.__rawSql
  return `'${String(value).replace(/'/g, "''")}'`
}

function dateTimeLocalOneHourFromNow() {
  const value = new Date(Date.now() + 60 * 60 * 1000)
  const part = (number) => String(number).padStart(2, '0')
  return `${value.getFullYear()}-${part(value.getMonth() + 1)}-${part(value.getDate())}T${part(value.getHours())}:${part(value.getMinutes())}`
}

function sqlJsonb(value) {
  return `${sqlValue(JSON.stringify(value ?? {}))}::jsonb`
}

function surveyTokenHash(token) {
  return createHash('sha256').update(String(token).trim()).digest('hex')
}

function emailHashForSmoke(email) {
  return createHash('sha256').update(String(email).trim().toLowerCase()).digest('hex')
}

function subjectKeyHash(tenantId, subjectKey) {
  return createHash('sha256')
    .update(`${String(tenantId).trim()}\0${String(subjectKey).trim()}`)
    .digest('hex')
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
