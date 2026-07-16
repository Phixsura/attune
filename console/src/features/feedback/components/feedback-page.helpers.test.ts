import { describe, expect, it } from 'vitest'
import type { Feedback, FeedbackListFilters } from '@/features/feedback/api/list-feedback-infinite'
import type { Dimension } from '@/proto/attune/v1/common'
import { feedbackPageTestables } from './feedback-page'

const t = (key: string) => key
const fallbackT = (key: string, options?: { defaultValue?: string }) => options?.defaultValue ?? key

const urgentFeedback = {
  id: 'urgent',
  createdAt: '2026-06-16T10:00:00Z',
  enrichmentStatus: 'done',
  isUrgent: true,
  workflowState: { category: 'active' },
  enrichedAttrs: {},
  tags: [],
  allowedNextStates: [],
  content: 'urgent feedback',
  source: 'api',
  type: 'request',
  userId: 'user-1',
  pageUrl: 'https://example.com',
} as unknown as Feedback

const activeFeedback = {
  ...urgentFeedback,
  id: 'active',
  isUrgent: false,
  workflowState: { category: 'active' },
  createdAt: '2026-06-16T09:00:00Z',
} as unknown as Feedback

const closedFeedback = {
  ...urgentFeedback,
  id: 'closed',
  isUrgent: false,
  workflowState: { category: 'closed' },
  createdAt: '2026-06-16T08:00:00Z',
} as unknown as Feedback

const defaultFeedback = {
  ...urgentFeedback,
  id: 'default',
  isUrgent: false,
  workflowState: undefined,
  createdAt: '2026-06-15T08:00:00Z',
  enrichmentStatus: '',
} as unknown as Feedback

