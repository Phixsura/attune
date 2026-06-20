import { useQuery } from '@tanstack/react-query'
import { Loader2, Sparkles, TrendingUp } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { evalSuggestionsQuery } from '@/features/settings/api/get-eval-suggestions'
import { usePromoteSuggestedValue } from '@/features/settings/api/promote-suggested-value'

// formatPct renders a 0–1 ratio as a percent. A nonzero ratio that would
// round to "0%" is shown as "<1%" so a real candidate never appears to have
// zero confidence/impact (reachable for rare values in large eval samples).
function formatPct(ratio: number): string {
  const pct = ratio * 100
  if (pct > 0 && pct < 0.5) return '<1%'
  return `${Math.round(pct)}%`
}

// SuggestedValuesPanel surfaces off-list values the LLM repeatedly suggested
// during eval (#83). Running the eval re-classifies a sample via the LLM, so
// the fetch is gated behind an explicit "Analyze" click. Each candidate can be
// promoted into the dimension taxonomy in one click; on success it disappears
// from the list and coverage rises.
export function SuggestedValuesPanel({ canEdit }: { canEdit: boolean }) {
  const { t } = useTranslation()
  const [analyzed, setAnalyzed] = useState(false)
  const q = useQuery(evalSuggestionsQuery(analyzed))
  const promote = usePromoteSuggestedValue()
  const [promoting, setPromoting] = useState<string | null>(null)
  // Synchronous re-entrancy latch: `promoting` state updates async, so a
  // rapid second click reads the stale (null) value and slips through. A ref
  // flips synchronously, so the second click bails before a duplicate POST.
  const inFlight = useRef(false)

  const onPromote = async (dim: string, value: string) => {
    if (inFlight.current) return
    inFlight.current = true
    setPromoting(`${dim}:${value}`)
    try {
      await promote.mutateAsync({
        dimensionName: dim,
        value,
        displayName: { entries: { 'zh-CN': value } },
      })
      toast.success(t('settings.suggestions.promoted', { value }))
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('settings.suggestions.promote_failed'))
    } finally {
      inFlight.current = false
      setPromoting(null)
    }
  }

  const data = q.data
  const coverageDims = data ? Object.keys(data.coverage).sort() : []
  const hasResults = data && (data.candidates.length > 0 || coverageDims.length > 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Sparkles className="h-4 w-4" />
          {t('settings.suggestions.title')}
        </CardTitle>
        <CardDescription>{t('settings.suggestions.help')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Analyze runs an admin-only, LLM-cost eval — gate it behind edit
            permission so a view-only member doesn't trigger a 403. */}
        {!canEdit && (
          <p className="text-sm text-muted-foreground" data-testid="suggestions-readonly">
            {t('settings.suggestions.readonly')}
          </p>
        )}

        {canEdit && !analyzed && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => setAnalyzed(true)}
            data-testid="analyze-suggestions"
          >
            <TrendingUp className="mr-2 h-3.5 w-3.5" />
            {t('settings.suggestions.analyze')}
          </Button>
        )}

        {analyzed && q.isPending && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t('settings.suggestions.analyzing')}
          </div>
        )}

        {analyzed && q.isError && (
          <p className="text-sm text-destructive" data-testid="suggestions-error">
            {t('settings.suggestions.error')}
          </p>
        )}

        {analyzed && !q.isPending && !q.isError && !hasResults && (
          <p className="text-sm text-muted-foreground" data-testid="suggestions-empty">
            {t('settings.suggestions.empty')}
          </p>
        )}

        {data && hasResults && coverageDims.length > 0 && (
          <div className="flex flex-wrap gap-2" data-testid="coverage-summary">
            <span className="text-xs text-muted-foreground">
              {t('settings.suggestions.coverage_label')}
            </span>
            {coverageDims.map((dim) => (
              <span
                key={dim}
                className="rounded border px-2 py-0.5 text-xs tabular-nums"
                data-testid={`coverage-${dim}`}
              >
                <span className="font-mono">{dim}</span> {Math.round(data.coverage[dim] * 100)}%
              </span>
            ))}
          </div>
        )}

        {data && data.candidates.length > 0 && (
          <Table data-testid="suggestions-table">
            <TableHeader>
              <TableRow>
                <TableHead>{t('settings.suggestions.col_dim')}</TableHead>
                <TableHead>{t('settings.suggestions.col_value')}</TableHead>
                <TableHead className="text-right">{t('settings.suggestions.col_count')}</TableHead>
                <TableHead className="text-right">
                  {t('settings.suggestions.col_confidence')}
                </TableHead>
                <TableHead className="text-right">{t('settings.suggestions.col_impact')}</TableHead>
                <TableHead className="text-right" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.candidates.map((c) => {
                const key = `${c.dim}:${c.value}`
                return (
                  <TableRow key={key} data-testid={`suggestion-row-${c.value}`}>
                    <TableCell className="font-mono text-xs">{c.dim}</TableCell>
                    <TableCell className="font-medium">{c.value}</TableCell>
                    <TableCell className="text-right tabular-nums">{c.count}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatPct(c.confidence)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-emerald-600">
                      +{formatPct(c.coverageImpact)}
                    </TableCell>
                    <TableCell className="text-right">
                      {canEdit && (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={promoting !== null}
                          onClick={() => onPromote(c.dim, c.value)}
                          data-testid={`promote-${c.value}`}
                        >
                          {promoting === key ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            t('settings.suggestions.promote')
                          )}
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
