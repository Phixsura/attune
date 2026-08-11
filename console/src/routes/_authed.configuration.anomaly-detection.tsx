import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  type AnomalyConfig,
  anomalyConfigQuery,
  useUpdateAnomalyConfig,
} from '@/features/anomalies/api/anomalies'
import { useDocumentTitle } from '@/hooks/use-document-title'

export const Route = createFileRoute('/_authed/configuration/anomaly-detection')({
  component: AnomalyConfigPage,
})

const SENSITIVITIES = ['high', 'medium', 'low'] as const
const NOTIFY_MODES = ['immediate', 'digest', 'off'] as const

export function AnomalyConfigPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('anomalies.config.title', 'Anomaly detection'))
  const { data } = useQuery(anomalyConfigQuery())
  const update = useUpdateAnomalyConfig()
  const [draft, setDraft] = useState<AnomalyConfig | undefined>()

  useEffect(() => {
    if (data && !draft) setDraft(data)
  }, [data, draft])

  if (!draft) {
    return <div className="p-4" data-testid="anomaly-config-loading" />
  }

  const save = () => {
    update.mutate(
      { config: draft },
      {
        onError: (err) => toast.error(String(err)),
        onSuccess: () => toast.success(t('anomalies.config.saved', 'Configuration saved')),
      },
    )
  }

  return (
    <div className="max-w-2xl space-y-4 p-4" data-testid="anomaly-config-page">
      <Card>
        <CardHeader>
          <CardTitle>{t('anomalies.config.title', 'Anomaly detection')}</CardTitle>
          <CardDescription>
            {t(
              'anomalies.config.subtitle',
              'Spike/drop detection over daily feedback volume. Defaults are safe; raise sensitivity only if you want more alerts.',
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center gap-2">
            <input
              checked={draft.detectionEnabled}
              data-testid="detection-enabled"
              id="detection-enabled"
              onChange={(e) => setDraft({ ...draft, detectionEnabled: e.target.checked })}
              type="checkbox"
            />
            <Label htmlFor="detection-enabled">
              {t('anomalies.config.enabled', 'Detection enabled')}
            </Label>
          </div>

          <div className="grid gap-2">
            <Label>{t('anomalies.config.sensitivity', 'Sensitivity')}</Label>
            <div className="flex gap-2" data-testid="sensitivity-tiers">
              {SENSITIVITIES.map((s) => (
                <Button
                  key={s}
                  onClick={() => setDraft({ ...draft, sensitivity: s })}
                  size="sm"
                  variant={draft.sensitivity === s ? 'default' : 'outline'}
                >
                  {t(`anomalies.config.sensitivity_${s}`, s)}
                </Button>
              ))}
            </div>
          </div>

          <div className="grid gap-2">
            <Label htmlFor="min-count">
              {t('anomalies.config.min_count', 'Minimum daily count to alert')}
            </Label>
            <Input
              data-testid="min-count"
              id="min-count"
              max={10000}
              min={0}
              onChange={(e) => setDraft({ ...draft, minCount: Number(e.target.value) })}
              type="number"
              value={draft.minCount}
            />
          </div>

          <div className="grid gap-2">
            <Label htmlFor="settle-delay">
              {t('anomalies.config.settle_delay', 'Settle delay (hours after day close)')}
            </Label>
            <Input
              data-testid="settle-delay"
              id="settle-delay"
              max={48}
              min={0}
              onChange={(e) => setDraft({ ...draft, settleDelayHours: Number(e.target.value) })}
              type="number"
              value={draft.settleDelayHours}
            />
          </div>

          <div className="grid gap-2">
            <Label>{t('anomalies.config.notify_mode', 'Notifications')}</Label>
            <div className="flex gap-2" data-testid="notify-modes">
              {NOTIFY_MODES.map((m) => (
                <Button
                  key={m}
                  onClick={() => setDraft({ ...draft, notifyMode: m })}
                  size="sm"
                  variant={draft.notifyMode === m ? 'default' : 'outline'}
                >
                  {t(`anomalies.config.notify_${m}`, m)}
                </Button>
              ))}
            </div>
          </div>

          <Button data-testid="save-config" disabled={update.isPending} onClick={save}>
            {t('common.save', 'Save')}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('anomalies.config.custom_slices', 'Custom slices')}</CardTitle>
          <CardDescription>
            {t(
              'anomalies.config.custom_slices_hint',
              'Monitor a specific combination (e.g. source=zendesk AND severity=critical). Up to 20.',
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {draft.customSlices.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="no-custom-slices">
              {t('anomalies.config.no_custom_slices', 'No custom slices configured')}
            </p>
          ) : (
            <ul className="space-y-2" data-testid="custom-slice-list">
              {draft.customSlices.map((s) => (
                <li
                  className="flex items-center gap-2 rounded border p-2 text-sm"
                  key={s.id || s.name}
                >
                  <span className="font-medium">{s.name}</span>
                  {s.lastError ? (
                    <span
                      className="rounded bg-red-500/15 px-1.5 py-0.5 text-xs text-red-600"
                      data-testid="slice-error-badge"
                    >
                      {s.lastError}
                    </span>
                  ) : null}
                  <Button
                    className="ml-auto"
                    onClick={() =>
                      setDraft({
                        ...draft,
                        customSlices: draft.customSlices.filter((x) => x !== s),
                      })
                    }
                    size="sm"
                    variant="ghost"
                  >
                    {t('common.remove', 'Remove')}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
