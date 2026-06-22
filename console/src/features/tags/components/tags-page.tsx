import { useQuery } from '@tanstack/react-query'
import { Archive, Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { TagBadge } from '@/components/tag/tag-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
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
  const [archiveTarget, setArchiveTarget] = useState<Tag | undefined>()

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

  const handleArchiveConfirm = () => {
    if (!archiveTarget) return
    archiveTag.mutate(archiveTarget.id, {
      onSuccess: () => {
        setArchiveTarget(undefined)
        toast.success(t('tags.archived'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  const items = tags.data ?? []
  const totalTags = items.length
  const usedTags = items.filter((tag) => Number(tag.usageCount) > 0).length
  const scopedTags = items.filter((tag) => Boolean(tag.exclusiveScope)).length
  const dormantTags = items.filter((tag) => Number(tag.usageCount) === 0).length

  return (
    <section className="min-w-0 space-y-6">
      <PageHero
        eyebrow={t('shell.groups.configuration')}
        title={t('tags.title')}
        subtitle={t('tags.subtitle')}
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            {t('tags.create_button')}
          </Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('tags.summary.total')}
              value={String(totalTags)}
              hint={t('tags.summary.total_hint')}
            />
            <PageHeroMetric
              label={t('tags.summary.in_use')}
              value={String(usedTags)}
              hint={t('tags.summary.in_use_hint')}
            />
            <PageHeroMetric
              label={t('tags.summary.scoped')}
              value={String(scopedTags)}
              hint={t('tags.summary.scoped_hint')}
            />
            <PageHeroMetric
              label={t('tags.summary.dormant')}
              value={String(dormantTags)}
              hint={t('tags.summary.dormant_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(20rem,0.65fr)]">
        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('tags.registry_title')}</CardTitle>
            <CardDescription>
              {t('tags.registry_description', { count: totalTags })}
            </CardDescription>
          </CardHeader>
          <CardContent className="px-0">
            {items.length === 0 ? (
              <EmptyState
                title={t('tags.empty.title')}
                description={t('tags.empty.body')}
                action={{ label: t('tags.create_button'), onClick: () => setCreateOpen(true) }}
                className="py-16"
              />
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
                        <div className="flex min-w-0 items-center gap-2.5">
                          <TagBadge name={tag.name} color={tag.color} />
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium text-foreground">
                              {tag.name}
                            </div>
                            <div className="truncate text-xs text-muted-foreground">
                              {tag.description || t('tags.no_description')}
                            </div>
                          </div>
                        </div>
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
                            aria-label={t('tags.edit_label')}
                            onClick={() => setEditTag(tag)}
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-muted-foreground hover:text-destructive"
                            aria-label={t('tags.archive_label')}
                            onClick={() => setArchiveTarget(tag)}
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
          </CardContent>
        </Card>

        <Card className="border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <CardTitle>{t('tags.playbook_title')}</CardTitle>
            <CardDescription>{t('tags.playbook_description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 pt-6">
            <PlaybookRow
              index="1"
              title={t('tags.playbook.scope_title')}
              body={t('tags.playbook.scope_body')}
            />
            <PlaybookRow
              index="2"
              title={t('tags.playbook.color_title')}
              body={t('tags.playbook.color_body')}
            />
            <PlaybookRow
              index="3"
              title={t('tags.playbook.archive_title')}
              body={t('tags.playbook.archive_body')}
            />
          </CardContent>
        </Card>
      </div>

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

      <Dialog
        open={!!archiveTarget}
        onOpenChange={(v) => {
          if (!v) setArchiveTarget(undefined)
        }}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>{t('tags.archive_label')}</DialogTitle>
            <DialogDescription>
              {t('tags.archive_confirm', { name: archiveTarget?.name })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setArchiveTarget(undefined)}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleArchiveConfirm}>
              {t('common.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function PlaybookRow({ index, title, body }: { index: string; title: string; body: string }) {
  return (
    <div className="rounded-[1rem] border border-border/70 bg-background/85 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <div className="flex size-7 shrink-0 items-center justify-center rounded-full bg-foreground text-xs font-semibold text-background">
          {index}
        </div>
        <div>
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <div className="mt-1 text-sm leading-6 text-muted-foreground">{body}</div>
        </div>
      </div>
    </div>
  )
}
