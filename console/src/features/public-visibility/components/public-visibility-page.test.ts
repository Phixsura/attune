import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'
import type {
  ModerationSubject,
  PublicRequestPublication,
  PublicVisibilityPolicy,
} from '@/proto/attune/v1/public_visibility'
import {
  ModerationState,
  PortalSubmissionFieldKind,
  PublicAccessMode,
  PublicIdentityMode,
  PublicSurface,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { publicVisibilityPageTestables, syncPublicationModeration } from './public-visibility-page'

describe('syncPublicationModeration', () => {
  it('updates the loaded publication moderation state for the matching subject', () => {
    const current = publicationWithModeration(
      moderationSubject('subject-1', ModerationState.MODERATION_STATE_PENDING),
    )
    const updated = moderationSubject('subject-1', ModerationState.MODERATION_STATE_APPROVED)

    expect(syncPublicationModeration(current, updated)?.moderation).toEqual(updated)
  })

  it('leaves unrelated loaded publication state unchanged', () => {
    const current = publicationWithModeration(
      moderationSubject('subject-1', ModerationState.MODERATION_STATE_PENDING),
    )
    const unrelated = moderationSubject('subject-2', ModerationState.MODERATION_STATE_APPROVED)

    expect(syncPublicationModeration(current, unrelated)).toBe(current)
  })
})

describe('publicVisibilityPageTestables', () => {
  it('counts and filters moderation subjects by queue view', () => {
    const pending = moderationSubject('pending', ModerationState.MODERATION_STATE_PENDING)
    const approved = moderationSubject('approved', ModerationState.MODERATION_STATE_APPROVED)
    const rejected = moderationSubject('rejected', ModerationState.MODERATION_STATE_REJECTED)
    const hidden = moderationSubject('hidden', ModerationState.MODERATION_STATE_HIDDEN)
    const spam = moderationSubject('spam', ModerationState.MODERATION_STATE_SPAM)
    const subjects = [pending, approved, rejected, hidden, spam]

    expect(publicVisibilityPageTestables.countStates(subjects)).toEqual({
      pending: 1,
      approved: 1,
      hidden: 3,
    })
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'pending')).toEqual([pending])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'approved')).toEqual([approved])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'blocked')).toEqual([
      rejected,
      hidden,
      spam,
    ])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'all')).toEqual(subjects)
  })

  it('maps saved views to and from moderation filters', () => {
    const t = ((key: string) => key) as TFunction
    const filters = {
      queueView: 'blocked' as const,
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
      ],
    }
    const state = publicVisibilityPageTestables.savedViewStateFromFilters(filters)

    expect(state).toEqual({
      queueView: 'blocked',
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
      ],
    })
    expect(publicVisibilityPageTestables.savedViewStateSignature(state)).toBe(
      'blocked|PUBLIC_SURFACE_REQUEST_COMMENT,PUBLIC_SURFACE_PORTAL_SUBMISSION',
    )
    expect(publicVisibilityPageTestables.savedViewStateToFilters(state)).toEqual({
      queueView: 'blocked',
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
      ],
    })
    expect(publicVisibilityPageTestables.suggestSavedViewName(filters, t)).toBe(
      'public_visibility.queue_views.blocked · public_visibility.surfaces.PUBLIC_SURFACE_REQUEST_COMMENT · public_visibility.surfaces.PUBLIC_SURFACE_PORTAL_SUBMISSION',
    )
    expect(publicVisibilityPageTestables.describeSavedViewState(filters, t)).toBe(
      'public_visibility.saved_views.summary',
    )
  })

  it('maps policy and profile records into editable form payloads', () => {
    const policy = policyFixture()
    const policyForm = publicVisibilityPageTestables.formFromPolicy(policy)
    expect(policyForm).toMatchObject({
      portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
      searchIndexingEnabled: true,
      requestsEnabled: true,
      submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
      defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
      submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
      showSubmitterDisplay: true,
      portalSubmissionForm: {
        headline: 'Share feedback',
        description: 'Tell us what is broken, missing, or worth improving.',
        acknowledgement: 'Thanks. We will review your submission.',
        submitButtonLabel: 'Submit feedback',
        showPageUrl: true,
        fields: [
          {
            key: 'severity',
            label: 'Severity',
            kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
            required: true,
            placeholder: 'Choose a severity',
            options: ['low', 'medium', 'high'],
          },
        ],
      },
    })
    expect(publicVisibilityPageTestables.policyRequestFromForm(policyForm)).toMatchObject({
      portalAccessMode: policy.portalAccessMode,
      searchIndexingEnabled: policy.searchIndexingEnabled,
      requestsEnabled: policy.requestsEnabled,
      commentsEnabled: policy.commentsEnabled,
      roadmapEnabled: policy.roadmapEnabled,
      changelogEnabled: policy.changelogEnabled,
      submissionWriteMode: policy.submissionWriteMode,
      commentWriteMode: policy.commentWriteMode,
      voteWriteMode: policy.voteWriteMode,
      defaultRequestState: policy.defaultRequestState,
      defaultCommentState: policy.defaultCommentState,
      submitterIdentityMode: policy.submitterIdentityMode,
      showVoteCount: policy.showVoteCount,
      showCommentCount: policy.showCommentCount,
      showSubmitterDisplay: policy.showSubmitterDisplay,
      hidePublicTimestamps: policy.hidePublicTimestamps,
      portalSubmissionForm: policyForm.portalSubmissionForm,
    })

    const form = publicVisibilityPageTestables.profileFormFromPublication(
      publicationWithModeration(
        moderationSubject('subject-1', ModerationState.MODERATION_STATE_APPROVED),
      ),
      'fallback-request',
    )
    expect(form).toMatchObject({
      requestId: 'request-1',
      publicSlug: 'request-one',
      publicTitle: 'Request one',
      includedInPortal: true,
      submittedByDisplay: '',
    })
    expect(
      publicVisibilityPageTestables.profileFormFromPublication(
        { moderation: formSubject() },
        'r-2',
      ),
    ).toEqual({ ...publicVisibilityPageTestables.defaultProfileForm(), requestId: 'r-2' })
    expect(publicVisibilityPageTestables.defaultPortalSubmissionForm()).toEqual({
      headline: 'Send feedback',
      description: 'Share bugs, ideas, or anything blocking your work.',
      acknowledgement: 'Thanks. We will review your submission.',
      submitButtonLabel: 'Submit feedback',
      showPageUrl: true,
      fields: [],
    })
    expect(
      publicVisibilityPageTestables.profileRequestFromForm({
        ...publicVisibilityPageTestables.defaultProfileForm(),
        requestId: ' request-2 ',
        publicSlug: ' slug ',
        publicTitle: ' title ',
        publicSummary: ' summary ',
        publicState: ' planned ',
        roadmapColumn: ' Next ',
        submittedByDisplay: ' Jane ',
      }),
    ).toMatchObject({
      requestId: 'request-2',
      publicSlug: 'slug',
      publicTitle: 'title',
      publicSummary: 'summary',
      publicState: 'planned',
      roadmapColumn: 'Next',
      submittedByDisplay: 'Jane',
    })
  })

  it('derives stable moderation reason defaults and formatting fallbacks', () => {
    expect(publicVisibilityPageTestables.reasonCodeForAction('approve')).toBe('operator.approved')
    expect(publicVisibilityPageTestables.reasonCodeForAction('reject')).toBe('operator.rejected')
    expect(publicVisibilityPageTestables.reasonCodeForAction('hide')).toBe('operator.hidden')
    expect(publicVisibilityPageTestables.reasonCodeForAction('spam')).toBe('operator.spam')
    expect(publicVisibilityPageTestables.reasonCodeForAction('restore')).toBe('operator.restored')

    expect(publicVisibilityPageTestables.actionRequiresReason('approve')).toBe(false)
    expect(publicVisibilityPageTestables.actionRequiresReason('restore')).toBe(false)
    expect(publicVisibilityPageTestables.actionRequiresReason('reject')).toBe(true)
    expect(publicVisibilityPageTestables.actionRequiresReason('hide')).toBe(true)
    expect(publicVisibilityPageTestables.actionRequiresReason('spam')).toBe(true)

    expect(publicVisibilityPageTestables.reasonOptionsForAction('approve')).toContain('policy.safe')
    expect(publicVisibilityPageTestables.reasonOptionsForAction('reject')).toContain(
      'policy.sensitive',
    )
    expect(publicVisibilityPageTestables.reasonOptionsForAction('hide')).toContain(
      'policy.outdated',
    )
    expect(publicVisibilityPageTestables.reasonOptionsForAction('spam')).toContain('abuse.spam')
    expect(publicVisibilityPageTestables.reasonOptionsForAction('restore')).toContain(
      'appeal.accepted',
    )

    expect(publicVisibilityPageTestables.formatReasonCode(t, 'operator.approved')).toBe('Approved')
    expect(
      publicVisibilityPageTestables.formatSurface(t, PublicSurface.PUBLIC_SURFACE_REQUEST),
    ).toBe('Request')
    expect(
      publicVisibilityPageTestables.formatState(t, ModerationState.MODERATION_STATE_APPROVED),
    ).toBe('Approved state')
    expect(publicVisibilityPageTestables.formatDate('')).toBe('')
    expect(publicVisibilityPageTestables.formatDate('not-a-date')).toBe('not-a-date')
    expect(publicVisibilityPageTestables.messageOf(new Error('failed request'))).toBe(
      'failed request',
    )
    expect(publicVisibilityPageTestables.messageOf('unknown')).toBe('failed')
  })

  it('normalizes portal submission form configs and field kinds', () => {
    const t = ((key: string) => key) as TFunction

    expect(publicVisibilityPageTestables.defaultForm().portalSubmissionForm).toEqual(
      publicVisibilityPageTestables.defaultPortalSubmissionForm(),
    )
    expect(publicVisibilityPageTestables.portalSubmissionFormFromPolicy(undefined)).toEqual(
      publicVisibilityPageTestables.defaultPortalSubmissionForm(),
    )

    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
      ),
    ).toBe('text')
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA,
      ),
    ).toBe('textarea')
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
      ),
    ).toBe('select')
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
      ),
    ).toBe('multiselect')
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN,
      ),
    ).toBe('boolean')
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindName(
        99 as unknown as PortalSubmissionFieldKind,
      ),
    ).toBe('text')

    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindLabel(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
        t,
      ),
    ).toBe('public_visibility.portal.field.kind_values.select')
    expect(publicVisibilityPageTestables.portalSubmissionFieldKindOptions(t)).toEqual([
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
        label: 'public_visibility.portal.field.kind_values.text',
      },
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA,
        label: 'public_visibility.portal.field.kind_values.textarea',
      },
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
        label: 'public_visibility.portal.field.kind_values.select',
      },
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
        label: 'public_visibility.portal.field.kind_values.multiselect',
      },
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN,
        label: 'public_visibility.portal.field.kind_values.boolean',
      },
    ])

    const policyForm = publicVisibilityPageTestables.portalSubmissionFormFromPolicy({
      headline: 'Portal title',
      description: 'Portal description',
      acknowledgement: 'Portal acknowledgement',
      submitButtonLabel: 'Send it',
      showPageUrl: false,
      fields: [
        {
          key: 'severity',
          label: 'Severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
          required: true,
          placeholder: 'Choose a severity',
          options: ['low', 'medium', 'high'],
        },
      ],
    })
    expect(policyForm).toEqual({
      headline: 'Portal title',
      description: 'Portal description',
      acknowledgement: 'Portal acknowledgement',
      submitButtonLabel: 'Send it',
      showPageUrl: false,
      fields: [
        {
          key: 'severity',
          label: 'Severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
          required: true,
          placeholder: 'Choose a severity',
          options: ['low', 'medium', 'high'],
        },
      ],
    })

    expect(
      publicVisibilityPageTestables.portalSubmissionFormRequestFromForm({
        headline: ' Portal title ',
        description: ' Portal description ',
        acknowledgement: ' Thanks ',
        submitButtonLabel: ' Send it ',
        showPageUrl: true,
        fields: [
          {
            key: ' Severity ',
            label: ' Severity ',
            kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
            required: true,
            placeholder: ' Choose a severity ',
            options: [' alpha ', ' ', ' beta '],
          },
        ],
      }),
    ).toEqual({
      headline: 'Portal title',
      description: 'Portal description',
      acknowledgement: 'Thanks',
      submitButtonLabel: 'Send it',
      showPageUrl: true,
      fields: [
        {
          key: 'severity',
          label: 'Severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
          required: true,
          placeholder: 'Choose a severity',
          options: ['alpha', 'beta'],
        },
      ],
    })
  })
})

