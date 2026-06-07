import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Loader2, RotateCcw, Sparkles } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import type { Dimension } from '@/proto/attune/v1/common'

export const Route = createFileRoute('/_authed/settings')({
  component: SettingsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(enrichConfigQuery()),
})

function SettingsPage() {
  const { t } = useTranslation()
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

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.settings')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('settings.subtitle')}</p>
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
              onChange={(e) => setPrompt(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
          </div>
          <Button type="button" variant="outline" size="sm" onClick={handleRestoreDefault}>
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
          <DimensionsEditor value={dimensions} onChange={setDimensions} />
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
              onChange={(e) => setSample(e.target.value)}
            />
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={handlePreview}
            disabled={preview.isPending}
          >
            {preview.isPending ? (
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
        <Button onClick={handleSave} disabled={save.isPending}>
          {save.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {t('common.save')}
        </Button>
      </div>
    </div>
  )
}
