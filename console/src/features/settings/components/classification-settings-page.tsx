import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { DraftBanner } from '@/components/draft-banner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { SaveStatus } from '@/components/save-status'
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
import { clearStoredDraft, readDraft, useDraftGuard } from '@/hooks/use-draft-guard'
import { useKeyboardSave } from '@/hooks/use-keyboard-save'
import {
  type EditableDimension,
  markPersisted,
  mergePromotedValue,
  seedDimensions,
  toWireDimensions,
} from '@/lib/editable-rows'

function configFingerprint(cfg: EnrichConfig): string {
  return JSON.stringify({
    p: cfg.promptTemplate ?? cfg.defaultPromptTemplate,
    d: cfg.dimensions,
  })
}

export function ClassificationSettingsPage() {
  const { t } = useTranslation()
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
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-16 text-sm text-muted-foreground">
        <p>{t('common.error')}</p>
        <Button type="button" variant="outline" size="sm" onClick={() => cfg.refetch()}>
          {t('common.retry')}
        </Button>
      </div>
    )
  }

  return <ClassificationSettingsForm initial={cfg.data} refetch={cfg.refetch} />
}

function ClassificationSettingsForm({
  initial,
  refetch,
}: {
  initial: EnrichConfig
  refetch: () => void
}) {
  const { t } = useTranslation()
  const { can } = usePermissions()
  const canEdit = can('settings:enrich_config:edit')
  const save = useUpdateEnrichConfig()
  const preview = usePreviewEnrichPrompt()

  const storedDraft = useRef(
    readDraft<{ prompt: string; rows: EditableDimension[] }>('classification-settings'),
  ).current
  const restoredFromStorage = storedDraft !== null
  const [prompt, setPrompt] = useState(
    () => storedDraft?.prompt ?? initial.promptTemplate ?? initial.defaultPromptTemplate,
  )
  const [rows, setRows] = useState<EditableDimension[]>(
    () => storedDraft?.rows ?? seedDimensions(initial.dimensions ?? []),
  )
  const [sample, setSample] = useState('')
  const [previewText, setPreviewText] = useState('')
  const [touched, setTouched] = useState(() => {
    if (!restoredFromStorage) return false
    const serverPrompt = initial.promptTemplate ?? initial.defaultPromptTemplate
    return (
      prompt !== serverPrompt ||
      JSON.stringify(toWireDimensions(rows)) !== JSON.stringify(initial.dimensions ?? [])
    )
  })
  const [discardOpen, setDiscardOpen] = useState(false)
  const [recoveryDismissed, setRecoveryDismissed] = useState(false)
  const [lastSavedAt, setLastSavedAt] = useState<Date | null>(null)
  const [conflictDetected, setConflictDetected] = useState(false)

  const serverFp = useMemo(() => configFingerprint(initial), [initial])
  const lastHydratedFp = useRef(serverFp)
  const touchedRef = useRef(touched)
  touchedRef.current = touched
  const awaitingServerHydrate = useRef(false)

  useEffect(() => {
    if (serverFp === lastHydratedFp.current) return
    lastHydratedFp.current = serverFp
    if (awaitingServerHydrate.current) {
      awaitingServerHydrate.current = false
      return
    }
    if (touchedRef.current) {
      setConflictDetected(true)
      return
    }
    setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
    setRows(seedDimensions(initial.dimensions ?? []))
  }, [serverFp, initial])

  const guard = useDraftGuard({
    storageKey: 'classification-settings',
    draft: { prompt, rows },
    dirty: touched,
    disabled: !canEdit,
    onExternalSave: refetch,
  })

  useEffect(() => {
    if (restoredFromStorage && !touched) {
      clearStoredDraft('classification-settings')
    }
  }, [restoredFromStorage, touched])

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

  const handleDiscard = () => {
    const snapshot = { prompt, rows }
    setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
    setRows(seedDimensions(initial.dimensions ?? []))
    setTouched(false)
    guard.clearDraft()
    setDiscardOpen(false)
    toast.info(t('draft.discarded_undo'), {
      id: 'classification-discard-undo',
      duration: 5000,
      action: {
        label: t('draft.undo'),
        onClick: () => {
          setPrompt(snapshot.prompt)
          setRows(snapshot.rows)
          setTouched(true)
        },
      },
    })
  }

  const handleDismissRecovery = () => {
    setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
    setRows(seedDimensions(initial.dimensions ?? []))
    setTouched(false)
    guard.clearDraft()
    setRecoveryDismissed(true)
  }

  const handleConflictLoadServer = () => {
    setPrompt(initial.promptTemplate ?? initial.defaultPromptTemplate)
    setRows(seedDimensions(initial.dimensions ?? []))
    setTouched(false)
    guard.clearDraft()
    setConflictDetected(false)
  }

  const handleConflictKeepDraft = () => {
    setConflictDetected(false)
  }

  const submitSave = (afterSuccess?: () => void) => {
    const defaultTmpl = initial.defaultPromptTemplate ?? ''
    const isDefaultPrompt = prompt.trim() === defaultTmpl.trim()
    const sentDimKeys = new Set(rows.map((r) => r._key))
    const sentTaxKeys = new Set(rows.flatMap((r) => r.taxonomy.map((tx) => tx._key)))
    awaitingServerHydrate.current = true
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
          setRecoveryDismissed(true)
          setConflictDetected(false)
          afterSuccess?.()
        },
        onError: (err) => {
          awaitingServerHydrate.current = false
          toast.error(err instanceof Error ? err.message : t('common.error'))
        },
      },
    )
  }

  const handleSave = () => {
    submitSave(() => {
      setLastSavedAt(new Date())
      toast.success(t('settings.saved'))
    })
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
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  useKeyboardSave({
    onSave: handleSave,
    enabled: canEdit && touched && !save.isPending,
  })

  const constrained = rows.some((d) => d.taxonomy.length > 0)
  const modeLabel = constrained ? t('settings.mode_constrained') : t('settings.mode_freeform')
  const constrainedCount = rows.filter((d) => d.taxonomy.length > 0).length
  const urgentEnabledCount = rows.filter((d) => d.urgentSet?.length > 0).length
  const showRecovery = restoredFromStorage && touched && !recoveryDismissed

  return (
    <section className="space-y-6" aria-label={touched ? t('draft.aria_unsaved') : undefined}>
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

      {showRecovery && (
        <DraftBanner
          variant="recovery"
          onDiscard={handleDismissRecovery}
          onKeep={() => setRecoveryDismissed(true)}
        />
      )}

      {conflictDetected && (
        <DraftBanner
          variant="conflict"
          onDiscard={handleConflictLoadServer}
          onKeep={handleConflictKeepDraft}
        />
      )}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(22rem,0.85fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('settings.prompt_title')}</CardTitle>
            <CardDescription>{t('settings.prompt_help')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-2">
            <div className="space-y-2">
              <Label htmlFor="prompt">{t('settings.prompt_label')}</Label>
              <textarea
                id="prompt"
                className="min-h-[320px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm leading-7 outline-none ring-offset-background placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                value={prompt}
                onChange={(e) => updatePrompt(e.target.value)}
                disabled={!canEdit || save.isPending}
              />
              <p className="text-xs text-muted-foreground">{t('settings.prompt_tokens')}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" onClick={handleSave} disabled={!canEdit || save.isPending}>
                {save.isPending ? (
                  <>
                    <Loader2 className="mr-2 size-4 animate-spin" />
                    {t('draft.status_saving')}
                  </>
                ) : (
                  t('common.save')
                )}
              </Button>
              {touched && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setDiscardOpen(true)}
                  disabled={save.isPending}
                >
                  {t('draft.discard_changes')}
                </Button>
              )}
              <SaveStatus dirty={touched} saving={save.isPending} lastSavedAt={lastSavedAt} />
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('settings.preview_title')}</CardTitle>
            <CardDescription>{t('settings.preview_help')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 pt-2">
            <div className="space-y-2">
              <Label htmlFor="sample">{t('settings.preview_sample_label')}</Label>
              <div className="flex gap-2">
                <Input
                  id="sample"
                  value={sample}
                  onChange={(e) => setSample(e.target.value)}
                  placeholder={t('settings.preview_sample_placeholder')}
                />
                <Button
                  type="button"
                  variant="secondary"
                  onClick={handlePreview}
                  disabled={preview.isPending}
                >
                  {preview.isPending ? t('app.loading') : t('settings.preview_button')}
                </Button>
              </div>
            </div>
            {previewText ? (
              <pre className="max-h-[26rem] overflow-x-auto rounded-md border border-border/60 bg-muted/20 p-4 text-xs leading-6 whitespace-pre-wrap">
                {previewText}
              </pre>
            ) : (
              <p className="py-10 text-center text-sm text-muted-foreground">
                {t('settings.preview_empty_body')}
              </p>
            )}
          </CardContent>
        </Card>
      </div>

      <Card className="border-border/60 shadow-none">
        <CardHeader>
          <CardTitle className="text-base">{t('settings.dimensions_title')}</CardTitle>
          <CardDescription>{t('settings.dimensions_help')}</CardDescription>
        </CardHeader>
        <CardContent className="pt-2">
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
              {t('draft.discard_continue_editing')}
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
