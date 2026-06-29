import { FileText, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export interface ChangelogEntry {
  id: string
  title: string
  body: string
  published: boolean
  createdAt: string
}

export function ChangelogPage({
  entries,
  onAdd,
  onRemove,
  onPublish,
}: {
  entries: ChangelogEntry[]
  onAdd: (entry: { title: string; body: string }) => void
  onRemove: (id: string) => void
  onPublish: (id: string) => void
}) {
  const { t } = useTranslation()
  const [showForm, setShowForm] = useState(false)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')

  const handleAdd = () => {
    if (!title.trim()) return
    onAdd({ title: title.trim(), body: body.trim() })
    setTitle('')
    setBody('')
    setShowForm(false)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <FileText className="size-5 text-muted-foreground" />
          <h2 className="text-lg font-semibold">{t('changelog_page.title')}</h2>
        </div>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="size-4" />
          {t('changelog_page.create_entry')}
        </Button>
      </div>

      {showForm && (
        <div className="rounded-md border p-4 space-y-3">
          <Input
            placeholder={t('changelog_page.entry_title')}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
          <textarea
            className="flex min-h-[100px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
            placeholder={t('changelog_page.entry_body')}
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
          <Button size="sm" onClick={handleAdd} disabled={!title.trim()}>
            {t('changelog_page.draft')}
          </Button>
        </div>
      )}

      {entries.length === 0 ? (
        <p className="text-center text-muted-foreground py-8">{t('changelog_page.empty')}</p>
      ) : (
        <div className="space-y-4">
          {entries.map((entry) => (
            <div key={entry.id} className="rounded-lg border p-4 space-y-2">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="font-medium">{entry.title}</h3>
                  <span className="text-xs text-muted-foreground">{entry.createdAt}</span>
                </div>
                <div className="flex items-center gap-1">
                  <span
                    className={`text-xs px-2 py-0.5 rounded-full ${entry.published ? 'bg-green-100 text-green-700' : 'bg-yellow-100 text-yellow-700'}`}
                  >
                    {entry.published ? t('changelog_page.published') : t('changelog_page.draft')}
                  </span>
                  {!entry.published && (
                    <Button variant="ghost" size="sm" onClick={() => onPublish(entry.id)}>
                      {t('changelog_page.publish')}
                    </Button>
                  )}
                  <Button variant="ghost" size="icon" onClick={() => onRemove(entry.id)}>
                    <Trash2 className="size-4 text-destructive" />
                  </Button>
                </div>
              </div>
              {entry.body && <p className="text-sm text-muted-foreground">{entry.body}</p>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
