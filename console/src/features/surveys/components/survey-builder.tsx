import { BarChart3, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export type SurveyType = 'nps' | 'csat' | 'ces'

export interface SurveyDefinition {
  id: string
  name: string
  surveyType: SurveyType
  question: string
  enabled: boolean
}

const SURVEY_TYPES: SurveyType[] = ['nps', 'csat', 'ces']

const DEFAULT_QUESTIONS: Record<SurveyType, string> = {
  nps: 'How likely are you to recommend us? (0-10)',
  csat: 'How satisfied are you with our service? (1-5)',
  ces: 'How easy was it to resolve your issue? (1-7)',
}

export function SurveyBuilder({
  surveys,
  onAdd,
  onRemove,
  onToggle,
}: {
  surveys: SurveyDefinition[]
  onAdd: (survey: Omit<SurveyDefinition, 'id'>) => void
  onRemove: (id: string) => void
  onToggle: (id: string, enabled: boolean) => void
}) {
  const { t } = useTranslation()
  const [showForm, setShowForm] = useState(false)
  const [name, setName] = useState('')
  const [surveyType, setSurveyType] = useState<SurveyType>('nps')
  const [question, setQuestion] = useState(DEFAULT_QUESTIONS.nps)

  const handleTypeChange = (type: SurveyType) => {
    setSurveyType(type)
    setQuestion(DEFAULT_QUESTIONS[type])
  }

  const handleAdd = () => {
    if (!name.trim() || !question.trim()) return
    onAdd({
      name: name.trim(),
      surveyType,
      question: question.trim(),
      enabled: true,
    })
    setName('')
    setSurveyType('nps')
    setQuestion(DEFAULT_QUESTIONS.nps)
    setShowForm(false)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <BarChart3 className="size-5 text-muted-foreground" />
          <h2 className="text-lg font-semibold">{t('surveys.title')}</h2>
        </div>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="size-4" />
          {t('surveys.create')}
        </Button>
      </div>

      {showForm && (
        <div className="rounded-md border p-4 space-y-3">
          <Input
            placeholder={t('surveys.name_placeholder')}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <div className="grid grid-cols-2 gap-3">
            <Select value={surveyType} onValueChange={(v) => handleTypeChange(v as SurveyType)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SURVEY_TYPES.map((st) => (
                  <SelectItem key={st} value={st}>
                    {t(`surveys.type_${st}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Input
            placeholder={t('surveys.question_placeholder')}
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
          />
          <Button size="sm" onClick={handleAdd} disabled={!name.trim() || !question.trim()}>
            {t('surveys.save')}
          </Button>
        </div>
      )}

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('surveys.name')}</TableHead>
            <TableHead>{t('surveys.type')}</TableHead>
            <TableHead>{t('surveys.question')}</TableHead>
            <TableHead>{t('surveys.status')}</TableHead>
            <TableHead className="w-16" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {surveys.length === 0 ? (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                {t('surveys.empty')}
              </TableCell>
            </TableRow>
          ) : (
            surveys.map((s) => (
              <TableRow key={s.id}>
                <TableCell className="font-medium">{s.name}</TableCell>
                <TableCell>
                  <span className="inline-flex items-center rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                    {s.surveyType.toUpperCase()}
                  </span>
                </TableCell>
                <TableCell className="max-w-[200px] truncate text-sm text-muted-foreground">
                  {s.question}
                </TableCell>
                <TableCell>
                  <Button variant="ghost" size="sm" onClick={() => onToggle(s.id, !s.enabled)}>
                    {s.enabled ? t('surveys.enabled') : t('surveys.disabled')}
                  </Button>
                </TableCell>
                <TableCell>
                  <Button variant="ghost" size="icon" onClick={() => onRemove(s.id)}>
                    <Trash2 className="size-4 text-destructive" />
                  </Button>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}
