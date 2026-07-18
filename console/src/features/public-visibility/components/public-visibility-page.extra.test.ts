import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'
import {
  ModerationState,
  type ModerationSubject,
  PortalSubmissionFieldKind,
  PublicSurface,
  type SavedPublicVisibilityView,
} from '@/proto/attune/v1/public_visibility'
import { publicVisibilityPageTestables } from './public-visibility-page'

const t = ((
  key: string,
  opts?: { defaultValue?: string; count?: number; queue?: string; surfaces?: string },
) => {
  const catalog: Record<string, string> = {
    'public_visibility.queue_views.pending': 'Pending',
    'public_visibility.queue_views.approved': 'Approved',
    'public_visibility.queue_views.blocked': 'Blocked',
    'public_visibility.queue_views.all': 'All',
    'public_visibility.surfaces.PUBLIC_SURFACE_REQUEST': 'Request',
    'public_visibility.surfaces.PUBLIC_SURFACE_REQUEST_COMMENT': 'Request comment',
    'public_visibility.surfaces.PUBLIC_SURFACE_ROADMAP_ITEM': 'Roadmap item',
    'public_visibility.surfaces.PUBLIC_SURFACE_CHANGELOG_POST': 'Changelog post',
    'public_visibility.surfaces.PUBLIC_SURFACE_PORTAL_SUBMISSION': 'Portal submission',
    'public_visibility.states.MODERATION_STATE_APPROVED': 'Approved state',
    'public_visibility.reason_codes.operator.approved': 'Approved',
    'public_visibility.reason_codes.operator.rejected': 'Rejected',
    'public_visibility.reason_codes.operator.hidden': 'Hidden',
    'public_visibility.reason_codes.operator.spam': 'Spam',
    'public_visibility.reason_codes.operator.restored': 'Restored',
    'public_visibility.reason_codes.policy.safe': 'Safe',
    'public_visibility.reason_codes.policy.sensitive': 'Sensitive',
    'public_visibility.reason_codes.policy.out_of_scope': 'Out of scope',
    'public_visibility.reason_codes.policy.low_quality': 'Low quality',
    'public_visibility.reason_codes.policy.outdated': 'Outdated',
    'public_visibility.reason_codes.policy.private': 'Private',
    'public_visibility.reason_codes.policy.redacted': 'Redacted',
    'public_visibility.reason_codes.policy.corrected': 'Corrected',
    'public_visibility.reason_codes.appeal.accepted': 'Appeal accepted',
  }

  if (key === 'public_visibility.saved_views.summary_queue_only') {
    return `${opts?.queue}`
  }
  if (key === 'public_visibility.saved_views.summary') {
    return `${opts?.queue} · ${opts?.surfaces}`
  }
  if (key === 'public_visibility.portal.field.kind_values.text') return 'Text'
  if (key === 'public_visibility.portal.field.kind_values.select') return 'Select'
  if (key === 'public_visibility.portal.field.kind_values.multiselect') return 'Multi-select'
  if (key === 'public_visibility.portal.field.kind_values.textarea') return 'Textarea'
  if (key === 'public_visibility.portal.field.kind_values.boolean') return 'Boolean'
  return catalog[key] ?? opts?.defaultValue ?? key
}) as TFunction

function subject(id: string, state: ModerationState): ModerationSubject {
  return {
    id,
    state,
  } as ModerationSubject
}

