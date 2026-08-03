import { describe, expect, it } from 'vitest'
import {
  ModerationState,
  type ModerationSubject,
  PortalSubmissionFieldKind,
  PublicAccessMode,
  PublicIdentityMode,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { buildPublicPrivacyPreflight } from './public-privacy-preflight'

describe('buildPublicPrivacyPreflight', () => {
  it('surfaces public access, identity, portal field, moderation, and recovery risks', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [
        moderationSubject('pending', ModerationState.MODERATION_STATE_PENDING),
        moderationSubject('approved', ModerationState.MODERATION_STATE_APPROVED),
      ],
      policy: publicPolicy(),
    })

    expect(preflight.fingerprint).toBe(
      'public / 3 public surfaces / 2 moderation subjects / 1 portal fields',
    )
    expect(preflight.summary).toBe('4 privacy preflight checks need attention')
    expect(preflight.totals).toMatchObject({
      blocked: 0,
      needs_data: 0,
      ready: 1,
      total: 5,
      watch: 4,
    })
    expect(preflight.lanes.find((lane) => lane.key === 'access_boundary')).toMatchObject({
      signal: 'public / 3 public surfaces / search off',
      status: 'watch',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'moderation_gate')).toMatchObject({
      signal: '1 pending / 1 approved / request pending / comment pending',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'identity_exposure')).toMatchObject({
      signal: 'identity display_name / submitter on / timestamps visible',
      status: 'watch',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'submission_fields')).toMatchObject({
      signal: '1 fields / 1 required / page URL on',
      status: 'watch',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'review_recovery')).toMatchObject({
      signal: '0 blocked / 0 reasoned / 2 subjects',
      status: 'watch',
    })
  })

  it('blocks automatic public approval and unreasoned blocked decisions', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [moderationSubject('hidden', ModerationState.MODERATION_STATE_HIDDEN)],
      policy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_APPROVED,
        defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
        portalSubmissionForm: {
          acknowledgement: 'Thanks.',
          description: 'Share feedback.',
          fields: [
            {
              key: '',
              kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
              label: '',
              options: [],
              placeholder: '',
              required: false,
            },
          ],
          headline: 'Share feedback',
          showPageUrl: false,
          submitButtonLabel: 'Submit',
        },
      }),
    })

    expect(preflight.summary).toBe('3 privacy preflight checks are blocked')
    expect(preflight.totals).toMatchObject({ blocked: 3, ready: 0, watch: 2 })
    expect(preflight.lanes.find((lane) => lane.key === 'moderation_gate')).toMatchObject({
      signal: '0 pending / 0 approved / request approved / comment approved',
      status: 'blocked',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'submission_fields')).toMatchObject({
      signal: '1 fields / 0 required / page URL off',
      status: 'blocked',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'review_recovery')).toMatchObject({
      signal: '1 blocked / 0 reasoned / 1 subjects',
      status: 'blocked',
    })
  })

  it('keeps missing policy evidence visible for moderation-only operators', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [],
    })

    expect(preflight.fingerprint).toBe(
      'public policy unknown / 0 public surfaces / 0 moderation subjects / 0 portal fields',
    )
    expect(preflight.summary).toBe('4 privacy preflight checks need evidence')
    expect(preflight.totals).toMatchObject({
      blocked: 0,
      needs_data: 4,
      ready: 1,
      total: 5,
      watch: 0,
    })
  })

  it('keeps missing moderation recovery evidence explicit when policy has loaded', () => {
    const preflight = buildPublicPrivacyPreflight({
      policy: publicPolicy({
        hidePublicTimestamps: true,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
      }),
    })

    expect(preflight.summary).toBe('1 privacy preflight checks need evidence')
    expect(preflight.lanes.find((lane) => lane.key === 'review_recovery')).toMatchObject({
      evidence: 'moderation recovery evidence is missing',
      signal: 'review recovery evidence missing',
      status: 'needs_data',
    })
  })

  it('verifies private authenticated policy with reasoned recovery evidence', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [
        moderationSubject(
          'rejected',
          ModerationState.MODERATION_STATE_REJECTED,
          'policy_violation',
        ),
        moderationSubject('hidden', ModerationState.MODERATION_STATE_HIDDEN, 'private_data'),
        moderationSubject('spam', ModerationState.MODERATION_STATE_SPAM, 'spam'),
      ],
      policy: publicPolicy({
        commentsEnabled: false,
        hidePublicTimestamps: true,
        portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED,
        portalSubmissionForm: undefined,
        requestsEnabled: false,
        roadmapEnabled: false,
        searchIndexingEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION,
      }),
    })

    expect(preflight.fingerprint).toBe(
      'authenticated / 0 public surfaces / 3 moderation subjects / 0 portal fields',
    )
    expect(preflight.summary).toBe('public privacy preflight evidence is verified')
    expect(preflight.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(preflight.lanes.find((lane) => lane.key === 'access_boundary')).toMatchObject({
      evidence: 'requests off / comments off / roadmap off / changelog off',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'identity_exposure')).toMatchObject({
      signal: 'identity organization / submitter off / timestamps hidden',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'submission_fields')).toMatchObject({
      evidence: '0 portal fields / 0 required / page URL off / keys none',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'review_recovery')).toMatchObject({
      evidence: '1 hidden / 1 rejected / 1 spam / 3 reasoned blocked',
      signal: '3 blocked / 3 reasoned / 3 subjects',
      status: 'ready',
    })
  })

  it('keeps optional portal fields ready when page URL collection is disabled', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [],
      policy: publicPolicy({
        hidePublicTimestamps: true,
        portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED,
        portalSubmissionForm: {
          acknowledgement: 'Thanks.',
          description: 'Share feedback.',
          fields: [
            {
              key: 'context',
              kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
              label: 'Context',
              options: [],
              placeholder: '',
              required: false,
            },
          ],
          headline: 'Share feedback',
          showPageUrl: false,
          submitButtonLabel: 'Submit',
        },
        requestsEnabled: false,
        roadmapEnabled: false,
        searchIndexingEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
      }),
    })

    expect(preflight.summary).toBe('public privacy preflight evidence is verified')
    expect(preflight.lanes.find((lane) => lane.key === 'submission_fields')).toMatchObject({
      signal: '1 fields / 0 required / page URL off',
      status: 'ready',
    })
  })

  it.each([
    [PublicAccessMode.PUBLIC_ACCESS_MODE_INVITE_ONLY, 'invite_only'],
    [PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED, 'disabled'],
    [PublicAccessMode.PUBLIC_ACCESS_MODE_UNSPECIFIED, 'unknown'],
    [PublicAccessMode.UNRECOGNIZED, 'unknown'],
  ])('labels %s access mode and search risk', (portalAccessMode, label) => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [],
      policy: publicPolicy({
        portalAccessMode,
        searchIndexingEnabled: portalAccessMode !== PublicAccessMode.PUBLIC_ACCESS_MODE_UNSPECIFIED,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS,
      }),
    })

    expect(preflight.fingerprint).toContain(`${label} / 3 public surfaces`)
    expect(preflight.lanes.find((lane) => lane.key === 'access_boundary')?.signal).toContain(
      `${label} / 3 public surfaces`,
    )
  })

  it('keeps unrecognized identity and moderation enum values visible as unknown', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [moderationSubject('unknown', ModerationState.UNRECOGNIZED)],
      policy: publicPolicy({
        defaultCommentState: ModerationState.UNRECOGNIZED,
        defaultRequestState: ModerationState.MODERATION_STATE_SPAM,
        hidePublicTimestamps: true,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.UNRECOGNIZED,
      }),
    })

    expect(preflight.summary).toBe('2 privacy preflight checks need attention')
    expect(preflight.lanes.find((lane) => lane.key === 'moderation_gate')).toMatchObject({
      signal: '0 pending / 0 approved / request spam / comment unknown',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'identity_exposure')).toMatchObject({
      signal: 'identity unknown / submitter off / timestamps hidden',
      status: 'ready',
    })
    expect(preflight.lanes.find((lane) => lane.key === 'review_recovery')).toMatchObject({
      signal: '0 blocked / 0 reasoned / 1 subjects',
      status: 'ready',
    })
  })

  it('labels rejected and hidden moderation defaults without blocking review-only policy', () => {
    const preflight = buildPublicPrivacyPreflight({
      moderationSubjects: [],
      policy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_HIDDEN,
        defaultRequestState: ModerationState.MODERATION_STATE_REJECTED,
        hidePublicTimestamps: true,
        portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED,
        portalSubmissionForm: undefined,
        requestsEnabled: false,
        roadmapEnabled: false,
        searchIndexingEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS,
      }),
    })

    expect(preflight.summary).toBe('public privacy preflight evidence is verified')
    expect(preflight.lanes.find((lane) => lane.key === 'moderation_gate')).toMatchObject({
      signal: '0 pending / 0 approved / request rejected / comment hidden',
      status: 'ready',
    })
  })
})

