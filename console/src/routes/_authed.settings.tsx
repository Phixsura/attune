import { useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import {
  Bell,
  Bot,
  GitBranch,
  History,
  KeyRound,
  Loader2,
  Mailbox,
  Newspaper,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  Tags,
  Users,
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
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import { DigestSubscriptionPage } from '@/features/digest-subscription/components/digest-subscription-page'
import { GuardPoliciesPage } from '@/features/guard-policies/components/guard-policies-page'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { MembersPage } from '@/features/members/components/members-page'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { meQuery } from '@/features/session/api/get-me'
import { type Permission, usePermissions } from '@/features/session/hooks/use-permissions'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import { TagsPage } from '@/features/tags/components/tags-page'
import { WorkflowSettingsPage } from '@/features/workflow/components/workflow-settings-page'
import type { Dimension } from '@/proto/attune/v1/common'

type SettingsSection =
  | 'classification'
  | 'guard_policies'
  | 'inbound_sources'
  | 'notify_targets'
  | 'digest_subscription'
  | 'api_keys'
  | 'audit_log'
  | 'tags'
  | 'workflow'
  | 'members'

const SETTINGS_SECTIONS: SettingsSection[] = [
  'classification',
  'guard_policies',
  'inbound_sources',
  'notify_targets',
  'digest_subscription',
  'api_keys',
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
          <SettingsSectionContent section={activeSection} />
        )}
      </div>
    </div>
  )
}

function ClassificationSettings({
  canEdit,
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
  canEdit: boolean
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
              className="min-h-[220px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              value={prompt}
              onChange={(e) => onPromptChange(e.target.value)}
              disabled={!canEdit}
            />
            <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
          </div>
          {canEdit && (
            <Button type="button" variant="outline" size="sm" onClick={onRestoreDefault}>
              <RotateCcw className="mr-2 h-3.5 w-3.5" />
              {t('settings.restore_default')}
            </Button>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('settings.dimensions_title')}</CardTitle>
          <CardDescription>{t('settings.dimensions_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          <DimensionsEditor value={dimensions} onChange={onDimensionsChange} disabled={!canEdit} />
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

      {canEdit && (
        <div className="flex justify-end">
          <Button onClick={onSave} disabled={savePending}>
            {savePending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {t('common.save')}
          </Button>
        </div>
      )}
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
    <aside className="lg:sticky lg:top-20 lg:self-start">
      <nav
        aria-label={t('settings.sidebar_label')}
        className="flex gap-1 overflow-x-auto border-b border-border pb-2 lg:block lg:space-y-1 lg:overflow-visible lg:border-r lg:border-b-0 lg:pr-4 lg:pb-0"
      >
        {areas
          .filter((area) => !area.permission || can(area.permission))
          .map((area) => (
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

function sectionVisible(section: SettingsSection, can: (permission: Permission) => boolean) {
  switch (section) {
    case 'classification':
      return can('settings:enrich_config:view')
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
