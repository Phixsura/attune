import { useTranslation } from 'react-i18next'
import type { GetAnomalyEvidenceResponse } from '@/proto/attune/v1/anomaly'

interface ContributionBarsProps {
  evidence: GetAnomalyEvidenceResponse
}

/**
 * Top-3 contribution bars: which grouping values explain the deviation.
 * share = (obs_v − exp_v) / (obs_total − exp_total), kept at |share| ≥ 15%.
 */
export function ContributionBars({ evidence }: ContributionBarsProps) {
  const { t } = useTranslation()
  if (evidence.spread || evidence.contributions.length === 0) {
    return (
      <p className="text-sm text-muted-foreground" data-testid="contribution-spread">
        {t('anomalies.contribution.spread', 'Broadly distributed — no concentrated origin')}
      </p>
    )
  }
  return (
    <ul className="space-y-2" data-testid="contribution-bars">
      {evidence.contributions.map((c) => (
        <li key={`${c.dim}:${c.value}`}>
          <div className="flex items-center justify-between text-sm">
            <span className="truncate">
              {c.dim}={c.value}
            </span>
            <span className="tabular-nums text-muted-foreground">{Math.round(c.share * 100)}%</span>
          </div>
          <div className="h-2 w-full rounded bg-muted">
            <div
              className="h-2 rounded bg-primary"
              style={{ width: `${Math.min(100, Math.abs(c.share) * 100)}%` }}
            />
          </div>
        </li>
      ))}
    </ul>
  )
}
