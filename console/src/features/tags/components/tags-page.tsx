import { useQuery } from '@tanstack/react-query'
import { Archive, Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { TagBadge } from '@/components/tag/tag-badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useArchiveTag } from '@/features/tags/api/archive-tag'
import { useCreateTag } from '@/features/tags/api/create-tag'
import { type Tag, tagsQuery } from '@/features/tags/api/list-tags'
import { useUpdateTag } from '@/features/tags/api/update-tag'
import { TagFormDialog } from './tag-form-dialog'

export function TagsPage() {
  const { t } = useTranslation()
  const tags = useQuery(tagsQuery())
  const createTag = useCreateTag()
  const updateTag = useUpdateTag()
  const archiveTag = useArchiveTag()

  const [createOpen, setCreateOpen] = useState(false)
  const [editTag, setEditTag] = useState<Tag | undefined>()

  const handleCreate = (data: {
    name: string
    color: string
    description: string
    exclusiveScope: string
  }) => {
    createTag.mutate(
      {
        name: data.name,
        color: data.color,
        description: data.description || undefined,
        exclusiveScope: data.exclusiveScope || undefined,
      },
      {
        onSuccess: () => {
          setCreateOpen(false)
          toast.success(t('tags.created'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleUpdate = (data: {
    name: string
    color: string
    description: string
    exclusiveScope: string
  }) => {
    if (!editTag) return
    updateTag.mutate(
      {
        id: editTag.id,
        name: data.name,
        color: data.color,
        description: data.description,
        exclusiveScope: data.exclusiveScope || undefined,
      },
      {
        onSuccess: () => {
          setEditTag(undefined)
          toast.success(t('settings.saved'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleArchive = (tag: Tag) => {
    archiveTag.mutate(tag.id, {
      onSuccess: () => toast.success(t('tags.archived')),
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  const items = tags.data ?? []

  return (
    <section className="min-w-0 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold tracking-tight">{t('tags.title')}</h2>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('tags.subtitle')}</p>
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" />
          {t('tags.create_button')}
        </Button>
      </div>

      {items.length === 0 ? (
        <EmptyState title={t('tags.empty.title')} description={t('tags.empty.body')} />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('tags.table.name')}</TableHead>
              <TableHead>{t('tags.table.scope')}</TableHead>
              <TableHead className="text-right">{t('tags.table.usage')}</TableHead>
              <TableHead className="w-24" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((tag) => (
              <TableRow key={tag.id}>
                <TableCell>
                  <TagBadge name={tag.name} color={tag.color} />
                  {tag.description ? (
                    <span className="ml-2 text-xs text-muted-foreground">{tag.description}</span>
                  ) : null}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {tag.exclusiveScope || '—'}
                </TableCell>
                <TableCell className="text-right tabular-nums">{tag.usageCount}</TableCell>
                <TableCell>
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={() => setEditTag(tag)}
                    >
                      <Pencil className="h-3.5 w-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-destructive"
                      onClick={() => handleArchive(tag)}
                    >
                      <Archive className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <TagFormDialog
        open={createOpen}
        pending={createTag.isPending}
        onOpenChange={setCreateOpen}
        onSubmit={handleCreate}
      />

      <TagFormDialog
        open={!!editTag}
        tag={editTag}
        pending={updateTag.isPending}
        onOpenChange={(v) => {
          if (!v) setEditTag(undefined)
        }}
        onSubmit={handleUpdate}
      />
    </section>
  )
}
