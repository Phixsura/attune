import { describe, expect, it, vi } from 'vitest'
import { PromptVersionHistory } from '@/routes/_authed.settings'
import { renderWithProviders, screen, within } from '@/testing/test-utils'

const dim = (name: string) => ({
  name,
  displayName: { entries: {} },
  kind: 'single',
  taxonomy: [],
  urgentSet: [],
  required: false,
  examples: [],
  extractionHint: '',
})

describe('PromptVersionHistory', () => {
  it('shows a diff confirmation before activating an inactive version', async () => {
    const onActivate = vi.fn()
    const { user } = renderWithProviders(
      <PromptVersionHistory
        activating={false}
        canEdit={true}
        onActivate={onActivate}
        versions={[
          {
            id: 'active-version',
            promptVersion: 'enrich.default@1',
            promptFingerprint: 'sha256:active',
            schemaFingerprint: 'sha256:schema-active',
            policyId: 'enrich.default',
            policyVersion: '1',
            mode: 'default',
            promptSource: 'built_in',
            createdAt: '2026-06-21T00:00:00Z',
            isActive: true,
            hasTemplate: false,
            dimensionsCount: 1,
            warnings: [],
            dimensions: [dim('severity')],
            policyConfig: {
              outputLanguagePolicy: 'source_and_display',
              titleMaxChars: 30,
              rationaleMaxChars: 30,
              displayFieldsRequired: true,
              tone: 'concise',
              domainGuidance: '',
            },
          },
          {
            id: 'target-version',
            promptVersion: 'enrich.legacy_custom_template@sha256:abc',
            promptFingerprint: 'sha256:target',
            schemaFingerprint: 'sha256:schema-target',
            policyId: 'enrich.legacy_custom_template',
            policyVersion: 'sha256:abc',
            mode: 'legacy_custom_override',
            promptSource: 'custom_template',
            createdAt: '2026-06-21T01:00:00Z',
            isActive: false,
            hasTemplate: true,
            promptTemplate: 'custom {{content}}',
            dimensionsCount: 1,
            warnings: ['missing_dimensions'],
            dimensions: [dim('type')],
            policyConfig: {
              outputLanguagePolicy: 'display_only',
              titleMaxChars: 60,
              rationaleMaxChars: 30,
              displayFieldsRequired: true,
              tone: 'neutral',
              domainGuidance: '',
            },
          },
        ]}
      />,
    )

    await user.click(screen.getByRole('button', { name: '激活此版本' }))

    expect(onActivate).not.toHaveBeenCalled()
    const dialog = screen.getByRole('dialog', { name: '激活历史提示词版本' })
    expect(within(dialog).getByText('影响摘要')).toBeInTheDocument()
    expect(within(dialog).getByText('提示词模板会变化。')).toBeInTheDocument()
    expect(within(dialog).getByText('新增 1，删除 1，保留 0')).toBeInTheDocument()

    await user.click(within(dialog).getByTestId('prompt-version-activate-confirm'))
    expect(onActivate).toHaveBeenCalledWith('target-version')
  })
})
