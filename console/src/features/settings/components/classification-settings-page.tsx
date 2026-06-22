import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import { SuggestedValuesPanel } from '@/features/settings/components/suggested-values-panel'
import type { Dimension } from '@/proto/attune/v1/common'

export function ClassificationSettingsPage() {
  const { t } = useTranslation()
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
    const defaultTmpl = cfg.data?.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    preview.mutate(
      {
        sampleContent: content,
        promptTemplate: isDefaultPrompt ? undefined : prompt,
        dimensions,
        useDraftConfig: true,
      },
      {
        onSuccess: (resp) => setPreviewText(resp.renderedPrompt),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  if (cfg.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  const constrained = dimensions.some((d) => d.taxonomy.length > 0)
  const modeLabel = constrained ? t('settings.mode_constrained') : t('settings.mode_freeform')
  const constrainedCount = dimensions.filter((d) => d.taxonomy.length > 0).length
  const urgentEnabledCount = dimensions.filter((d) => d.urgentSet?.length > 0).length

  return (
    <section className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.configuration')}
        title={t('settings.classification_title')}
        subtitle={t('settings.classification_help')}
        metrics={
          <>
            <PageHeroMetric
              label={t('settings.summary.dimensions')}
              value={String(dimensions.length)}
              hint={t('settings.summary.dimensions_hint')}
            />
            <PageHeroMetric
              label={t('settings.summary.constrained')}
              value={String(constrainedCount)}
              hint={t('settings.summary.constrained_hint')}
            />
            <PageHeroMetric
              label={t('settings.summary.urgent')}
              value={String(urgentEnabledCount)}
              hint={t('settings.summary.urgent_hint')}
            />
            <PageHeroMetric
              label={t('settings.summary.mode')}
              value={modeLabel}
              hint={t('settings.summary.mode_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(22rem,0.85fr)]">
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('settings.prompt_title')}</CardTitle>
            <CardDescription>
              {t('settings.prompt_help')} · {modeLabel}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            <div className="space-y-2">
              <Label htmlFor="prompt">{t('settings.prompt_label')}</Label>
              <textarea
                id="prompt"
                className="min-h-[320px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm leading-7 outline-none ring-offset-background placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                disabled={!can('settings:enrich_config:edit')}
              />
              <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={handleRestoreDefault}
                disabled={!can('settings:enrich_config:edit')}
              >
                {t('settings.restore_default')}
              </Button>
              <Button
                type="button"
                onClick={handleSave}
                disabled={!can('settings:enrich_config:edit') || save.isPending}
              >
                {save.isPending ? t('app.loading') : t('common.save')}
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('settings.preview_title')}</CardTitle>
            <CardDescription>{t('settings.preview_help')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 pt-6">
            <div className="space-y-2">
              <Label htmlFor="sample">{t('settings.preview_sample_label')}</Label>
              <Input
                id="sample"
                value={sample}
                onChange={(e) => setSample(e.target.value)}
                placeholder={t('settings.preview_sample_placeholder')}
              />
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={handlePreview}
                disabled={preview.isPending}
              >
                {preview.isPending ? t('app.loading') : t('settings.preview_button')}
              </Button>
            </div>
            {previewText ? (
              <pre className="max-h-[26rem] overflow-x-auto rounded-[1rem] border border-border/70 bg-muted/25 p-4 text-xs leading-6 whitespace-pre-wrap">
                {previewText}
              </pre>
            ) : (
              <EmptyState
                title={t('settings.preview_empty_title')}
                description={t('settings.preview_empty_body')}
                className="rounded-[1rem] border border-dashed border-border/70 bg-muted/10 py-14"
              />
            )}
          </CardContent>
        </Card>
      </div>

      <Card className="border-border/70 shadow-none">
        <CardHeader className="border-b border-border/60 bg-muted/15">
          <CardTitle>{t('settings.dimensions_title')}</CardTitle>
          <CardDescription>{t('settings.dimensions_help')}</CardDescription>
        </CardHeader>
        <CardContent className="pt-6">
          <DimensionsEditor
            value={dimensions}
            onChange={setDimensions}
            disabled={!can('settings:enrich_config:edit')}
          />
        </CardContent>
      </Card>

      <SuggestedValuesPanel canEdit={can('settings:enrich_config:edit')} />
    </section>
  )
}