describe('feedbackPageTestables', () => {
  it('derives sync keys, labels, and queue copy', () => {
    expect(feedbackPageTestables.qualityFilterSyncKey(undefined)).toBe('')
    expect(
      feedbackPageTestables.qualityFilterSyncKey({
        ids: ['2', '1'],
        confidenceLte: 0.5,
        createdFrom: '2026-06-16T00:00:00Z',
        createdTo: '2026-06-17T00:00:00Z',
        enrichedFrom: '2026-06-18T00:00:00Z',
        enrichedTo: '2026-06-19T00:00:00Z',
        qualitySignal: 'off_list',
      }),
    ).toBe(
      '2,1\x1f0.5\x1f2026-06-16T00:00:00Z\x1f2026-06-17T00:00:00Z\x1f2026-06-18T00:00:00Z\x1f2026-06-19T00:00:00Z\x1foff_list',
    )
    expect(feedbackPageTestables.scopeFilterSyncKey('portal', 'bug')).toBe('portal\x1fbug')
    expect(feedbackPageTestables.scopeFilterSyncKey('', '')).toBe('\x1f')
    expect(feedbackPageTestables.compactStatToneClass('default')).toBe(
      'border-border/60 bg-background',
    )
    expect(feedbackPageTestables.compactStatToneClass('urgent')).toBe(
      'border-red-200/75 bg-red-50/45',
    )
    expect(feedbackPageTestables.compactStatToneClass('active')).toBe(
      'border-amber-200/75 bg-amber-50/40',
    )
    expect(feedbackPageTestables.sortModeLabel('urgent', t)).toBe('feedback.sort.urgent')
    expect(feedbackPageTestables.sortModeLabel('active', t)).toBe('feedback.sort.active')
    expect(feedbackPageTestables.sortModeLabel('newest', t)).toBe('feedback.sort.newest')
    expect(feedbackPageTestables.queueModeLabel('urgent', t)).toBe('feedback.queue_mode.urgent')
    expect(feedbackPageTestables.queueModeLabel('active', t)).toBe('feedback.queue_mode.active')
    expect(feedbackPageTestables.queueModeLabel('failed', t)).toBe('feedback.queue_mode.failed')
    expect(feedbackPageTestables.queueModeLabel('terminal', t)).toBe('feedback.queue_mode.terminal')
    expect(feedbackPageTestables.queueModeLabel('ready', t)).toBe('feedback.queue_mode.ready')
    expect(feedbackPageTestables.queueModeLabel('all', t)).toBe('feedback.queue_mode.all')
    expect(feedbackPageTestables.enrichmentStatusLabel('pending', t)).toBe(
      'feedback.status.pending',
    )
    expect(feedbackPageTestables.enrichmentStatusLabel('enriching', t)).toBe(
      'feedback.status.enriching',
    )
    expect(feedbackPageTestables.enrichmentStatusLabel('done', t)).toBe('feedback.status.done')
    expect(feedbackPageTestables.enrichmentStatusLabel('failed', t)).toBe('feedback.status.failed')
    expect(feedbackPageTestables.enrichmentStatusLabel('unknown', t)).toBe('unknown')
    expect(feedbackPageTestables.feedbackSourceLabel('portal', t)).toBe('feedback.source.portal')
    expect(feedbackPageTestables.feedbackSourceLabel('custom-source', fallbackT)).toBe(
      'custom-source',
    )
    expect(feedbackPageTestables.feedbackSourceRowLabel('portal', t)).toBe(
      'feedback.row.portal_submission',
    )
    expect(feedbackPageTestables.feedbackSourceRowLabel('portal', fallbackT)).toBe('portal')
    expect(feedbackPageTestables.feedbackSourceRowLabel('web', t)).toBe('feedback.source.web')
    expect(feedbackPageTestables.feedbackTypeLabel('bug', t)).toBe('feedback.type.bug')
    expect(feedbackPageTestables.feedbackTypeLabel('incident', fallbackT)).toBe('incident')
    expect(feedbackPageTestables.feedbackSourceOptions(t)).toEqual([
      { value: 'api', label: 'feedback.source.api' },
      { value: 'web', label: 'feedback.source.web' },
      { value: 'mcp', label: 'feedback.source.mcp' },
      { value: 'portal', label: 'feedback.source.portal' },
      { value: 'other', label: 'feedback.source.other' },
    ])
    expect(feedbackPageTestables.feedbackTypeOptions(t)).toEqual([
      { value: 'request', label: 'feedback.type.request' },
      { value: 'bug', label: 'feedback.type.bug' },
      { value: 'general', label: 'feedback.type.general' },
    ])
    expect(feedbackPageTestables.qualitySignalLabel('low_confidence', t)).toBe(
      'feedback.quality_filters.low_confidence',
    )
    expect(feedbackPageTestables.qualitySignalLabel('off_list', t)).toBe(
      'feedback.quality_filters.off_list',
    )
    expect(feedbackPageTestables.qualitySignalLabel('parse_failure', t)).toBe(
      'feedback.quality_filters.parse_failure',
    )
    expect(feedbackPageTestables.qualitySignalLabel('terminal_failure', t)).toBe(
      'feedback.quality_filters.terminal_failure',
    )
    expect(feedbackPageTestables.qualitySignalLabel('custom', t)).toBe('custom')
  })

  it('covers queue posture and ai health branches', () => {
    expect(feedbackPageTestables.queuePrimaryActionLabel('urgent', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_urgent',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('active', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_active',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('failed', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_failed',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('terminal', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_terminal',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('ready', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_ready',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('all', 1, 0, 0, t)).toBe(
      'feedback.queue.actions.open_urgent',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('all', 0, 0, 2, t)).toBe(
      'feedback.queue.actions.open_terminal',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('all', 0, 1, 0, t)).toBe(
      'feedback.queue.actions.open_failed',
    )
    expect(feedbackPageTestables.queuePrimaryActionLabel('all', 0, 0, 0, t)).toBe(
      'feedback.queue.actions.open_priority',
    )

    expect(feedbackPageTestables.queuePostureTone('all', 0, 0, 0, 0, 0)).toBe('default')
    expect(feedbackPageTestables.queuePostureTone('urgent', 3, 0, 0, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queuePostureTone('failed', 3, 0, 0, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queuePostureTone('active', 3, 0, 0, 0, 0)).toBe('success')
    expect(feedbackPageTestables.queuePostureTone('ready', 3, 0, 0, 0, 0)).toBe('success')
    expect(feedbackPageTestables.queuePostureTone('all', 3, 1, 0, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queuePostureTone('all', 3, 0, 3, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queuePostureTone('all', 3, 0, 0, 3, 0)).toBe('success')
    expect(feedbackPageTestables.queuePostureTone('all', 3, 0, 0, 0, 2)).toBe('warning')
    expect(feedbackPageTestables.queuePostureTone('all', 3, 0, 0, 0, 0)).toBe('default')

    expect(feedbackPageTestables.queuePostureLabel('all', 0, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_empty',
    )
    expect(feedbackPageTestables.queuePostureLabel('urgent', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_urgent',
    )
    expect(feedbackPageTestables.queuePostureLabel('failed', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_failure',
    )
    expect(feedbackPageTestables.queuePostureLabel('active', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_active',
    )
    expect(feedbackPageTestables.queuePostureLabel('all', 3, 0, 3, 0, 0, t)).toBe(
      'feedback.queue.posture_failure',
    )
    expect(feedbackPageTestables.queuePostureLabel('all', 3, 0, 0, 3, 0, t)).toBe(
      'feedback.queue.posture_ready',
    )
    expect(feedbackPageTestables.queuePostureLabel('ready', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_ready',
    )
    expect(feedbackPageTestables.queuePostureLabel('all', 3, 0, 0, 0, 2, t)).toBe(
      'feedback.queue.posture_pending',
    )
    expect(feedbackPageTestables.queuePostureLabel('all', 3, 0, 0, 1, 0, t)).toBe(
      'feedback.queue.posture_mixed',
    )

    expect(feedbackPageTestables.queuePostureHint('all', 0, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_empty_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('urgent', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_urgent_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('failed', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_failure_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('active', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_active_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('all', 3, 0, 3, 0, 0, t)).toBe(
      'feedback.queue.posture_failure_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('all', 3, 0, 0, 3, 0, t)).toBe(
      'feedback.queue.posture_ready_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('ready', 3, 0, 0, 0, 0, t)).toBe(
      'feedback.queue.posture_ready_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('all', 3, 0, 0, 0, 2, t)).toBe(
      'feedback.queue.posture_pending_hint',
    )
    expect(feedbackPageTestables.queuePostureHint('all', 3, 0, 0, 1, 0, t)).toBe(
      'feedback.queue.posture_mixed_hint',
    )

    expect(feedbackPageTestables.queueAiHealthTone(0, 0, 0, 0)).toBe('default')
    expect(feedbackPageTestables.queueAiHealthTone(3, 3, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queueAiHealthTone(3, 0, 3, 0)).toBe('success')
    expect(feedbackPageTestables.queueAiHealthTone(3, 0, 0, 1)).toBe('warning')
    expect(feedbackPageTestables.queueAiHealthTone(3, 1, 0, 0)).toBe('danger')
    expect(feedbackPageTestables.queueAiHealthTone(3, 0, 0, 0)).toBe('default')

    expect(feedbackPageTestables.queueAiHealthLabel(0, 0, 0, 0, t)).toBe(
      'feedback.queue.ai_health_empty',
    )
    expect(feedbackPageTestables.queueAiHealthLabel(3, 3, 0, 0, t)).toBe(
      'feedback.queue.ai_health_failed',
    )
    expect(feedbackPageTestables.queueAiHealthLabel(3, 0, 3, 0, t)).toBe(
      'feedback.queue.ai_health_ready',
    )
    expect(feedbackPageTestables.queueAiHealthLabel(3, 0, 0, 1, t)).toBe(
      'feedback.queue.ai_health_pending',
    )
    expect(feedbackPageTestables.queueAiHealthLabel(3, 1, 0, 0, t)).toBe(
      'feedback.queue.ai_health_mixed',
    )
    expect(feedbackPageTestables.queueAiHealthLabel(3, 0, 0, 0, t)).toBe(
      'feedback.queue.ai_health_mixed',
    )

    expect(feedbackPageTestables.queueAiHealthHint(0, 0, 0, 0, t)).toBe(
      'feedback.queue.ai_health_empty_hint',
    )
    expect(feedbackPageTestables.queueAiHealthHint(3, 3, 0, 0, t)).toBe(
      'feedback.queue.ai_health_failed_hint',
    )
    expect(feedbackPageTestables.queueAiHealthHint(3, 0, 3, 0, t)).toBe(
      'feedback.queue.ai_health_ready_hint',
    )
    expect(feedbackPageTestables.queueAiHealthHint(3, 0, 0, 1, t)).toBe(
      'feedback.queue.ai_health_pending_hint',
    )
    expect(feedbackPageTestables.queueAiHealthHint(3, 1, 0, 0, t)).toBe(
      'feedback.queue.ai_health_mixed_hint',
    )

    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('all', 0, 0, 0, 0)).toBe(false)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('failed', 3, 1, 0, 0)).toBe(true)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('active', 3, 1, 0, 0)).toBe(false)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('all', 0, 1, 0, 0)).toBe(true)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('all', 3, 3, 0, 0)).toBe(true)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('all', 3, 1, 0, 1)).toBe(false)
    expect(feedbackPageTestables.shouldSurfaceRuntimeLink('all', 3, 1, 0, 0)).toBe(true)
  })

  it('covers sorting, semantic filters, and ready-state helpers', () => {
    const items = [defaultFeedback, activeFeedback, urgentFeedback]
    expect(feedbackPageTestables.sortFeedbackItems(items, 'newest').map((item) => item.id)).toEqual(
      ['urgent', 'active', 'default'],
    )
    expect(feedbackPageTestables.sortFeedbackItems(items, 'urgent').map((item) => item.id)).toEqual(
      ['urgent', 'active', 'default'],
    )
    expect(feedbackPageTestables.sortFeedbackItems(items, 'active').map((item) => item.id)).toEqual(
      ['urgent', 'active', 'default'],
    )

    expect(
      feedbackPageTestables.filterFeedbackItemsByQueueMode(items, 'urgent').map((item) => item.id),
    ).toEqual(['urgent'])
    expect(
      feedbackPageTestables.filterFeedbackItemsByQueueMode(items, 'active').map((item) => item.id),
    ).toEqual(['active', 'urgent'])
    expect(feedbackPageTestables.filterFeedbackItemsByQueueMode(items, 'failed')).toEqual([])
    expect(feedbackPageTestables.filterFeedbackItemsByQueueMode(items, 'all')).toEqual(items)

    const terminalItem = {
      ...defaultFeedback,
      id: 'terminal',
      enrichmentStatus: 'failed',
      enrichmentAttempts: 5,
      enrichmentNextRetryAt: null,
    } as unknown as Feedback
    const retryingFailure = {
      ...defaultFeedback,
      id: 'retrying-failure',
      enrichmentStatus: 'failed',
      enrichmentAttempts: 2,
      enrichmentNextRetryAt: '2026-06-16T11:00:00Z',
    } as unknown as Feedback
    expect(
      feedbackPageTestables
        .filterFeedbackItemsByQueueMode([terminalItem, retryingFailure], 'failed')
        .map((item) => item.id),
    ).toEqual(['terminal', 'retrying-failure'])
    expect(
      feedbackPageTestables
        .filterFeedbackItemsByQueueMode([terminalItem, retryingFailure], 'terminal')
        .map((item) => item.id),
    ).toEqual(['terminal'])
    expect(
      feedbackPageTestables
        .filterFeedbackItemsByQueueMode(
          [
            {
              ...defaultFeedback,
              id: 'ready',
              enrichmentStatus: 'done',
              enrichedTitle: 'AI title',
            } as unknown as Feedback,
            {
              ...defaultFeedback,
              id: 'ready-confidence',
              enrichmentStatus: 'done',
              classificationConfidence: 0,
            } as unknown as Feedback,
            {
              ...defaultFeedback,
              id: 'ready-attrs',
              enrichmentStatus: 'done',
              enrichedAttrs: { tags: ['billing'] },
            } as unknown as Feedback,
          ],
          'ready',
        )
        .map((item) => item.id),
    ).toEqual(['ready', 'ready-confidence', 'ready-attrs'])

    const dims: Dimension[] = [
      { name: 'region', kind: 'single' } as Dimension,
      { name: 'tags', kind: 'multi' } as Dimension,
    ]
    const filter = feedbackPageTestables.buildSemanticFilter(
      {
        attrs: [
          { dim: 'region', value: 'sg' },
          { dim: 'tags', value: 'billing' },
        ],
        urgent: true,
        tag: 'product',
        workflowState: 'state-1',
        enrichmentStatus: 'done',
        terminalFailedOnly: true,
      } as FeedbackListFilters,
      dims,
    )
    expect(filter).toEqual({
      attrs: [
        { dim: 'region', value: 'sg', multi: false },
        { dim: 'tags', value: 'billing', multi: true },
      ],
      enrichmentStatus: 'done',
      tagId: 'product',
      terminalFailedOnly: true,
      urgent: true,
      workflowStateId: 'state-1',
    })
    expect(
      feedbackPageTestables.buildSemanticFilter({ attrs: [] } as FeedbackListFilters, dims),
    ).toBeUndefined()
    expect(feedbackPageTestables.semanticFilterCacheKey(filter)).toBe(JSON.stringify(filter))
    expect(feedbackPageTestables.semanticFilterCacheKey(undefined)).toBe('{}')

    expect(
      feedbackPageTestables.semanticHitToFeedback({ feedback: { id: 7 } } as any),
    ).toMatchObject({
      id: '7',
      content: '',
      source: '',
      type: '',
      userId: '',
      pageUrl: '',
      enrichedAttrs: {},
      isUrgent: false,
      enrichmentStatus: '',
      createdAt: '',
      tags: [],
      allowedNextStates: [],
    })
    expect(
      feedbackPageTestables.semanticHitToFeedback({
        feedback: {
          id: 'kept',
          content: 'content',
          source: 'portal',
          type: 'bug',
          userId: 'user',
          pageUrl: 'https://example.com/page',
          enrichedAttrs: { product: 'console' },
          isUrgent: true,
          enrichmentStatus: 'done',
          createdAt: '2026-06-16T10:00:00Z',
          tags: [{ id: 'tag-1', name: 'Tag' }],
          allowedNextStates: [{ id: 'state-1', name: 'Next' }],
        },
      } as any),
    ).toMatchObject({
      id: 'kept',
      content: 'content',
      source: 'portal',
      type: 'bug',
      userId: 'user',
      pageUrl: 'https://example.com/page',
      enrichedAttrs: { product: 'console' },
      isUrgent: true,
      enrichmentStatus: 'done',
      createdAt: '2026-06-16T10:00:00Z',
      tags: [{ id: 'tag-1', name: 'Tag' }],
      allowedNextStates: [{ id: 'state-1', name: 'Next' }],
    })
    expect(feedbackPageTestables.semanticHitToFeedback({ feedback: {} } as any)).toBeNull()

    expect(
      feedbackPageTestables.hasFilledClassificationAttrs({ ...defaultFeedback, enrichedAttrs: {} }),
    ).toBe(false)
    expect(
      feedbackPageTestables.hasFilledClassificationAttrs({
        ...defaultFeedback,
        enrichedAttrs: { score: 1, tags: ['billing'], note: 'ok' },
      }),
    ).toBe(true)
    expect(
      feedbackPageTestables.hasFilledClassificationAttrs({
        ...defaultFeedback,
        enrichedAttrs: { score: false, tags: [], note: '' },
      }),
    ).toBe(true)
    expect(
      feedbackPageTestables.hasUsableAiTitle({ ...defaultFeedback, enrichedDisplayTitle: 'AI' }),
    ).toBe(true)
    expect(
      feedbackPageTestables.hasUsableAiTitle({ ...defaultFeedback, enrichedTitle: 'AI' }),
    ).toBe(true)
    expect(feedbackPageTestables.hasUsableAiTitle(defaultFeedback)).toBe(false)

    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'failed',
      }),
    ).toBe(false)
    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'pending',
      }),
    ).toBe(false)
    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'enriching',
      }),
    ).toBe(false)
    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'done',
        enrichedDisplayTitle: 'AI title',
      }),
    ).toBe(true)
    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'done',
        classificationConfidence: 0,
      }),
    ).toBe(true)
    expect(
      feedbackPageTestables.isFeedbackReadyForTriage({
        ...defaultFeedback,
        enrichmentStatus: 'done',
        enrichedAttrs: { note: 'classified' },
      }),
    ).toBe(true)

    expect(feedbackPageTestables.statusToneForFeedback(urgentFeedback)).toBe('urgent')
    expect(feedbackPageTestables.statusToneForFeedback(activeFeedback)).toBe('active')
    expect(feedbackPageTestables.statusToneForFeedback(closedFeedback)).toBe('closed')
    expect(feedbackPageTestables.statusToneForFeedback(defaultFeedback)).toBe('default')
    expect(feedbackPageTestables.statusLabelForFeedback(urgentFeedback, t)).toBe(
      'feedback.row.priority.urgent',
    )
    expect(feedbackPageTestables.statusLabelForFeedback(activeFeedback, t)).toBe(
      'feedback.row.priority.active',
    )
    expect(feedbackPageTestables.statusLabelForFeedback(closedFeedback, t)).toBe(
      'feedback.row.priority.closed',
    )
    expect(feedbackPageTestables.statusLabelForFeedback(defaultFeedback, t)).toBe(
      'feedback.row.priority.new',
    )

    expect(
      feedbackPageTestables.classificationToneForFeedback({
        ...defaultFeedback,
        enrichmentStatus: 'failed',
      }),
    ).toBe('error')
    expect(
      feedbackPageTestables.classificationToneForFeedback({
        ...defaultFeedback,
        enrichmentStatus: 'done',
        enrichedDisplayTitle: 'AI title',
      }),
    ).toBe('success')
    expect(
      feedbackPageTestables.classificationToneForFeedback({
        ...defaultFeedback,
        enrichmentStatus: 'pending',
      }),
    ).toBe('muted')
    expect(
      feedbackPageTestables.classificationLabelForFeedback(
        { ...defaultFeedback, enrichmentStatus: 'failed' },
        t,
      ),
    ).toBe('feedback.row.classification_failed')
    expect(
      feedbackPageTestables.classificationLabelForFeedback(
        {
          ...defaultFeedback,
          enrichmentStatus: 'done',
          enrichedDisplayTitle: 'AI title',
        },
        t,
      ),
    ).toBe('feedback.row.classification_ready')
    expect(
      feedbackPageTestables.classificationLabelForFeedback(
        {
          ...defaultFeedback,
          enrichmentStatus: 'enriching',
        },
        t,
      ),
    ).toBe('feedback.row.classification_enriching')
    expect(
      feedbackPageTestables.classificationLabelForFeedback(
        {
          ...defaultFeedback,
          enrichmentStatus: 'pending',
        },
        t,
      ),
    ).toBe('feedback.row.classification_pending')
    expect(feedbackPageTestables.classificationLabelForFeedback(defaultFeedback, t)).toBe(
      'feedback.row.unclassified_short',
    )

    expect(feedbackPageTestables.toneForFeedbackRow(true, 'active', false, false)).toContain(
      'border-red-200',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(true, 'active', true, false)).toContain(
      'border-red-200/90',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'active', true, false)).toContain(
      'amber',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'active', false, true)).toContain(
      'border-l-destructive/70',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'active', true, true)).toContain(
      'border-l-destructive',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'active', false, false)).toContain(
      'amber',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'closed', true, false)).toContain(
      'emerald',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, 'closed', false, false)).toContain(
      'emerald',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, '', true, false)).toContain(
      'border-slate',
    )
    expect(feedbackPageTestables.toneForFeedbackRow(false, '', false, false)).toContain(
      'border-border',
    )
    expect(feedbackPageTestables.eyebrowToneForFeedbackRow(true, 'active', false)).toBe(
      'bg-red-100 text-red-700',
    )
    expect(feedbackPageTestables.eyebrowToneForFeedbackRow(false, 'active', true)).toBe(
      'bg-destructive/10 text-destructive',
    )
    expect(feedbackPageTestables.eyebrowToneForFeedbackRow(false, 'active', false)).toBe(
      'bg-amber-100 text-amber-700',
    )
    expect(feedbackPageTestables.eyebrowToneForFeedbackRow(false, 'closed', false)).toBe(
      'bg-emerald-100 text-emerald-700',
    )
    expect(feedbackPageTestables.eyebrowToneForFeedbackRow(false, '', false)).toBe(
      'bg-muted text-muted-foreground',
    )
    expect(feedbackPageTestables.labelForFeedbackRow(true, 'active', false, t)).toBe(
      'feedback.row.priority.urgent',
    )
    expect(feedbackPageTestables.labelForFeedbackRow(false, 'active', true, t)).toBe(
      'feedback.row.priority.terminal',
    )
    expect(feedbackPageTestables.labelForFeedbackRow(false, 'active', false, t)).toBe(
      'feedback.row.priority.active',
    )
    expect(feedbackPageTestables.labelForFeedbackRow(false, 'closed', false, t)).toBe(
      'feedback.row.priority.closed',
    )
    expect(feedbackPageTestables.labelForFeedbackRow(false, '', false, t)).toBe(
      'feedback.row.priority.new',
    )
  })
})
