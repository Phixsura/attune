import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import {
  Bell,
  Bot,
  Gauge,
  GitBranch,
  History,
  Info,
  KeyRound,
  Loader2,
  Mailbox,
  Newspaper,
  RotateCcw,
  Scale,
  ShieldCheck,
  Sparkles,
  Tags,
  Users,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import { DigestSubscriptionPage } from '@/features/digest-subscription/components/digest-subscription-page'
import { GDPRPage } from '@/features/gdpr/components/gdpr-page'
import { GuardPoliciesPage } from '@/features/guard-policies/components/guard-policies-page'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { MembersPage } from '@/features/members/components/members-page'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { meQuery } from '@/features/session/api/get-me'
import { type Permission, usePermissions } from '@/features/session/hooks/use-permissions'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { listEnrichPromptVersions } from '@/features/settings/api/list-enrich-prompt-versions'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import {
  useActivateEnrichPromptVersion,
  useUpdateEnrichConfig,
} from '@/features/settings/api/update-enrich-config'
import { EnrichmentRuntimePage } from '@/features/settings/components/enrichment-runtime-page'
import { SuggestedValuesPanel } from '@/features/settings/components/suggested-values-panel'
import { TagsPage } from '@/features/tags/components/tags-page'
import { WorkflowSettingsPage } from '@/features/workflow/components/workflow-settings-page'
import type { Dimension } from '@/proto/attune/v1/common'
import type {
  EnrichConfig,
  EnrichPromptPolicy,
  EnrichPromptPolicyConfig,
  EnrichPromptVersion,
} from '@/proto/attune/v1/enrich_config'

type SettingsSection =
  | 'classification'
  | 'enrichment_runtime'
  | 'guard_policies'
  | 'inbound_sources'
  | 'notify_targets'
  | 'digest_subscription'
  | 'api_keys'
  | 'gdpr'
  | 'audit_log'
  | 'tags'
  | 'workflow'
  | 'members'

const SETTINGS_SECTIONS: SettingsSection[] = [
  'classification',
  'enrichment_runtime',
  'guard_policies',
  'inbound_sources',
  'notify_targets',
  'digest_subscription',
  'api_keys',
  'gdpr',
  'audit_log',
  'tags',
  'workflow',
  'members',
]

export const Route = createFileRoute('/_authed/settings')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQuery())
    const role = me.user?.role
    if (role !== 'admin' && role !== 'member') {
      throw redirect({ to: '/feedback' })
    }
  },
  validateSearch: (search: Record<string, unknown>): { section: SettingsSection } => {
    const raw = typeof search.section === 'string' ? search.section : 'classification'
    return {
      section: SETTINGS_SECTIONS.includes(raw as SettingsSection)
        ? (raw as SettingsSection)
        : 'classification',
    }
  },
  component: SettingsPage,
})

function SettingsPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { section } = Route.useSearch()
  const { can } = usePermissions()
  const cfg = useQuery(enrichConfigQuery())
  const save = useUpdateEnrichConfig()
  const activateVersion = useActivateEnrichPromptVersion()
  const preview = usePreviewEnrichPrompt()

  const [prompt, setPrompt] = useState('')
  const [dimensions, setDimensions] = useState<Dimension[]>([])
  const [policyConfig, setPolicyConfig] = useState<EnrichPromptPolicyConfig>(defaultPolicyConfig())
  const [sample, setSample] = useState('')
  const [previewText, setPreviewText] = useState('')
  const [previewPolicy, setPreviewPolicy] = useState<EnrichPromptPolicy | undefined>()
  const lastAppliedConfigKey = useRef('')
  const previewDraftKey = useRef('')
  const latestDraftKey = useRef('')
  const currentDraftKey = cfg.data
    ? enrichDraftKey({
        prompt,
        defaultPromptTemplate: cfg.data.defaultPromptTemplate,
        dimensions,
        policyConfig,
      })
    : ''
  latestDraftKey.current = currentDraftKey
  const serverDraftKey = cfg.data ? enrichConfigKey(cfg.data) : ''
  const isDirty =
    lastAppliedConfigKey.current !== '' &&
    currentDraftKey !== '' &&
    serverDraftKey !== '' &&
    currentDraftKey !== serverDraftKey

  useEffect(() => {
    if (!cfg.data) return
    const nextKey = enrichConfigKey(cfg.data)
    if (
      lastAppliedConfigKey.current !== '' &&
      isDirty &&
      lastAppliedConfigKey.current !== nextKey
    ) {
      return
    }
    setPrompt(cfg.data.promptTemplate ?? cfg.data.defaultPromptTemplate)
    setDimensions(cfg.data.dimensions ?? [])
    setPolicyConfig(normalizePolicyConfig(cfg.data.policyConfig))
    setPreviewText('')
    setPreviewPolicy(undefined)
    lastAppliedConfigKey.current = nextKey
  }, [cfg.data, isDirty])

  const handleRestoreDefault = () => {
    if (cfg.data?.defaultPromptTemplate) {
      setPrompt(cfg.data.defaultPromptTemplate)
      setPreviewText('')
      setPreviewPolicy(undefined)
    }
  }

  const handleSave = () => {
    if (!hasPromptVariable(prompt, 'content')) return
    const defaultTmpl = cfg.data?.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    save.mutate(
      {
        dimensions,
        promptTemplate: isDefaultPrompt ? undefined : prompt,
        policyConfig,
      },
      {
        onSuccess: (resp) => {
          if (resp.config) {
            lastAppliedConfigKey.current = enrichConfigKey(resp.config)
          }
          toast.success(t('settings.saved'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handlePreview = () => {
    const content = sample.trim() || t('settings.preview_sample_default')
    const defaultTmpl = cfg.data?.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    previewDraftKey.current = currentDraftKey
    preview.mutate(
      {
        sampleContent: content,
        dimensions,
        promptTemplate: isDefaultPrompt ? undefined : prompt,
        policyConfig,
        useDraftConfig: true,
      },
      {
        onSuccess: (resp) => {
          if (previewDraftKey.current !== latestDraftKey.current) return
          setPreviewText(resp.renderedPrompt)
          setPreviewPolicy(resp.promptPolicy)
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handleActivateVersion = (versionId: string) => {
    activateVersion.mutate(versionId, {
      onSuccess: (resp) => {
        if (resp.config) {
          lastAppliedConfigKey.current = enrichConfigKey(resp.config)
        }
        setPreviewText('')
        setPreviewPolicy(undefined)
        toast.success(t('settings.version_activated'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })
  }

  const constrained = dimensions.some((d) => d.taxonomy.length > 0)
  const modeLabel = constrained ? t('settings.mode_constrained') : t('settings.mode_freeform')
  const visibleSections = SETTINGS_SECTIONS.filter((item) => sectionVisible(item, can))
  const activeSection = visibleSections.includes(section)
    ? section
    : (visibleSections[0] ?? 'classification')
  const setSection = (next: SettingsSection) => {
    void navigate({
      to: '/settings',
      search: { section: next },
      replace: true,
    })
  }
  useEffect(() => {
    if (activeSection === section) return
    void navigate({
      to: '/settings',
      search: { section: activeSection },
      replace: true,
    })
  }, [activeSection, navigate, section])

  if (activeSection === 'classification' && cfg.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  if (activeSection === 'classification' && cfg.isError) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('nav.settings')}</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('settings.subtitle')}</p>
        </div>
        <div className="grid gap-6 lg:grid-cols-[240px_minmax(0,1fr)]">
          <SettingsSidebar active={activeSection} onSelect={setSection} />
          <Card>
            <CardHeader>
              <CardTitle>{t('settings.classification_error_title')}</CardTitle>
              <CardDescription>{t('settings.classification_error_body')}</CardDescription>
            </CardHeader>
            <CardContent>
              <Button type="button" variant="outline" onClick={() => void cfg.refetch()}>
                {t('common.retry')}
              </Button>
            </CardContent>
          </Card>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.settings')}</h1>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('settings.subtitle')}</p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[240px_minmax(0,1fr)]">
        <SettingsSidebar active={activeSection} onSelect={setSection} />

        {activeSection === 'classification' ? (
          <ClassificationSettings
            canEdit={can('settings:enrich_config:edit')}
            defaultPromptTemplate={cfg.data?.defaultPromptTemplate ?? ''}
            dimensions={dimensions}
            modeLabel={modeLabel}
            policy={cfg.data?.promptPolicy}
            previewPolicy={previewPolicy}
            previewPending={preview.isPending}
            previewText={previewText}
            prompt={prompt}
            policyConfig={policyConfig}
            sample={sample}
            activatingVersionId={
              activateVersion.isPending ? (activateVersion.variables ?? undefined) : undefined
            }
            promptVersions={cfg.data?.promptVersions ?? []}
            savePending={save.isPending}
            hasUnsavedChanges={isDirty}
            onActivateVersion={handleActivateVersion}
            onDimensionsChange={(next) => {
              setDimensions(next)
              setPreviewText('')
              setPreviewPolicy(undefined)
            }}
            onPreview={handlePreview}
            onPromptChange={(next) => {
              setPrompt(next)
              setPreviewText('')
              setPreviewPolicy(undefined)
            }}
            onPolicyConfigChange={(next) => {
              setPolicyConfig(next)
              setPreviewText('')
              setPreviewPolicy(undefined)
            }}
            onRestoreDefault={handleRestoreDefault}
            onSampleChange={setSample}
            onSave={handleSave}
          />
        ) : activeSection === 'enrichment_runtime' ? (
          <EnrichmentRuntimePage canEdit={can('settings:enrichment_runtime:edit')} />
        ) : (
          <SettingsSectionContent section={activeSection} />
        )}
      </div>
    </div>
  )
}

function ClassificationSettings({
  canEdit,
  defaultPromptTemplate,
  dimensions,
  modeLabel,
  onDimensionsChange,
  onActivateVersion,
  onPreview,
  onPolicyConfigChange,
  onPromptChange,
  onRestoreDefault,
  onSampleChange,
  onSave,
  previewPending,
  previewPolicy,
  previewText,
  policyConfig,
  promptVersions,
  policy,
  prompt,
  sample,
  activatingVersionId,
  savePending,
  hasUnsavedChanges,
}: {
  canEdit: boolean
  activatingVersionId?: string
  defaultPromptTemplate: string
  dimensions: Dimension[]
  modeLabel: string
  onActivateVersion: (versionId: string) => void
  onDimensionsChange: (dimensions: Dimension[]) => void
  onPreview: () => void
  onPolicyConfigChange: (policyConfig: EnrichPromptPolicyConfig) => void
  onPromptChange: (prompt: string) => void
  onRestoreDefault: () => void
  onSampleChange: (sample: string) => void
  onSave: () => void
  previewPending: boolean
  previewPolicy?: EnrichPromptPolicy
  previewText: string
  policyConfig: EnrichPromptPolicyConfig
  promptVersions: EnrichPromptVersion[]
  policy?: EnrichPromptPolicy
  prompt: string
  sample: string
  savePending: boolean
  hasUnsavedChanges: boolean
}) {
  const { t } = useTranslation()
  const activePolicy =
    previewPolicy ?? policyForCurrentPrompt(policy, prompt, defaultPromptTemplate)
  const promptState = policyState(activePolicy, prompt)
  const saveDisabled = savePending || !hasPromptVariable(prompt, 'content')
  return (
    <section className="min-w-0 space-y-6">
      <div>
        <h2 className="text-lg font-semibold tracking-tight">
          {t('settings.classification_title')}
        </h2>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
          {t('settings.classification_help')}
        </p>
        {hasUnsavedChanges ? (
          <p className="mt-2 text-xs text-amber-700">{t('settings.unsaved_changes')}</p>
        ) : null}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.prompt_title')}</CardTitle>
          <CardDescription>
            {t('settings.prompt_help')} · {modeLabel}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {activePolicy ? (
            <PromptPolicySummary policy={activePolicy} stateLabel={t(promptState)} />
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="prompt">{t('settings.prompt_label')}</Label>
            <textarea
              id="prompt"
              className="min-h-[220px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              value={prompt}
              onChange={(e) => onPromptChange(e.target.value)}
              disabled={!canEdit}
            />
            <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
            {!hasPromptVariable(prompt, 'content') ? (
              <p className="text-xs text-destructive">{t('settings.prompt_missing_content')}</p>
            ) : null}
          </div>
          {canEdit && (
            <Button type="button" variant="outline" size="sm" onClick={onRestoreDefault}>
              <RotateCcw className="mr-2 h-3.5 w-3.5" />
              {t('settings.restore_default')}
            </Button>
          )}
          {activePolicy ? <PromptContractDetails policy={activePolicy} /> : null}
        </CardContent>
      </Card>

      <PromptPolicyConfigPanel
        canEdit={canEdit}
        value={policyConfig}
        onChange={onPolicyConfigChange}
      />

      <PromptVersionHistory
        canEdit={canEdit}
        activatingVersionId={activatingVersionId}
        activating={Boolean(activatingVersionId)}
        versions={promptVersions}
        onActivate={onActivateVersion}
      />

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.dimensions_title')}</CardTitle>
          <CardDescription>{t('settings.dimensions_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          <DimensionsEditor value={dimensions} onChange={onDimensionsChange} disabled={!canEdit} />
        </CardContent>
      </Card>

      <SuggestedValuesPanel canEdit={canEdit} />

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.preview_title')}</CardTitle>
          <CardDescription>{t('settings.preview_help')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="sample">{t('settings.preview_sample_label')}</Label>
            <Input
              id="sample"
              placeholder={t('settings.preview_sample_placeholder')}
              value={sample}
              onChange={(e) => onSampleChange(e.target.value)}
            />
          </div>
          <Button type="button" variant="outline" onClick={onPreview} disabled={previewPending}>
            {previewPending ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <Sparkles className="mr-2 h-4 w-4" />
            )}
            {t('settings.preview_button')}
          </Button>
          {previewText ? (
            <pre className="max-h-80 overflow-auto rounded-md border border-border bg-muted/40 p-3 text-xs whitespace-pre-wrap">
              {previewText}
            </pre>
          ) : null}
        </CardContent>
      </Card>

      {canEdit && (
        <div className="flex justify-end">
          <Button onClick={onSave} disabled={saveDisabled}>
            {savePending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {t('common.save')}
          </Button>
        </div>
      )}
    </section>
  )
}

function policyState(policy: EnrichPromptPolicy | undefined, prompt: string) {
  if (!policy) return 'settings.policy_state_default'
  const hasWarnings = policy.warnings.some((w) => w.severity !== 'info')
  if (!hasPromptVariable(prompt, 'content')) return 'settings.policy_state_invalid'
  if (policy.promptSource === 'custom_template' && hasWarnings) {
    return 'settings.policy_state_warning'
  }
  if (policy.promptSource === 'custom_template') return 'settings.policy_state_custom'
  return 'settings.policy_state_default'
}

function policyForCurrentPrompt(
  policy: EnrichPromptPolicy | undefined,
  prompt: string,
  defaultPromptTemplate: string,
) {
  if (!policy) return undefined
  if (
    policy.promptSource === 'custom_template' &&
    defaultPromptTemplate.trim() &&
    prompt.trim() === defaultPromptTemplate.trim()
  ) {
    return defaultPromptPolicyFrom(policy)
  }
  return policy
}

function defaultPromptPolicyFrom(policy: EnrichPromptPolicy): EnrichPromptPolicy {
  return {
    ...policy,
    policyId: 'enrich.default',
    policyVersion: '1',
    promptVersion: 'enrich.default@1',
    promptFingerprint: '',
    schemaFingerprint: '',
    mode: 'default',
    promptSource: 'built_in',
    templateLanguage: policy.displayLocale.toLowerCase().startsWith('zh') ? 'zh' : 'en',
    warnings: [],
  }
}

function hasPromptVariable(prompt: string, name: string) {
  const escapedName = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`\\{\\{\\s*${escapedName}\\s*\\}\\}`).test(prompt)
}

function PromptPolicyConfigPanel({
  canEdit,
  onChange,
  value,
}: {
  canEdit: boolean
  onChange: (value: EnrichPromptPolicyConfig) => void
  value: EnrichPromptPolicyConfig
}) {
  const { t } = useTranslation()
  const cfg = normalizePolicyConfig(value)
  const update = (patch: Partial<EnrichPromptPolicyConfig>) =>
    onChange(normalizePolicyConfig({ ...cfg, ...patch }))
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('settings.policy_config_title')}</CardTitle>
        <CardDescription>{t('settings.policy_config_help')}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label>{t('settings.policy_output_language')}</Label>
          <Select
            value={cfg.outputLanguagePolicy}
            onValueChange={(next) => update({ outputLanguagePolicy: next })}
            disabled={!canEdit}
          >
            <SelectTrigger aria-label={t('settings.policy_output_language')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="source_and_display">
                {t('settings.policy_output_source_and_display')}
              </SelectItem>
              <SelectItem value="display_only">
                {t('settings.policy_output_display_only')}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t('settings.policy_tone')}</Label>
          <Select
            value={cfg.tone}
            onValueChange={(next) => update({ tone: next })}
            disabled={!canEdit}
          >
            <SelectTrigger aria-label={t('settings.policy_tone')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="concise">{t('settings.policy_tone_concise')}</SelectItem>
              <SelectItem value="neutral">{t('settings.policy_tone_neutral')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label htmlFor="title-max">{t('settings.policy_title_max')}</Label>
          <Input
            id="title-max"
            type="number"
            min={10}
            max={120}
            value={cfg.titleMaxChars}
            onChange={(event) => update({ titleMaxChars: Number(event.target.value) })}
            disabled={!canEdit}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="rationale-max">{t('settings.policy_rationale_max')}</Label>
          <Input
            id="rationale-max"
            type="number"
            min={10}
            max={240}
            value={cfg.rationaleMaxChars}
            onChange={(event) => update({ rationaleMaxChars: Number(event.target.value) })}
            disabled={!canEdit}
          />
        </div>
        <label className="flex items-center gap-2 text-sm md:col-span-2">
          <input
            type="checkbox"
            checked={cfg.displayFieldsRequired}
            onChange={(event) => update({ displayFieldsRequired: event.target.checked })}
            disabled={!canEdit}
          />
          {t('settings.policy_display_fields_required')}
        </label>
        <div className="space-y-2 md:col-span-2">
          <Label htmlFor="domain-guidance">{t('settings.policy_domain_guidance')}</Label>
          <textarea
            id="domain-guidance"
            className="min-h-[84px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            value={cfg.domainGuidance}
            onChange={(event) => update({ domainGuidance: event.target.value })}
            disabled={!canEdit}
          />
        </div>
      </CardContent>
    </Card>
  )
}

function defaultPolicyConfig(): EnrichPromptPolicyConfig {
  return {
    outputLanguagePolicy: 'source_and_display',
    titleMaxChars: 30,
    rationaleMaxChars: 30,
    displayFieldsRequired: true,
    tone: 'concise',
    domainGuidance: '',
  }
}

function normalizePolicyConfig(value?: EnrichPromptPolicyConfig): EnrichPromptPolicyConfig {
  return {
    ...defaultPolicyConfig(),
    ...value,
    titleMaxChars: value?.titleMaxChars || 30,
    rationaleMaxChars: value?.rationaleMaxChars || 30,
    displayFieldsRequired: value?.displayFieldsRequired ?? true,
  }
}

function enrichConfigKey(config: EnrichConfig) {
  return enrichDraftKey({
    prompt: config.promptTemplate ?? config.defaultPromptTemplate,
    defaultPromptTemplate: config.defaultPromptTemplate,
    dimensions: config.dimensions ?? [],
    policyConfig: normalizePolicyConfig(config.policyConfig),
  })
}

function enrichDraftKey({
  defaultPromptTemplate,
  dimensions,
  policyConfig,
  prompt,
}: {
  defaultPromptTemplate: string
  dimensions: Dimension[]
  policyConfig: EnrichPromptPolicyConfig
  prompt: string
}) {
  const isDefaultPrompt = prompt.trim() === defaultPromptTemplate.trim()
  return JSON.stringify({
    promptTemplate: isDefaultPrompt ? null : prompt,
    dimensions,
    policyConfig: normalizePolicyConfig(policyConfig),
  })
}

function PromptPolicySummary({
  policy,
  stateLabel,
}: {
  policy: EnrichPromptPolicy
  stateLabel: string
}) {
  const { t } = useTranslation()
  const warnings = policy.warnings.filter((w) => w.severity !== 'info')
  return (
    <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="max-w-full break-all rounded border border-border bg-background px-2 py-0.5 font-mono text-xs">
          {policy.promptVersion}
        </span>
        <span className="rounded border border-border bg-background px-2 py-0.5 text-xs">
          {stateLabel}
        </span>
        <span className="text-xs text-muted-foreground">
          {t('settings.policy_display_language', {
            language: policy.displayLanguageName,
            locale: policy.displayLocale,
          })}
        </span>
      </div>
      {warnings.length > 0 ? (
        <div className="mt-3 space-y-1">
          {warnings.map((warning) => (
            <div key={`${warning.code}-${warning.message}`} className="flex gap-2 text-xs">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-600" />
              <span>{promptWarningText(t, warning)}</span>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function PromptContractDetails({ policy }: { policy: EnrichPromptPolicy }) {
  const { t } = useTranslation()
  return (
    <details className="rounded-md border border-border bg-background p-3 text-xs">
      <summary className="cursor-pointer font-medium">{t('settings.policy_contract')}</summary>
      <div className="mt-3 grid gap-4 md:grid-cols-2">
        <div>
          <div className="mb-2 font-medium text-muted-foreground">
            {t('settings.policy_variables')}
          </div>
          <ul className="space-y-1">
            {policy.variables.map((item) => (
              <li key={item.name}>
                <span className="font-mono">{item.name}</span>
                <span className="text-muted-foreground">
                  {' '}
                  · {item.required ? t('settings.policy_required') : t('settings.policy_optional')}
                  {item.meaning ? ` · ${item.meaning}` : ''}
                </span>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <div className="mb-2 font-medium text-muted-foreground">
            {t('settings.policy_outputs')}
          </div>
          <ul className="space-y-1">
            {policy.outputs.map((item) => (
              <li key={item.name}>
                <span className="font-mono">{item.name}</span>
                <span className="text-muted-foreground">
                  {' '}
                  · {item.language} ·{' '}
                  {item.required ? t('settings.policy_required') : t('settings.policy_optional')}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </details>
  )
}

export function PromptVersionHistory({
  activating,
  activatingVersionId,
  canEdit,
  onActivate,
  versions,
}: {
  activating: boolean
  activatingVersionId?: string
  canEdit: boolean
  onActivate: (versionId: string) => void
  versions: EnrichPromptVersion[]
}) {
  const { t } = useTranslation()
  const [targetVersion, setTargetVersion] = useState<EnrichPromptVersion | null>(null)
  const [query, setQuery] = useState('')
  const versionSearch = query.trim()
  const versionHistory = useInfiniteQuery({
    queryKey: ['console', 'enrich-config', 'versions', versionSearch],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      listEnrichPromptVersions({
        cursor: pageParam,
        limit: 12,
        q: versionSearch,
        signal,
      }),
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    staleTime: 30_000,
  })
  const listedVersions =
    versionHistory.data?.pages.flatMap((page) => page.versions ?? []) ?? versions
  const displayVersions = versionSearch ? listedVersions : mergeVersions(versions, listedVersions)
  const activeVersion =
    displayVersions.find((version) => version.isActive) ??
    versions.find((version) => version.isActive)
  const closeDialog = () => setTargetVersion(null)
  const confirmActivate = () => {
    if (!targetVersion) return
    onActivate(targetVersion.id)
    closeDialog()
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('settings.version_history_title')}</CardTitle>
        <CardDescription>{t('settings.version_history_help')}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="mb-3">
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('settings.version_history_search')}
          />
        </div>
        {displayVersions.length === 0 ? (
          <div className="rounded-md border border-dashed border-border p-4 text-sm text-muted-foreground">
            {versionSearch
              ? t('settings.version_history_search_empty')
              : t('settings.version_history_empty')}
          </div>
        ) : (
          <div className="space-y-2">
            {displayVersions.map((version) => {
              const promptChanged = activeVersion
                ? !samePromptSnapshot(version, activeVersion)
                : false
              const dimensionsChanged = activeVersion
                ? !sameDimensionsSnapshot(version, activeVersion)
                : false
              return (
                <div key={version.id} className="rounded-md border border-border bg-background p-3">
                  <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                    <div className="min-w-0 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="max-w-full break-all rounded border border-border bg-muted px-2 py-0.5 font-mono text-xs">
                          {version.promptVersion}
                        </span>
                        {version.isActive ? (
                          <span className="rounded border border-border bg-muted px-2 py-0.5 text-xs">
                            {t('settings.version_active')}
                          </span>
                        ) : null}
                        {!version.isActive && promptChanged ? (
                          <span className="rounded border border-border px-2 py-0.5 text-xs">
                            {t('settings.version_prompt_changed')}
                          </span>
                        ) : null}
                        {!version.isActive && dimensionsChanged ? (
                          <span className="rounded border border-border px-2 py-0.5 text-xs">
                            {t('settings.version_dimensions_changed')}
                          </span>
                        ) : null}
                      </div>
                      <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                        <span>{formatVersionTime(version.createdAt)}</span>
                        <span>
                          {version.hasTemplate
                            ? t('settings.version_custom_template')
                            : t('settings.version_default_template')}
                        </span>
                        <span>
                          {t('settings.version_dimensions', { count: version.dimensionsCount })}
                        </span>
                        {version.warnings.length > 0 ? (
                          <span>
                            {t('settings.version_warnings', { count: version.warnings.length })}
                          </span>
                        ) : null}
                      </div>
                    </div>
                    {canEdit && !version.isActive ? (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="shrink-0"
                        disabled={activating}
                        onClick={() => setTargetVersion(version)}
                      >
                        {activatingVersionId === version.id ? (
                          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        ) : (
                          <History className="mr-2 h-4 w-4" />
                        )}
                        {t('settings.version_activate')}
                      </Button>
                    ) : null}
                  </div>
                  <details className="mt-3 text-sm">
                    <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
                      {t('settings.version_details')}
                    </summary>
                    <div className="mt-3 grid gap-3 md:grid-cols-2">
                      <div className="min-w-0 space-y-1">
                        <div className="text-xs font-medium text-muted-foreground">
                          {t('settings.version_prompt_snapshot')}
                        </div>
                        <pre className="max-h-40 overflow-auto break-words rounded border border-border bg-muted p-2 text-xs whitespace-pre-wrap">
                          {version.promptTemplate || t('settings.version_uses_default_prompt')}
                        </pre>
                      </div>
                      <div className="min-w-0 space-y-1">
                        <div className="text-xs font-medium text-muted-foreground">
                          {t('settings.version_dimensions_snapshot')}
                        </div>
                        <div className="flex flex-wrap gap-1 rounded border border-border bg-muted p-2">
                          {version.dimensions.length > 0 ? (
                            version.dimensions.map((dimension) => (
                              <span
                                key={dimension.name}
                                className="rounded border border-border bg-background px-2 py-0.5 font-mono text-xs"
                              >
                                {dimension.name}
                              </span>
                            ))
                          ) : (
                            <span className="text-xs text-muted-foreground">
                              {t('settings.version_no_dimensions')}
                            </span>
                          )}
                        </div>
                        {version.warnings.length > 0 ? (
                          <div className="flex flex-wrap gap-1 pt-1">
                            {version.warnings.map((warning) => (
                              <span
                                key={warning}
                                className="rounded border border-border px-2 py-0.5 font-mono text-xs"
                              >
                                {warning}
                              </span>
                            ))}
                          </div>
                        ) : null}
                        {version.promptFingerprint ? (
                          <div className="pt-1 text-xs text-muted-foreground">
                            <span className="font-medium">
                              {t('settings.version_prompt_fingerprint')}:
                            </span>{' '}
                            <span className="break-all font-mono">{version.promptFingerprint}</span>
                          </div>
                        ) : null}
                        {version.schemaFingerprint ? (
                          <div className="text-xs text-muted-foreground">
                            <span className="font-medium">
                              {t('settings.version_schema_fingerprint')}:
                            </span>{' '}
                            <span className="break-all font-mono">{version.schemaFingerprint}</span>
                          </div>
                        ) : null}
                      </div>
                    </div>
                  </details>
                </div>
              )
            })}
            {versionHistory.hasNextPage ? (
              <Button
                type="button"
                variant="outline"
                className="w-full"
                onClick={() => versionHistory.fetchNextPage()}
                disabled={versionHistory.isFetchingNextPage}
              >
                {versionHistory.isFetchingNextPage ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : (
                  <History className="mr-2 h-4 w-4" />
                )}
                {t('settings.version_history_load_more')}
              </Button>
            ) : null}
          </div>
        )}
        <Dialog open={targetVersion !== null} onOpenChange={(open) => !open && closeDialog()}>
          <DialogContent className="sm:max-w-3xl">
            <DialogHeader>
              <DialogTitle>{t('settings.version_activate_dialog_title')}</DialogTitle>
              <DialogDescription>
                {t('settings.version_activate_dialog_body', {
                  version: targetVersion?.promptVersion ?? '',
                })}
              </DialogDescription>
            </DialogHeader>
            {targetVersion ? (
              <PromptVersionDiff activeVersion={activeVersion} targetVersion={targetVersion} />
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={closeDialog}>
                {t('common.cancel')}
              </Button>
              <Button
                type="button"
                onClick={confirmActivate}
                disabled={activating}
                data-testid="prompt-version-activate-confirm"
              >
                {targetVersion && activatingVersionId === targetVersion.id ? (
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                ) : null}
                {t('settings.version_activate_confirm')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

function mergeVersions(primary: EnrichPromptVersion[], fallback: EnrichPromptVersion[]) {
  const seen = new Set<string>()
  const out: EnrichPromptVersion[] = []
  for (const version of [...primary, ...fallback]) {
    if (!version.id || seen.has(version.id)) continue
    seen.add(version.id)
    out.push(version)
  }
  return out
}

function PromptVersionDiff({
  activeVersion,
  targetVersion,
}: {
  activeVersion?: EnrichPromptVersion
  targetVersion: EnrichPromptVersion
}) {
  const { t } = useTranslation()
  const promptChanged = activeVersion ? !samePromptSnapshot(activeVersion, targetVersion) : true
  const dimensions = activeVersion
    ? dimensionDiff(activeVersion.dimensions ?? [], targetVersion.dimensions ?? [])
    : {
        added: targetVersion.dimensions.map((dimension) => dimension.name),
        removed: [],
        kept: [],
      }
  const policyChanges = activeVersion
    ? policyConfigChanges(activeVersion.policyConfig, targetVersion.policyConfig)
    : []
  const warningChanged = activeVersion
    ? activeVersion.warnings.length !== targetVersion.warnings.length
    : targetVersion.warnings.length > 0
  const impact = [
    promptChanged ? t('settings.version_diff_prompt_changed') : '',
    dimensions.added.length || dimensions.removed.length
      ? t('settings.version_diff_dimensions_changed')
      : '',
    policyChanges.length ? t('settings.version_diff_policy_changed') : '',
    warningChanged ? t('settings.version_diff_warnings_changed') : '',
  ].filter(Boolean)
  return (
    <div className="space-y-4">
      <div className="grid gap-3 md:grid-cols-2">
        <VersionSnapshotCard
          title={t('settings.version_diff_current')}
          version={activeVersion}
          emptyText={t('settings.version_diff_no_current')}
        />
        <VersionSnapshotCard
          title={t('settings.version_diff_target')}
          version={targetVersion}
          emptyText={t('settings.version_diff_no_current')}
        />
      </div>
      <div className="rounded-md border border-border bg-muted/30 p-3 text-sm">
        <div className="font-medium">{t('settings.version_diff_impact_title')}</div>
        {impact.length > 0 ? (
          <ul className="mt-2 space-y-1 text-muted-foreground">
            {impact.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        ) : (
          <p className="mt-2 text-muted-foreground">{t('settings.version_diff_no_changes')}</p>
        )}
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <DiffBlock
          title={t('settings.version_diff_prompt')}
          value={
            promptChanged
              ? t('settings.version_diff_changed')
              : t('settings.version_diff_unchanged')
          }
        />
        <DiffBlock
          title={t('settings.version_diff_dimensions')}
          value={t('settings.version_diff_dimensions_value', {
            added: dimensions.added.length,
            removed: dimensions.removed.length,
            kept: dimensions.kept.length,
          })}
          detail={[
            dimensions.added.length
              ? t('settings.version_diff_added', { values: dimensions.added.join(', ') })
              : '',
            dimensions.removed.length
              ? t('settings.version_diff_removed', { values: dimensions.removed.join(', ') })
              : '',
          ]
            .filter(Boolean)
            .join('\n')}
        />
        <DiffBlock
          title={t('settings.version_diff_policy')}
          value={
            policyChanges.length
              ? t('settings.version_diff_policy_value', { count: policyChanges.length })
              : t('settings.version_diff_unchanged')
          }
          detail={policyChanges.join('\n')}
        />
      </div>
    </div>
  )
}

function VersionSnapshotCard({
  emptyText,
  title,
  version,
}: {
  emptyText: string
  title: string
  version?: EnrichPromptVersion
}) {
  const { t } = useTranslation()
  if (!version) {
    return (
      <div className="rounded-md border border-dashed border-border p-3 text-sm text-muted-foreground">
        {emptyText}
      </div>
    )
  }
  return (
    <div className="min-w-0 rounded-md border border-border p-3 text-sm">
      <div className="mb-2 font-medium">{title}</div>
      <div className="max-w-full break-all font-mono text-xs">{version.promptVersion}</div>
      <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
        <span>
          {version.hasTemplate
            ? t('settings.version_custom_template')
            : t('settings.version_default_template')}
        </span>
        <span>{t('settings.version_dimensions', { count: version.dimensionsCount })}</span>
        <span>{formatVersionTime(version.createdAt)}</span>
      </div>
    </div>
  )
}

function DiffBlock({ detail, title, value }: { detail?: string; title: string; value: string }) {
  return (
    <div className="min-w-0 rounded-md border border-border p-3 text-sm">
      <div className="text-xs font-medium text-muted-foreground">{title}</div>
      <div className="mt-1">{value}</div>
      {detail ? (
        <pre className="mt-2 whitespace-pre-wrap text-xs text-muted-foreground">{detail}</pre>
      ) : null}
    </div>
  )
}

function samePromptSnapshot(a: EnrichPromptVersion, b: EnrichPromptVersion) {
  return (a.promptTemplate ?? '') === (b.promptTemplate ?? '')
}

function sameDimensionsSnapshot(a: EnrichPromptVersion, b: EnrichPromptVersion) {
  return JSON.stringify(a.dimensions ?? []) === JSON.stringify(b.dimensions ?? [])
}

function dimensionDiff(current: Dimension[], target: Dimension[]) {
  const currentNames = new Set(current.map((dimension) => dimension.name))
  const targetNames = new Set(target.map((dimension) => dimension.name))
  const added = [...targetNames].filter((name) => !currentNames.has(name)).sort()
  const removed = [...currentNames].filter((name) => !targetNames.has(name)).sort()
  const kept = [...targetNames].filter((name) => currentNames.has(name)).sort()
  return { added, removed, kept }
}

function policyConfigChanges(
  current?: EnrichPromptPolicyConfig,
  target?: EnrichPromptPolicyConfig,
) {
  const before = normalizePolicyConfig(current)
  const after = normalizePolicyConfig(target)
  const fields: Array<keyof EnrichPromptPolicyConfig> = [
    'outputLanguagePolicy',
    'titleMaxChars',
    'rationaleMaxChars',
    'displayFieldsRequired',
    'tone',
    'domainGuidance',
  ]
  return fields
    .filter((field) => before[field] !== after[field])
    .map((field) => `${field}: ${String(before[field] ?? '')} -> ${String(after[field] ?? '')}`)
}

function formatVersionTime(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function promptWarningText(
  t: ReturnType<typeof useTranslation>['t'],
  warning: { code: string; message: string },
) {
  switch (warning.code) {
    case 'legacy_variable_modules':
      return t('settings.policy_warning_legacy_modules')
    case 'missing_dimensions':
      return t('settings.policy_warning_missing_dimensions')
    case 'unknown_variable':
      return t('settings.policy_warning_unknown_variable')
    default:
      return warning.message
  }
}

function SettingsSectionContent({ section }: { section: SettingsSection }) {
  return (
    <section className="min-w-0">
      {section === 'guard_policies' ? <GuardPoliciesPage /> : null}
      {section === 'inbound_sources' ? <InboundSourcesPage /> : null}
      {section === 'notify_targets' ? <NotifyTargetsPage /> : null}
      {section === 'digest_subscription' ? <DigestSubscriptionPage /> : null}
      {section === 'api_keys' ? <ApiKeysPage /> : null}
      {section === 'gdpr' ? <GDPRPage /> : null}
      {section === 'audit_log' ? <AuditLogPage /> : null}
      {section === 'tags' ? <TagsPage /> : null}
      {section === 'workflow' ? <WorkflowSettingsPage /> : null}
      {section === 'members' ? <MembersPage /> : null}
    </section>
  )
}

function SettingsSidebar({
  active,
  onSelect,
}: {
  active: SettingsSection
  onSelect: (section: SettingsSection) => void
}) {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const areas = [
    {
      section: 'classification',
      icon: Bot,
      title: t('settings.areas.classification.title'),
      body: t('settings.areas.classification.body'),
    },
    {
      section: 'enrichment_runtime',
      icon: Gauge,
      title: t('settings.areas.enrichment_runtime.title'),
      body: t('settings.areas.enrichment_runtime.body'),
      permission: 'settings:enrichment_runtime:view',
    },
    {
      section: 'guard_policies',
      icon: ShieldCheck,
      title: t('settings.areas.guardrails.title'),
      body: t('settings.areas.guardrails.body'),
    },
    {
      section: 'inbound_sources',
      icon: Mailbox,
      title: t('settings.areas.inbound.title'),
      body: t('settings.areas.inbound.body'),
    },
    {
      section: 'notify_targets',
      icon: Bell,
      title: t('settings.areas.notify.title'),
      body: t('settings.areas.notify.body'),
    },
    {
      section: 'digest_subscription',
      icon: Newspaper,
      title: t('settings.areas.digest.title'),
      body: t('settings.areas.digest.body'),
    },
    {
      section: 'api_keys',
      icon: KeyRound,
      title: t('settings.areas.api_keys.title'),
      body: t('settings.areas.api_keys.body'),
      permission: 'settings:api_keys:view',
    },
    {
      section: 'gdpr',
      icon: Scale,
      title: t('settings.areas.gdpr.title'),
      body: t('settings.areas.gdpr.body'),
      permission: 'settings:gdpr:view',
    },
    {
      section: 'audit_log',
      icon: History,
      title: t('settings.areas.audit_log.title'),
      body: t('settings.areas.audit_log.body'),
      permission: 'settings:audit_log:view',
    },
    {
      section: 'tags',
      icon: Tags,
      title: t('settings.areas.tags.title'),
      body: t('settings.areas.tags.body'),
      permission: 'settings:tags:view',
    },
    {
      section: 'workflow',
      icon: GitBranch,
      title: t('settings.areas.workflow.title'),
      body: t('settings.areas.workflow.body'),
      permission: 'settings:workflow:view',
    },
    {
      section: 'members',
      icon: Users,
      title: t('settings.areas.members.title'),
      body: t('settings.areas.members.body'),
      permission: 'settings:members:view',
    },
  ] satisfies Array<{
    body: string
    icon: typeof Bot
    permission?: Permission
    section: SettingsSection
    title: string
  }>
  return (
    <aside className="min-w-0 lg:sticky lg:top-20 lg:self-start">
      <nav
        aria-label={t('settings.sidebar_label')}
        className="flex min-w-0 max-w-full gap-1 overflow-x-auto border-b border-border pb-2 lg:block lg:space-y-1 lg:overflow-visible lg:border-r lg:border-b-0 lg:pr-4 lg:pb-0"
      >
        {areas
          .filter((area) => !area.permission || can(area.permission))
          .map((area) => (
            <button
              key={area.section}
              type="button"
              className={
                active === area.section
                  ? 'flex min-w-48 shrink-0 items-start gap-3 rounded-md bg-primary/5 px-3 py-2 text-left text-sm ring-1 ring-primary/20 lg:min-w-0 lg:shrink'
                  : 'flex min-w-48 shrink-0 items-start gap-3 rounded-md px-3 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground lg:min-w-0 lg:shrink'
              }
              onClick={() => onSelect(area.section)}
            >
              <area.icon className="mt-0.5 h-4 w-4 shrink-0" />
              <div className="min-w-0">
                <div className="font-medium">{area.title}</div>
                <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{area.body}</p>
              </div>
            </button>
          ))}
      </nav>
    </aside>
  )
}

function sectionVisible(section: SettingsSection, can: (permission: Permission) => boolean) {
  switch (section) {
    case 'classification':
      return can('settings:enrich_config:view')
    case 'enrichment_runtime':
      return can('settings:enrichment_runtime:view')
    case 'guard_policies':
      return can('settings:guard_policies:view')
    case 'inbound_sources':
      return can('settings:inbound:view')
    case 'notify_targets':
      return can('settings:notify_targets:view')
    case 'digest_subscription':
      return can('settings:digest:view')
    case 'api_keys':
      return can('settings:api_keys:view')
    case 'gdpr':
      return can('settings:gdpr:view')
    case 'audit_log':
      return can('settings:audit_log:view')
    case 'tags':
      return can('settings:tags:view')
    case 'workflow':
      return can('settings:workflow:view')
    case 'members':
      return can('settings:members:view')
    default:
      return false
  }
}
