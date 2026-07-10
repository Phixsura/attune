import { describe, expect, it } from 'vitest'
import type {
  ModerationSubject,
  PublicRequestPublication,
} from '@/proto/attune/v1/public_visibility'
import { ModerationState, PublicSurface } from '@/proto/attune/v1/public_visibility'
import { syncPublicationModeration } from './public-visibility-page'

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
