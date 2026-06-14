import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import {
  Bell,
  Bot,
  KeyRound,
  Loader2,
  Mailbox,
  Newspaper,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  Tags,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'
import { DigestSubscriptionPage } from '@/features/digest-subscription/components/digest-subscription-page'
import { GuardPoliciesPage } from '@/features/guard-policies/components/guard-policies-page'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import { TagsPage } from '@/features/tags/components/tags-page'
import type { Dimension } from '@/proto/attune/v1/common'

type SettingsSection =
  | 'classification'
  | 'guard_policies'
  | 'inbound_sources'
  | 'notify_targets'
  | 'digest_subscription'
  | 'api_keys'
  | 'tags'

const SETTINGS_SECTIONS: SettingsSection[] = [
  'classification',
  'guard_policies',
  'inbound_sources',
  'notify_targets',
  'digest_subscription',
  'api_keys',
  'tags',
]

export const Route = createFileRoute('/_authed/settings')({
  validateSearch: (search): { section: SettingsSection } => {
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
  const cfg = useQuery(enrichConfigQuery())
  const save = useUpdateEnrichConfig()
  const preview = usePreviewEnrichPrompt()

  const [prompt, setPrompt] = useState('')
  const [dimensions, setDimensions] = useState<Dimension[]>([])
  const [sample, setSample] = useState('')
  const [previewText, setPreviewText] = useState('')

  useEffect(() => {
    if (!cfg.data) return
    setPrompt(cfg.data.promptTemplate ?? cfg.data.defaultPromptTemplate)
    setDimensions(cfg.data.dimensions ?? [])
  }, [cfg.data])

  const handleRestoreDefault = () => {
    if (cfg.data?.defaultPromptTemplate) {
      setPrompt(cfg.data.defaultPromptTemplate)
    }
  }

  const handleSave = () => {
    const defaultTmpl = cfg.data?.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    save.mutate(
      {
        dimensions,
        promptTemplate: isDefaultPrompt ? undefined : prompt,
      },
      {
        onSuccess: () => toast.success(t('settings.saved')),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handlePreview = () => {
    const content = sample.trim() || t('settings.preview_sample_default')
    preview.mutate(
      { sampleContent: content },
      {
        onSuccess: (resp) => setPreviewText(resp.renderedPrompt),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  if (section === 'classification' && cfg.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  const constrained = dimensions.some((d) => d.taxonomy.length > 0)
  const modeLabel = constrained ? t('settings.mode_constrained') : t('settings.mode_freeform')
  const setSection = (next: SettingsSection) => {
    void navigate({
      to: '/settings',
      search: { section: next },
      replace: true,
    })
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.settings')}</h1>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('settings.subtitle')}</p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[240px_minmax(0,1fr)]">
        <SettingsSidebar active={section} onSelect={setSection} />

        {section === 'classification' ? (
          <ClassificationSettings
            dimensions={dimensions}
            modeLabel={modeLabel}
            previewPending={preview.isPending}
            previewText={previewText}
            prompt={prompt}
            sample={sample}
            savePending={save.isPending}
            onDimensionsChange={setDimensions}
            onPreview={handlePreview}
            onPromptChange={setPrompt}
            onRestoreDefault={handleRestoreDefault}
            onSampleChange={setSample}
            onSave={handleSave}
          />
        ) : (
          <SettingsSectionContent section={section} />
        )}
      </div>
    </div>
  )
}

function ClassificationSettings({
  dimensions,
  modeLabel,
  onDimensionsChange,
  onPreview,
  onPromptChange,
  onRestoreDefault,
  onSampleChange,
  onSave,
  previewPending,
  previewText,
  prompt,
  sample,
  savePending,
}: {
  dimensions: Dimension[]
  modeLabel: string
  onDimensionsChange: (dimensions: Dimension[]) => void
  onPreview: () => void
  onPromptChange: (prompt: string) => void
  onRestoreDefault: () => void
  onSampleChange: (sample: string) => void
  onSave: () => void
  previewPending: boolean
  previewText: string
  prompt: string
  sample: string
  savePending: boolean
}) {
  const { t } = useTranslation()
  return (
    <section className="min-w-0 space-y-6">
      <div>
        <h2 className="text-lg font-semibold tracking-tight">
          {t('settings.classification_title')}
        </h2>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
          {t('settings.classification_help')}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.prompt_title')}</CardTitle>
          <CardDescription>
            {t('settings.prompt_help')} · {modeLabel}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="prompt">{t('settings.prompt_label')}</Label>
            <textarea
              id="prompt"
              className="min-h-[220px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              value={prompt}
              onChange={(e) => onPromptChange(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={onRestoreDefault}>
            <RotateCcw className="mr-2 h-3.5 w-3.5" />
            {t('settings.restore_default')}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.dimensions_title')}</CardTitle>
          <CardDescription>{t('settings.dimensions_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          <DimensionsEditor value={dimensions} onChange={onDimensionsChange} />
        </CardContent>
      </Card>

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

      <div className="flex justify-end">
        <Button onClick={onSave} disabled={savePending}>
          {savePending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {t('common.save')}
        </Button>
      </div>
    </section>
  )
}

function SettingsSectionContent({ section }: { section: SettingsSection }) {
  return (
    <section className="min-w-0">
      {section === 'guard_policies' ? <GuardPoliciesPage /> : null}
      {section === 'inbound_sources' ? <InboundSourcesPage /> : null}
      {section === 'notify_targets' ? <NotifyTargetsPage /> : null}
      {section === 'digest_subscription' ? <DigestSubscriptionPage /> : null}
      {section === 'api_keys' ? <ApiKeysPage /> : null}
      {section === 'tags' ? <TagsPage /> : null}
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
  const areas = [
    {
      section: 'classification',
      icon: Bot,
      title: t('settings.areas.classification.title'),
      body: t('settings.areas.classification.body'),
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
    },
    {
      section: 'tags',
      icon: Tags,
      title: t('settings.areas.tags.title'),
      body: t('settings.areas.tags.body'),
    },
  ] satisfies Array<{
    body: string
    icon: typeof Bot
    section: SettingsSection
    title: string
  }>
  return (
    <aside className="lg:sticky lg:top-20 lg:self-start">
      <nav
        aria-label={t('settings.sidebar_label')}
        className="flex gap-1 overflow-x-auto border-b border-border pb-2 lg:block lg:space-y-1 lg:overflow-visible lg:border-r lg:border-b-0 lg:pr-4 lg:pb-0"
      >
        {areas.map((area) => (
          <button
            key={area.section}
            type="button"
            className={
              active === area.section
                ? 'flex min-w-48 items-start gap-3 rounded-md bg-primary/5 px-3 py-2 text-left text-sm ring-1 ring-primary/20 lg:min-w-0'
                : 'flex min-w-48 items-start gap-3 rounded-md px-3 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground lg:min-w-0'
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
