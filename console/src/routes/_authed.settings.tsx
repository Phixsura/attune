import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { Loader2, RotateCcw, Sparkles } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import { ModuleMode } from '@/proto/attune/v1/enrich_config'

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
  const [modules, setModules] = useState<string[]>([])
  const [moduleInput, setModuleInput] = useState('')
  const [sample, setSample] = useState('')
  const [previewText, setPreviewText] = useState('')

  useEffect(() => {
    if (!cfg.data) return
    setPrompt(cfg.data.promptTemplate ?? cfg.data.defaultPromptTemplate)
    setModules(cfg.data.modules ?? [])
  }, [cfg.data])

  const handleRestoreDefault = () => {
    if (cfg.data?.defaultPromptTemplate) {
      setPrompt(cfg.data.defaultPromptTemplate)
    }
  }

  const handleAddModule = () => {
    const name = moduleInput.trim()
    if (!name) return
    if (modules.some((m) => m.toLowerCase() === name.toLowerCase())) {
      setModuleInput('')
      return
    }
    setModules((prev) => [...prev, name])
    setModuleInput('')
  }

  const handleSave = () => {
    const defaultTmpl = cfg.data?.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    const body = {
      modules,
      promptTemplate: isDefaultPrompt ? undefined : prompt,
    }
    save.mutate(body, {
      onSuccess: () => toast.success(t('settings.saved')),
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })
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

  const modeLabel =
    cfg.data?.moduleMode === ModuleMode.MODULE_MODE_CONSTRAINED
      ? t('settings.mode_constrained')
      : t('settings.mode_freeform')

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
          <CardTitle>{t('settings.modules_title')}</CardTitle>
          <CardDescription>{t('settings.modules_help')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap gap-2">
            {modules.map((m) => (
              <span
                key={m}
                className="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-3 py-1 text-sm"
              >
                {m}
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground"
                  onClick={() => setModules((prev) => prev.filter((x) => x !== m))}
                  aria-label={t('settings.remove_module', { name: m })}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
          <div className="flex gap-2">
            <Input
              placeholder={t('settings.module_placeholder')}
              value={moduleInput}
              onChange={(e) => setModuleInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  handleAddModule()
                }
              }}
            />
            <Button type="button" variant="secondary" onClick={handleAddModule}>
              {t('settings.add_module')}
            </Button>
          </div>
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
