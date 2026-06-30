import { describe, expect, it } from 'vitest'
import {
  selectTerminalFailurePriority,
  type TerminalFailureWorkbenchSectionLike,
} from './terminal-failure-workbench'

describe('terminal-failure-workbench', () => {
  it('prefers higher count, then newer evidence, then older backfill, then stable keys', () => {
    const sections: TerminalFailureWorkbenchSectionLike[] = [
      {
        key: 'reason_class',
        title: 'Reason class',
        clusters: [
          {
            key: 'smaller',
            label: 'Smaller cluster',
            count: '4',
            oldestCreatedAt: '2026-06-01T00:00:00Z',
            newestCreatedAt: '2026-06-02T00:00:00Z',
            sampleFeedbackIds: ['101'],
          },
          {
            key: 'runner_up',
            label: 'Runner up',
            count: '5',
            oldestCreatedAt: '2026-06-01T00:00:00Z',
            newestCreatedAt: '2026-06-02T00:00:00Z',
            sampleFeedbackIds: ['102'],
          },
        ],
      },
      {
        key: 'model_channel',
        title: 'Model channel',
        clusters: [
          {
            key: 'freshest',
            label: 'Freshest cluster',
            count: '5',
            oldestCreatedAt: '2026-06-01T00:00:00Z',
            newestCreatedAt: '2026-06-03T00:00:00Z',
            sampleFeedbackIds: ['201'],
          },
        ],
      },
      {
        key: 'config_fingerprint',
        title: 'Config fingerprint',
        clusters: [
          {
            key: 'same_timing',
            label: 'Same timing cluster',
            count: '5',
            oldestCreatedAt: '2026-05-31T00:00:00Z',
            newestCreatedAt: '2026-06-03T00:00:00Z',
            sampleFeedbackIds: ['301'],
          },
        ],
      },
      {
        key: 'age_bucket',
        title: 'Age bucket',
        clusters: [
          {
            key: 'beta',
            label: 'Beta cluster',
            count: '5',
            oldestCreatedAt: '2026-05-31T00:00:00Z',
            newestCreatedAt: '2026-06-03T00:00:00Z',
            sampleFeedbackIds: ['401'],
          },
          {
            key: 'alpha',
            label: 'Alpha cluster',
            count: '5',
            oldestCreatedAt: '2026-05-31T00:00:00Z',
            newestCreatedAt: '2026-06-03T00:00:00Z',
            sampleFeedbackIds: ['402'],
          },
        ],
      },
    ]

    expect(selectTerminalFailurePriority(sections)).toMatchObject({
      sectionKey: 'age_bucket',
      sectionTitle: 'Age bucket',
      cluster: {
        key: 'alpha',
        label: 'Alpha cluster',
      },
    })
  })

  it('returns null when every section is empty', () => {
    expect(
      selectTerminalFailurePriority([
        {
          key: 'reason_class',
          title: 'Reason class',
          clusters: [],
        },
        {
          key: 'model_channel',
          title: 'Model channel',
          clusters: [],
        },
        {
          key: 'config_fingerprint',
          title: 'Config fingerprint',
          clusters: [],
        },
        {
          key: 'age_bucket',
          title: 'Age bucket',
          clusters: [],
        },
      ]),
    ).toBeNull()
  })
})
