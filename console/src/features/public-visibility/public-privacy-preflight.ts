import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicVisibilityPolicy,
} from '@/proto/attune/v1/public_visibility'

export type PublicPrivacyPreflightStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type PublicPrivacyPreflightLaneKey =
  | 'access_boundary'
  | 'moderation_gate'
  | 'identity_exposure'
  | 'submission_fields'
  | 'review_recovery'

export type PublicPrivacyPreflightLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: PublicPrivacyPreflightLaneKey
  owner: string
  signal: string
  status: PublicPrivacyPreflightStatus
  title: string
}

export type PublicPrivacyPreflight = {
  fingerprint: string
  lanes: PublicPrivacyPreflightLane[]
  summary: string
  totals: Record<PublicPrivacyPreflightStatus, number> & {
    total: number
  }
}

export type PublicPrivacyPreflightInput = {
  moderationSubjects?: ModerationSubject[]
  policy?: PublicVisibilityPolicy
}

export function buildPublicPrivacyPreflight(
  input: PublicPrivacyPreflightInput,
): PublicPrivacyPreflight {
  const lanes = [
    accessBoundaryLane(input),
    moderationGateLane(input),
    identityExposureLane(input),
    submissionFieldsLane(input),
    reviewRecoveryLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${accessModeLabel(input.policy)} / ${formatNumber(
      enabledPublicSurfaceCount(input.policy),
    )} public surfaces / ${formatNumber(input.moderationSubjects?.length ?? 0)} moderation subjects / ${formatNumber(
      portalFieldCount(input.policy),
    )} portal fields`,
    lanes,
    summary: publicPrivacySummary(totals),
    totals,
  }
}

function accessBoundaryLane(input: PublicPrivacyPreflightInput): PublicPrivacyPreflightLane {
  const policy = input.policy
  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review access policy',
    evidence: policy
      ? `${surfaceState(policy.requestsEnabled, 'requests')} / ${surfaceState(
          policy.commentsEnabled,
          'comments',
        )} / ${surfaceState(policy.roadmapEnabled, 'roadmap')} / ${surfaceState(
          policy.changelogEnabled,
          'changelog',
        )}`
      : 'public access policy evidence is missing',
    guardrail:
      'Public access must be intentional, search indexing must be explicit, and each public surface must be enabled only after privacy review.',
    key: 'access_boundary',
    owner: 'Security + Portal',
    signal: policy
      ? `${accessModeLabel(policy)} / ${formatNumber(
          enabledPublicSurfaceCount(policy),
        )} public surfaces / search ${onOff(policy.searchIndexingEnabled)}`
      : 'public access boundary evidence missing',
    status: accessBoundaryStatus(policy),
    title: 'Public access boundary',
  }
}

function moderationGateLane(input: PublicPrivacyPreflightInput): PublicPrivacyPreflightLane {
  const policy = input.policy
  const counts = moderationCounts(input.moderationSubjects)
  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review moderation gates',
    evidence: policy
      ? `request default ${moderationStateLabel(
          policy.defaultRequestState,
        )} / comment default ${moderationStateLabel(policy.defaultCommentState)}`
      : 'moderation default policy evidence is missing',
    guardrail:
      'Public requests and comments should enter a pending state before publication unless the surface is disabled.',
    key: 'moderation_gate',
    owner: 'Support + Security',
    signal: policy
      ? `${counts.pending} pending / ${counts.approved} approved / request ${moderationStateLabel(
          policy.defaultRequestState,
        )} / comment ${moderationStateLabel(policy.defaultCommentState)}`
      : 'moderation gate evidence missing',
    status: moderationGateStatus(policy),
    title: 'Moderation gate',
  }
}

function identityExposureLane(input: PublicPrivacyPreflightInput): PublicPrivacyPreflightLane {
  const policy = input.policy
  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review identity exposure',
    evidence: policy
      ? `${identityModeLabel(policy.submitterIdentityMode)} identity / submitter ${onOff(
          policy.showSubmitterDisplay,
        )} / timestamps ${policy.hidePublicTimestamps ? 'hidden' : 'visible'} / votes ${onOff(
          policy.showVoteCount,
        )} / comments ${onOff(policy.showCommentCount)}`
      : 'identity exposure policy evidence is missing',
    guardrail:
      'Names, organizations, timestamps, votes, and comment counts must be intentional public fields, not accidental projections.',
    key: 'identity_exposure',
    owner: 'Security + Product',
    signal: policy
      ? `identity ${identityModeLabel(policy.submitterIdentityMode)} / submitter ${onOff(
          policy.showSubmitterDisplay,
        )} / timestamps ${policy.hidePublicTimestamps ? 'hidden' : 'visible'}`
      : 'identity exposure evidence missing',
    status: identityExposureStatus(policy),
    title: 'Identity exposure',
  }
}

function submissionFieldsLane(input: PublicPrivacyPreflightInput): PublicPrivacyPreflightLane {
  const policy = input.policy
  const fields = policy?.portalSubmissionForm?.fields ?? []
  const required = fields.filter((field) => field.required).length
  const customKeys = fields.map((field) => field.key).filter(Boolean)
  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review portal fields',
    evidence: policy
      ? `${fields.length} portal fields / ${required} required / page URL ${onOff(
          policy.portalSubmissionForm?.showPageUrl ?? false,
        )} / keys ${customKeys.join(', ') || 'none'}`
      : 'portal submission field evidence is missing',
    guardrail:
      'Portal submissions need explicit field keys, required flags, and page-URL collection review before entering feedback intake.',
    key: 'submission_fields',
    owner: 'Portal + Support',
    signal: policy
      ? `${fields.length} fields / ${required} required / page URL ${onOff(
          policy.portalSubmissionForm?.showPageUrl ?? false,
        )}`
      : 'portal field evidence missing',
    status: submissionFieldsStatus(policy),
    title: 'Portal submission fields',
  }
}

function reviewRecoveryLane(input: PublicPrivacyPreflightInput): PublicPrivacyPreflightLane {
  const subjects = input.moderationSubjects
  const counts = moderationCounts(subjects)
  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review recovery queue',
    evidence: subjects
      ? `${counts.hidden} hidden / ${counts.rejected} rejected / ${counts.spam} spam / ${counts.reasonedBlocked} reasoned blocked`
      : 'moderation recovery evidence is missing',
    guardrail:
      'Rejected, hidden, and spam decisions need recoverable state, reason codes, and visible review ownership.',
    key: 'review_recovery',
    owner: 'Support + Compliance',
    signal: subjects
      ? `${counts.blocked} blocked / ${counts.reasonedBlocked} reasoned / ${subjects.length} subjects`
      : 'review recovery evidence missing',
    status: reviewRecoveryStatus(subjects),
    title: 'Review recovery path',
  }
}

function accessBoundaryStatus(
  policy: PublicVisibilityPolicy | undefined,
): PublicPrivacyPreflightStatus {
  if (!policy) return 'needs_data'
  if (policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_UNSPECIFIED) {
    return 'blocked'
  }
  if (policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC) return 'watch'
  if (policy.searchIndexingEnabled) return 'watch'
  return 'ready'
}

function moderationGateStatus(
  policy: PublicVisibilityPolicy | undefined,
): PublicPrivacyPreflightStatus {
  if (!policy) return 'needs_data'
  if (
    policy.defaultRequestState === ModerationState.MODERATION_STATE_APPROVED ||
    policy.defaultCommentState === ModerationState.MODERATION_STATE_APPROVED
  ) {
    return 'blocked'
  }
  return 'ready'
}

function identityExposureStatus(
  policy: PublicVisibilityPolicy | undefined,
): PublicPrivacyPreflightStatus {
  if (!policy) return 'needs_data'
  if (
    policy.submitterIdentityMode === PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME &&
    policy.showSubmitterDisplay &&
    !policy.hidePublicTimestamps
  ) {
    return 'watch'
  }
  if (policy.showVoteCount || policy.showCommentCount) return 'watch'
  return 'ready'
}

function submissionFieldsStatus(
  policy: PublicVisibilityPolicy | undefined,
): PublicPrivacyPreflightStatus {
  if (!policy) return 'needs_data'
  const fields = policy.portalSubmissionForm?.fields ?? []
  const hasMissingKeys = fields.some((field) => !field.key.trim() || !field.label.trim())
  if (hasMissingKeys) return 'blocked'
  if (policy.portalSubmissionForm?.showPageUrl || fields.some((field) => field.required)) {
    return 'watch'
  }
  return 'ready'
}

function reviewRecoveryStatus(
  subjects: ModerationSubject[] | undefined,
): PublicPrivacyPreflightStatus {
  if (!subjects) return 'needs_data'
  const counts = moderationCounts(subjects)
  if (counts.blocked > counts.reasonedBlocked) return 'blocked'
  if (counts.pending > 0) return 'watch'
  return 'ready'
}

function moderationCounts(subjects: ModerationSubject[] | undefined) {
  const counts = {
    approved: 0,
    blocked: 0,
    hidden: 0,
    pending: 0,
    reasonedBlocked: 0,
    rejected: 0,
    spam: 0,
  }
  for (const subject of subjects ?? []) {
    if (subject.state === ModerationState.MODERATION_STATE_PENDING) counts.pending += 1
    if (subject.state === ModerationState.MODERATION_STATE_APPROVED) counts.approved += 1
    if (subject.state === ModerationState.MODERATION_STATE_REJECTED) counts.rejected += 1
    if (subject.state === ModerationState.MODERATION_STATE_HIDDEN) counts.hidden += 1
    if (subject.state === ModerationState.MODERATION_STATE_SPAM) counts.spam += 1
    if (
      subject.state === ModerationState.MODERATION_STATE_REJECTED ||
      subject.state === ModerationState.MODERATION_STATE_HIDDEN ||
      subject.state === ModerationState.MODERATION_STATE_SPAM
    ) {
      counts.blocked += 1
      if (subject.reasonCode.trim()) counts.reasonedBlocked += 1
    }
  }
  return counts
}

function publicPrivacySummary(totals: PublicPrivacyPreflight['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} privacy preflight checks are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} privacy preflight checks need evidence`
  if (totals.watch > 0) return `${totals.watch} privacy preflight checks need attention`
  return 'public privacy preflight evidence is verified'
}

function enabledPublicSurfaceCount(policy: PublicVisibilityPolicy | undefined) {
  if (!policy) return 0
  return [
    policy.requestsEnabled,
    policy.commentsEnabled,
    policy.roadmapEnabled,
    policy.changelogEnabled,
  ].filter(Boolean).length
}

function portalFieldCount(policy: PublicVisibilityPolicy | undefined) {
  return policy?.portalSubmissionForm?.fields.length ?? 0
}

function accessModeLabel(policy: PublicVisibilityPolicy | undefined) {
  if (!policy) return 'public policy unknown'
  switch (policy.portalAccessMode) {
    case PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC:
      return 'public'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED:
      return 'authenticated'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_INVITE_ONLY:
      return 'invite_only'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED:
      return 'disabled'
    default:
      return 'unknown'
  }
}

function identityModeLabel(mode: PublicIdentityMode) {
  switch (mode) {
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS:
      return 'anonymous'
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME:
      return 'display_name'
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION:
      return 'organization'
    default:
      return 'unknown'
  }
}

function moderationStateLabel(state: ModerationState) {
  switch (state) {
    case ModerationState.MODERATION_STATE_PENDING:
      return 'pending'
    case ModerationState.MODERATION_STATE_APPROVED:
      return 'approved'
    case ModerationState.MODERATION_STATE_REJECTED:
      return 'rejected'
    case ModerationState.MODERATION_STATE_HIDDEN:
      return 'hidden'
    case ModerationState.MODERATION_STATE_SPAM:
      return 'spam'
    default:
      return 'unknown'
  }
}

function surfaceState(enabled: boolean, label: string) {
  return `${label} ${onOff(enabled)}`
}

function onOff(enabled: boolean) {
  return enabled ? 'on' : 'off'
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}
