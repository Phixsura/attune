import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useDeleteDigestSubscription } from '@/features/digest-subscription/api/delete-digest-subscription'
import { digestSubscriptionQuery } from '@/features/digest-subscription/api/get-digest-subscription'
import { useUpsertDigestSubscription } from '@/features/digest-subscription/api/upsert-digest-subscription'

type Frequency = 'daily' | 'weekly'

const WEEKDAYS = [0, 1, 2, 3, 4, 5, 6]

// DigestSubscriptionPage is the per-tenant daily-digest config tab (#27). It is
// a singleton: load the current config (or create-defaults when none exists),
// edit inline, then Save (PUT) or Delete.
export function DigestSubscriptionPage() {
  const { t } = useTranslation()
  const q = useQuery(digestSubscriptionQuery())
  const upsert = useUpsertDigestSubscription()
  const del = useDeleteDigestSubscription()

  const [enabled, setEnabled] = useState(true)
  const [frequency, setFrequency] = useState<Frequency>('daily')
  const [sendHour, setSendHour] = useState(9)
  const [byweekday, setByweekday] = useState(1)
  const [llmMin, setLlmMin] = useState(6)
  const [sendOnEmpty, setSendOnEmpty] = useState(false)
  const [themePrompt, setThemePrompt] = useState('')
  const [clusteringEnabled, setClusteringEnabled] = useState(false)

  useEffect(() => {
    const d = q.data
    if (!d) return
    setEnabled(d.enabled)
    setFrequency(d.frequency === 'weekly' ? 'weekly' : 'daily')
    setSendHour(d.sendHour ?? 9)
    setByweekday(d.byweekday ?? 1)
    setLlmMin(d.llmMinFeedback || 6)
    setSendOnEmpty(d.sendOnEmpty)
    setThemePrompt(d.themePrompt ?? '')
    setClusteringEnabled(d.clusteringEnabled ?? false)
  }, [q.data])

  const handleSave = () => {
    upsert.mutate(
      {
        enabled,
        frequency,
        sendHour,
        llmMinFeedback: llmMin,
        sendOnEmpty,
        byweekday: frequency === 'weekly' ? byweekday : undefined,
        themePrompt: themePrompt.trim() || undefined,
        clusteringEnabled,
      },
      {
        onSuccess: () => toast.success(t('common.save')),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handleDelete = () => {
    del.mutate(undefined, {
      onSuccess: () => toast.success(t('digest.deleted')),
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })
  }

  if (q.isPending) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  const exists = q.data != null

  return (
    <section className="min-w-0 space-y-6">
      <div>
        <h2 className="text-lg font-semibold tracking-tight">{t('digest.title')}</h2>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('digest.subtitle')}</p>
      </div>

      <Card>
        <CardContent className="space-y-5 pt-6">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              data-testid="digest-enabled"
            />
            {t('digest.enabled_field')}
          </label>

          <div className="space-y-2">
            <Label htmlFor="digest-frequency">{t('digest.frequency_field')}</Label>
            <Select value={frequency} onValueChange={(v) => setFrequency(v as Frequency)}>
              <SelectTrigger id="digest-frequency" data-testid="digest-frequency">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="daily">{t('digest.frequency_daily')}</SelectItem>
                <SelectItem value="weekly">{t('digest.frequency_weekly')}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="digest-send-hour">{t('digest.send_hour_field')}</Label>
            <Input
              id="digest-send-hour"
              type="number"
              min={0}
              max={23}
              value={sendHour}
              onChange={(e) => setSendHour(Number(e.target.value) || 0)}
              data-testid="digest-send-hour"
            />
            <p className="text-xs text-muted-foreground">{t('digest.send_hour_help')}</p>
          </div>

          {frequency === 'weekly' ? (
            <div className="space-y-2">
              <Label htmlFor="digest-weekday">{t('digest.weekday_field')}</Label>
              <Select value={String(byweekday)} onValueChange={(v) => setByweekday(Number(v))}>
                <SelectTrigger id="digest-weekday" data-testid="digest-weekday">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {WEEKDAYS.map((d) => (
                    <SelectItem key={d} value={String(d)}>
                      {t(`digest.weekday.${d}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}

          <div className="space-y-2">
            <Label htmlFor="digest-llm-min">{t('digest.llm_min_field')}</Label>
            <Input
              id="digest-llm-min"
              type="number"
              min={1}
              value={llmMin}
              onChange={(e) => setLlmMin(Number(e.target.value) || 6)}
              data-testid="digest-llm-min"
            />
            <p className="text-xs text-muted-foreground">{t('digest.llm_min_help')}</p>
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={sendOnEmpty}
              onChange={(e) => setSendOnEmpty(e.target.checked)}
              data-testid="digest-send-on-empty"
            />
            {t('digest.send_on_empty_field')}
          </label>

          <div className="space-y-2">
            <Label htmlFor="digest-theme-prompt">{t('digest.theme_prompt_field')}</Label>
            <Input
              id="digest-theme-prompt"
              value={themePrompt}
              onChange={(e) => setThemePrompt(e.target.value)}
              placeholder={t('digest.theme_prompt_placeholder')}
              data-testid="digest-theme-prompt"
            />
          </div>

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={clusteringEnabled}
              onChange={(e) => setClusteringEnabled(e.target.checked)}
              data-testid="digest-clustering-enabled"
            />
            {t('digest.clustering_enabled_field')}
          </label>
          <p className="text-xs text-muted-foreground">{t('digest.clustering_enabled_help')}</p>

          {q.data?.nextRunAt ? (
            <p className="text-xs text-muted-foreground" data-testid="digest-next-run">
              {t('digest.next_run')}: {q.data.nextRunAt}
            </p>
          ) : null}
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <Button
          type="button"
          variant="ghost"
          onClick={handleDelete}
          disabled={!exists || del.isPending}
          data-testid="digest-delete"
        >
          {t('digest.delete_button')}
        </Button>
        <Button
          type="button"
          onClick={handleSave}
          disabled={upsert.isPending}
          data-testid="digest-save"
        >
          {upsert.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
          {t('common.save')}
        </Button>
      </div>
    </section>
  )
}
