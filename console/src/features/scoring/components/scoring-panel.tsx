import { Calculator } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

type ScoringModel = 'rice' | 'ice'

interface ScoreInputs {
  reach: number
  impact: number
  confidence: number
  effort: number
}

function calculateRICE(inputs: ScoreInputs): number {
  if (inputs.effort === 0) return 0
  return (inputs.reach * inputs.impact * inputs.confidence) / inputs.effort
}

function calculateICE(inputs: ScoreInputs): number {
  return inputs.impact * inputs.confidence * (inputs.effort > 0 ? 10 / inputs.effort : 0)
}

export function ScoringPanel({
  onApply,
}: {
  onApply: (model: ScoringModel, score: number, inputs: ScoreInputs) => void
}) {
  const { t } = useTranslation()
  const [model, setModel] = useState<ScoringModel>('rice')
  const [inputs, setInputs] = useState<ScoreInputs>({
    reach: 100,
    impact: 3,
    confidence: 80,
    effort: 5,
  })

  const score = useMemo(
    () => (model === 'rice' ? calculateRICE(inputs) : calculateICE(inputs)),
    [model, inputs],
  )

  const handleChange = (field: keyof ScoreInputs, value: string) => {
    const num = Number.parseFloat(value) || 0
    setInputs((prev) => ({ ...prev, [field]: num }))
  }

  const handleReset = () => {
    setInputs({ reach: 100, impact: 3, confidence: 80, effort: 5 })
  }

  return (
    <div className="space-y-4 rounded-lg border p-4">
      <div className="flex items-center gap-2">
        <Calculator className="size-5 text-muted-foreground" />
        <h3 className="font-semibold">{t('scoring.title')}</h3>
      </div>

      <div className="flex gap-2">
        <Button
          variant={model === 'rice' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setModel('rice')}
        >
          {t('scoring.rice')}
        </Button>
        <Button
          variant={model === 'ice' ? 'default' : 'outline'}
          size="sm"
          onClick={() => setModel('ice')}
        >
          {t('scoring.ice')}
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-3">
        {model === 'rice' && (
          <div className="space-y-1">
            <span className="text-xs text-muted-foreground">{t('scoring.reach')}</span>
            <Input
              type="number"
              value={inputs.reach}
              onChange={(e) => handleChange('reach', e.target.value)}
            />
          </div>
        )}
        <div className="space-y-1">
          <span className="text-xs text-muted-foreground">{t('scoring.impact')}</span>
          <Input
            type="number"
            value={inputs.impact}
            onChange={(e) => handleChange('impact', e.target.value)}
          />
        </div>
        <div className="space-y-1">
          <span className="text-xs text-muted-foreground">{t('scoring.confidence')}</span>
          <Input
            type="number"
            value={inputs.confidence}
            onChange={(e) => handleChange('confidence', e.target.value)}
          />
        </div>
        <div className="space-y-1">
          <span className="text-xs text-muted-foreground">
            {model === 'rice' ? t('scoring.effort') : t('scoring.ease')}
          </span>
          <Input
            type="number"
            value={inputs.effort}
            onChange={(e) => handleChange('effort', e.target.value)}
          />
        </div>
      </div>

      <div className="flex items-center justify-between">
        <div>
          <span className="text-sm text-muted-foreground">{t('scoring.score')}: </span>
          <span className="text-lg font-bold" data-testid="score-value">
            {score.toFixed(1)}
          </span>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={handleReset}>
            {t('scoring.reset')}
          </Button>
          <Button size="sm" onClick={() => onApply(model, score, inputs)}>
            {t('scoring.apply')}
          </Button>
        </div>
      </div>
    </div>
  )
}