function publicPolicy(overrides: Partial<PublicVisibilityPolicy> = {}): PublicVisibilityPolicy {
  return {
    changelogEnabled: false,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    commentsEnabled: true,
    createdAt: '2026-07-10T00:00:00Z',
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    hidePublicTimestamps: false,
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
    portalSubmissionForm: {
      acknowledgement: 'Thanks.',
      description: 'Share feedback.',
      fields: [
        {
          key: 'severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
          label: 'Severity',
          options: ['low', 'medium', 'high'],
          placeholder: 'Choose severity',
          required: true,
        },
      ],
      headline: 'Share feedback',
      showPageUrl: true,
      submitButtonLabel: 'Submit',
    },
    requestsEnabled: true,
    roadmapEnabled: true,
    roadmapStatusMapping: [],
    searchIndexingEnabled: false,
    showCommentCount: true,
    showSubmitterDisplay: true,
    showVoteCount: true,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
    tenantId: 'tenant-a',
    updatedAt: '2026-07-10T00:05:00Z',
    updatedBy: 'admin-1',
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
    ...overrides,
  }
}

function moderationSubject(id: string, state: ModerationState, reasonCode = ''): ModerationSubject {
  return {
    createdAt: '2026-07-10T00:01:00Z',
    id,
    reasonCode,
    reasonNote: '',
    reviewedAt: state === ModerationState.MODERATION_STATE_APPROVED ? '2026-07-10T00:04:00Z' : '',
    reviewedBy: state === ModerationState.MODERATION_STATE_APPROVED ? 'admin-1' : '',
    state,
    subjectId: `subject-${id}`,
    submittedByDisplay: 'Ada Lovelace',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    tenantId: 'tenant-a',
    updatedAt: '2026-07-10T00:04:00Z',
  }
}
