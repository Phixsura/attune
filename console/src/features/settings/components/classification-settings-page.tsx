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
import { UnsavedChangesDialog } from '@/components/unsaved-changes-dialog'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { type EnrichConfig, enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usePreviewEnrichPrompt } from '@/features/settings/api/preview-enrich-prompt'
import { useUpdateEnrichConfig } from '@/features/settings/api/update-enrich-config'
import { SuggestedValuesPanel } from '@/features/settings/components/suggested-values-panel'
import { readDraft, useDraftGuard } from '@/hooks/use-draft-guard'
import {
  type EditableDimension,
  markPersisted,
  mergePromotedValue,
  seedDimensions,
  toWireDimensions,
} from '@/lib/editable-rows'

export function ClassificationSettingsPage() {
  const { t } = useTranslation()
  // The draft is an operator-edited form, not a live feed: don't let a
  // background tab-refocus refetch fire while editing (the in-progress draft
  // lives in <ClassificationSettingsForm> state and the form is seeded once,
  // so a refetch can't silently clobber it either way — this just avoids the
  // pointless request).
  const cfg = useQuery({ ...enrichConfigQuery(), refetchOnWindowFocus: false })

  if (cfg.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  if (!cfg.data) {
    // Settled with no data (fetch error) — surface it with a retry instead of
    // an indefinite spinner.
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-sm text-muted-foreground">
        <p>{t('common.error')}</p>
        <Button type="button" variant="outline" size="sm" onClick={() => cfg.refetch()}>
          {t('common.retry')}
        </Button>
      </div>
    )
  }

  // Gated child: only mounted once `cfg.data` is defined, so the form seeds its
  // edit model exactly once (in a useState initializer) from a non-null
  // snapshot — never via a `useEffect([cfg.data])` that re-fires on refetch.
  return <ClassificationSettingsForm initial={cfg.data} />
}

function ClassificationSettingsForm({ initial }: { initial: EnrichConfig }) {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canEdit = can('settings:enrich_config:edit')
  const save = useUpdateEnrichConfig()
  const preview = usePreviewEnrichPrompt()

  const storedDraft = readDraft<{ prompt: string; rows: EditableDimension[] }>(
    'classification-settings',
  )
  const [restoredFromStorage] = useState(() => storedDraft !== null)
  const [prompt, setPrompt] = useState(
    () => storedDraft?.prompt ?? initial.promptTemplate ?? initial.defaultPromptTemplate,
  )
  const [rows, setRows] = useState<EditableDimension[]>(
    () => storedDraft?.rows ?? seedDimensions(initial.dimensions ?? []),
  )
  const [sample, setSample] = useState('')
  const [previewText, setPreviewText] = useState('')
  const [touched, setTouched] = useState(() => restoredFromStorage)
  const [discardOpen, setDiscardOpen] = useState(false)

  const guard = useDraftGuard({
    storageKey: 'classification-settings',
    draft: { prompt, rows },
    dirty: touched,
    disabled: !canEdit,
  })

  const updatePrompt = (value: string) => {
    setPrompt(value)
    setTouched(true)
  }

  const updateRows = (
    value: EditableDimension[] | ((prev: EditableDimension[]) => EditableDimension[]),
  ) => {
    setRows(value)
    setTouched(true)
  }

  // biome-ignore lint/correctness/useExhaustiveDependencies: fire once on mount
  useEffect(() => {
    if (!restoredFromStorage) return
    toast.info(t('draft.recovered'), {
      duration: 8000,
      action: {
        label: t('draft.recovered_discard'),
        onClick: () => {
          setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
          setRows(seedDimensions(initial.dimensions ?? []))
          setTouched(false)
          guard.clearDraft()
        },
      },
    })
  }, [])

  const handleRestoreDefault = () => {
    if (initial.defaultPromptTemplate) updatePrompt(initial.defaultPromptTemplate)
  }

  const handleDiscard = () => {
    setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
    setRows(seedDimensions(initial.dimensions ?? []))
    setTouched(false)
    guard.clearDraft()
    setDiscardOpen(false)
  }

  const handleSave = () => {
    const defaultTmpl = initial.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    // Snapshot the identities being submitted, so on success we lock exactly
    // those rows (and taxonomy rows) — and leave anything the operator adds
    // during the in-flight save still editable.
    const sentDimKeys = new Set(rows.map((r) => r._key))
    const sentTaxKeys = new Set(rows.flatMap((r) => r.taxonomy.map((tx) => tx._key)))
    save.mutate(
      {
        dimensions: toWireDimensions(rows),
        promptTemplate: isDefaultPrompt ? undefined : prompt,
      },
      {
        onSuccess: () => {
          setRows((prev) => markPersisted(prev, sentDimKeys, sentTaxKeys))
          guard.clearDraft()
          setTouched(false)
          toast.success(t('settings.saved'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handlePreview = () => {
    const content = sample.trim() || t('settings.preview_sample_default')
    const defaultTmpl = initial.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    preview.mutate(
      {
        sampleContent: content,
        promptTemplate: isDefaultPrompt ? undefined : prompt,
        dimensions: toWireDimensions(rows),
        useDraftConfig: true,
      },
      {
        onSuccess: (resp) => setPreviewText(resp.renderedPrompt),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const constrained = rows.some((d) => d.taxonomy.length > 0)
  const modeLabel = constrained ? t('settings.mode_constrained') : t('settings.mode_freeform')
  const constrainedCount = rows.filter((d) => d.taxonomy.length > 0).length
  const urgentEnabledCount = rows.filter((d) => d.urgentSet?.length > 0).length

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
              value={String(rows.length)}
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
                onChange={(e) => updatePrompt(e.target.value)}
                disabled={!canEdit}
              />
              <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={handleRestoreDefault}
                disabled={!canEdit}
              >
                {t('settings.restore_default')}
              </Button>
              <Button type="button" onClick={handleSave} disabled={!canEdit || save.isPending}>
                {save.isPending ? t('app.loading') : t('common.save')}
              </Button>
              {touched && (
                <Button type="button" variant="ghost" onClick={() => setDiscardOpen(true)}>
                  {t('draft.discard_changes')}
                </Button>
              )}
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
          {/* Lock the editor while a save is in flight: editing a just-submitted
              row's identifier mid-save would, on success, lock it to a value the
              server never received. */}
          <DimensionsEditor
            value={rows}
            onChange={updateRows}
            disabled={!canEdit || save.isPending}
          />
        </CardContent>
      </Card>

      <SuggestedValuesPanel
        canEdit={canEdit}
        onPromoted={(dim, value, displayName) =>
          updateRows((prev) => mergePromotedValue(prev, dim, value, displayName))
        }
      />

      <Dialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <DialogContent showCloseButton={false} role="alertdialog">
          <DialogHeader>
            <DialogTitle>{t('draft.discard_confirm_title')}</DialogTitle>
            <DialogDescription>{t('draft.discard_confirm_body')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDiscardOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="button" variant="destructive" onClick={handleDiscard}>
              {t('draft.discard_changes')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <UnsavedChangesDialog
        open={guard.dialogOpen}
        onConfirmLeave={guard.confirmLeave}
        onCancelLeave={guard.cancelLeave}
      />
    </section>
  )
}
