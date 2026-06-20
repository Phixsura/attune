import { useQuery } from '@tanstack/react-query'
import { Loader2, Sparkles, TrendingUp } from 'lucide-react'
import { useState } from 'react'
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

  const onPromote = async (dim: string, value: string) => {
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
      setPromoting(null)
    }
  }

  const data = q.data
  const hasResults = data && (data.candidates.length > 0 || Object.keys(data.coverage).length > 0)

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
        {!analyzed && (
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

        {data && hasResults && (
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
                      {Math.round(c.confidence * 100)}%
                    </TableCell>
                    <TableCell className="text-right tabular-nums text-emerald-600">
                      +{Math.round(c.coverageImpact * 100)}%
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