function publicationWithModeration(moderation: ModerationSubject): PublicRequestPublication {
  return {
    profile: {
      id: 'profile-1',
      tenantId: 'tenant-1',
      requestId: 'request-1',
      publicSlug: 'request-one',
      publicTitle: 'Request one',
      publicSummary: '',
      publicState: '',
      roadmapColumn: '',
      includedInPortal: true,
      includedInRoadmap: false,
      updatedBy: 'admin-1',
      createdAt: '2026-07-10T00:00:00Z',
      updatedAt: '2026-07-10T00:00:00Z',
    },
    moderation,
  }
}

function moderationSubject(id: string, state: ModerationState): ModerationSubject {
  return {
    id,
    tenantId: 'tenant-1',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    subjectId: 'profile-1',
    state,
    reasonCode: '',
    reasonNote: '',
    submittedByDisplay: '',
    reviewedBy: '',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
  }
}

function formSubject(): ModerationSubject {
  return moderationSubject('subject-form', ModerationState.MODERATION_STATE_PENDING)
}

function policyFixture(): PublicVisibilityPolicy {
  return {
    tenantId: 'tenant-1',
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
    searchIndexingEnabled: true,
    requestsEnabled: true,
    commentsEnabled: false,
    roadmapEnabled: true,
    changelogEnabled: false,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
    showVoteCount: true,
    showCommentCount: false,
    showSubmitterDisplay: true,
    hidePublicTimestamps: false,
    portalSubmissionForm: {
      headline: 'Share feedback',
      description: 'Tell us what is broken, missing, or worth improving.',
      acknowledgement: 'Thanks. We will review your submission.',
      submitButtonLabel: 'Submit feedback',
      showPageUrl: true,
      fields: [
        {
          key: 'severity',
          label: 'Severity',
          kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
          required: true,
          options: ['low', 'medium', 'high'],
          placeholder: 'Choose a severity',
        },
      ],
    },
    updatedBy: 'admin-1',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
  }
}

const t = ((key: string, opts?: { defaultValue?: string }) => {
  const catalog: Record<string, string> = {
    'public_visibility.reason_codes.operator.approved': 'Approved',
    'public_visibility.surfaces.PUBLIC_SURFACE_REQUEST': 'Request',
    'public_visibility.states.MODERATION_STATE_APPROVED': 'Approved state',
  }
  return catalog[key] ?? opts?.defaultValue ?? key
}) as TFunction