describe('public visibility helper coverage', () => {
  it('normalizes queue and surface filters for saved views', () => {
    const filters = {
      queueView: '  BLOCKED ',
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.UNRECOGNIZED,
        PublicSurface.PUBLIC_SURFACE_REQUEST,
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
      ],
    } as unknown as Parameters<typeof publicVisibilityPageTestables.savedViewStateFromFilters>[0]

    expect(publicVisibilityPageTestables.savedViewStateFromFilters(filters)).toEqual({
      queueView: 'blocked',
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST,
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
      ],
    })
    expect(publicVisibilityPageTestables.savedViewStateToFilters(undefined)).toEqual({
      queueView: 'pending',
      surfaces: [],
    })
    expect(publicVisibilityPageTestables.savedViewStateSignature(undefined)).toBe('pending|')
    expect(publicVisibilityPageTestables.normalizeQueueView('bogus')).toBe('pending')
    expect(
      publicVisibilityPageTestables.normalizeSurfaceSelection([
        PublicSurface.UNRECOGNIZED,
        PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
        PublicSurface.PUBLIC_SURFACE_REQUEST,
      ]),
    ).toEqual([
      PublicSurface.PUBLIC_SURFACE_REQUEST,
      PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
    ])

    const savedView: SavedPublicVisibilityView = {
      id: 'view-1',
      name: 'Approved portal requests',
      state: {
        queueView: 'approved',
        surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
      },
      createdAt: '2026-07-10T00:00:00Z',
      updatedAt: '2026-07-10T00:00:00Z',
    }
    const currentFilters = {
      queueView: 'blocked' as const,
      surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
    }

    expect(
      publicVisibilityPageTestables.savedViewSaveRequest(null, '  Triage  ', currentFilters),
    ).toEqual({
      kind: 'create',
      name: 'Triage',
      state: {
        queueView: 'blocked',
        surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
      },
    })
    expect(
      publicVisibilityPageTestables.savedViewSaveRequest(
        savedView,
        '  Approved requests  ',
        currentFilters,
      ),
    ).toEqual({
      kind: 'update',
      id: 'view-1',
      name: 'Approved requests',
      state: {
        queueView: 'blocked',
        surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
      },
    })
    expect(
      publicVisibilityPageTestables.savedViewSaveRequest(savedView, '   ', currentFilters),
    ).toBeNull()
    expect(publicVisibilityPageTestables.savedViewDeleteID(savedView)).toBe('view-1')
    expect(publicVisibilityPageTestables.savedViewDeleteID(null)).toBeNull()
    expect(
      publicVisibilityPageTestables.savedViewSelectionFromValue([savedView], '__current__'),
    ).toEqual({ selectedID: '', filters: null })
    expect(
      publicVisibilityPageTestables.savedViewSelectionFromValue([savedView], 'missing'),
    ).toEqual({ selectedID: '', filters: null })
    expect(
      publicVisibilityPageTestables.savedViewSelectionFromValue([savedView], 'view-1'),
    ).toEqual({
      selectedID: 'view-1',
      filters: {
        queueView: 'approved',
        surfaces: [PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION],
      },
    })
  })

  it('describes and names saved moderation views', () => {
    const filters = {
      queueView: 'approved' as const,
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST,
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
      ],
    }

    expect(publicVisibilityPageTestables.describeSavedViewState(filters, t)).toBe(
      'Approved · Request · Request comment · Portal submission',
    )
    expect(publicVisibilityPageTestables.suggestSavedViewName(filters, t)).toBe(
      'Approved · Request · Request comment',
    )
    expect(
      publicVisibilityPageTestables.describeSavedViewState({ queueView: 'all', surfaces: [] }, t),
    ).toBe('All')
  })

  it('covers moderation action, reason, and field-kind helpers', () => {
    expect(publicVisibilityPageTestables.reasonCodeForAction('approve')).toBe('operator.approved')
    expect(publicVisibilityPageTestables.reasonCodeForAction('restore')).toBe('operator.restored')
    expect(publicVisibilityPageTestables.actionRequiresReason('approve')).toBe(false)
    expect(publicVisibilityPageTestables.actionRequiresReason('spam')).toBe(true)
    expect(publicVisibilityPageTestables.reasonOptionsForAction('hide')).toEqual([
      'operator.hidden',
      'policy.sensitive',
      'policy.outdated',
      'policy.private',
    ])
    expect(publicVisibilityPageTestables.formatReasonCode(t, 'operator.approved')).toBe('Approved')
    expect(
      publicVisibilityPageTestables.formatSurface(t, PublicSurface.PUBLIC_SURFACE_REQUEST),
    ).toBe('Request')
    expect(publicVisibilityPageTestables.formatState(t, 'MODERATION_STATE_APPROVED')).toBe(
      'Approved state',
    )
    expect(
      publicVisibilityPageTestables.portalSubmissionFieldKindLabel(
        PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
        t,
      ),
    ).toBe('Select')
    expect(publicVisibilityPageTestables.portalSubmissionFieldKindOptions(t)).toEqual([
      { value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT, label: 'Text' },
      { value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA, label: 'Textarea' },
      { value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT, label: 'Select' },
      {
        value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
        label: 'Multi-select',
      },
      { value: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN, label: 'Boolean' },
    ])
  })

  it('counts and filters moderation subjects across all queue views', () => {
    const subjects = [
      subject('pending', ModerationState.MODERATION_STATE_PENDING),
      subject('approved', ModerationState.MODERATION_STATE_APPROVED),
      subject('hidden', ModerationState.MODERATION_STATE_HIDDEN),
      subject('rejected', ModerationState.MODERATION_STATE_REJECTED),
      subject('spam', ModerationState.MODERATION_STATE_SPAM),
    ]

    expect(publicVisibilityPageTestables.countStates(subjects)).toEqual({
      pending: 1,
      approved: 1,
      hidden: 3,
    })
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'pending')).toEqual([subjects[0]])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'approved')).toEqual([
      subjects[1],
    ])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'blocked')).toEqual([
      subjects[2],
      subjects[3],
      subjects[4],
    ])
    expect(publicVisibilityPageTestables.filterSubjects(subjects, 'all')).toEqual(subjects)
  })

  it('formats fallback text and safe message strings', () => {
    expect(publicVisibilityPageTestables.messageOf(new Error('boom'))).toBe('boom')
    expect(publicVisibilityPageTestables.messageOf('nope')).toBe('failed')
  })
})
