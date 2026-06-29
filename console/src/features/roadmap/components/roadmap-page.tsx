import { MapPin, Plus, ThumbsUp, Trash2 } from 'lucide-react'
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

export type RoadmapStatus = 'planned' | 'in_progress' | 'completed'

export interface RoadmapItem {
  id: string
  title: string
  description: string
  status: RoadmapStatus
  votes: number
}

const STATUS_LIST: RoadmapStatus[] = ['planned', 'in_progress', 'completed']

export function RoadmapPage({
  items,
  onAdd,
  onRemove,
  onVote,
}: {
  items: RoadmapItem[]
  onAdd: (item: Omit<RoadmapItem, 'id' | 'votes'>) => void
  onRemove: (id: string) => void
  onVote: (id: string) => void
}) {
  const { t } = useTranslation()
  const [showForm, setShowForm] = useState(false)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [status, setStatus] = useState<RoadmapStatus>('planned')

  const handleAdd = () => {
    if (!title.trim()) return
    onAdd({ title: title.trim(), description: description.trim(), status })
    setTitle('')
    setDescription('')
    setStatus('planned')
    setShowForm(false)
  }

  const grouped = STATUS_LIST.map((s) => ({
    status: s,
    items: items.filter((i) => i.status === s),
  }))

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <MapPin className="size-5 text-muted-foreground" />
          <h2 className="text-lg font-semibold">{t('roadmap.title')}</h2>
        </div>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="size-4" />
          {t('roadmap.add_item')}
        </Button>
      </div>

      {showForm && (
        <div className="rounded-md border p-4 space-y-3">
          <Input
            placeholder={t('roadmap.item_title')}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
          <Input
            placeholder={t('roadmap.item_description')}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
          <Select value={status} onValueChange={(v) => setStatus(v as RoadmapStatus)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUS_LIST.map((s) => (
                <SelectItem key={s} value={s}>
                  {t(`roadmap.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="sm" onClick={handleAdd} disabled={!title.trim()}>
            {t('roadmap.add_item')}
          </Button>
        </div>
      )}

      {items.length === 0 ? (
        <p className="text-center text-muted-foreground py-8">{t('roadmap.empty')}</p>
      ) : (
        <div className="grid grid-cols-3 gap-4">
          {grouped.map(({ status: s, items: groupItems }) => (
            <div key={s} className="space-y-3">
              <h3 className="text-sm font-semibold text-muted-foreground uppercase">
                {t(`roadmap.${s}`)}
              </h3>
              {groupItems.map((item) => (
                <div key={item.id} className="rounded-lg border p-3 space-y-2">
                  <div className="flex items-start justify-between">
                    <span className="font-medium text-sm">{item.title}</span>
                    <Button variant="ghost" size="icon" onClick={() => onRemove(item.id)}>
                      <Trash2 className="size-3 text-destructive" />
                    </Button>
                  </div>
                  {item.description && (
                    <p className="text-xs text-muted-foreground">{item.description}</p>
                  )}
                  <Button variant="outline" size="sm" onClick={() => onVote(item.id)}>
                    <ThumbsUp className="size-3" />
                    <span className="ml-1">{item.votes}</span>
                  </Button>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
