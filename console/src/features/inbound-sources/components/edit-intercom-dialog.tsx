import { useQuery } from '@tanstack/react-query'
import { Loader2, Settings2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { inboundSourceQuery } from '@/features/inbound-sources/api/get-inbound-source'
import type { InboundSource } from '@/features/inbound-sources/api/list-inbound-sources'
import { useUpdateInboundSource } from '@/features/inbound-sources/api/update-inbound-source'

const splitTagList = (raw: string): string[] =>
  raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)

// EditIntercomSourceDialog — in-place settings edit for an Intercom
// source: name, tag filters, detail budget, and optional token rotation.
// Editing preserves the sync watermark and existing feedback linkage; a
// delete/recreate would reset both. Region is immutable server-side.
// The form prefills from the GET detail's intercom_settings so saving
// an untouched field re-submits the stored value, never a default.
export function EditIntercomSourceDialog({
  source,
  onClose,
}: {
  source: InboundSource | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const update = useUpdateInboundSource()
  const detail = useQuery({
    ...inboundSourceQuery(source?.id ?? 'edit-target'),
    enabled: source !== null,
  })
  const settings = detail.data?.intercomSettings
  const [name, setName] = useState('')
  const [filterTags, setFilterTags] = useState('')
  const [filterExcludeTags, setFilterExcludeTags] = useState('')
  const [maxDetailFetches, setMaxDetailFetches] = useState(50)
  const [accessToken, setAccessToken] = useState('')

  // Re-seed the form whenever the source or its stored settings load.
  useEffect(() => {
    if (source) {
      setName(source.name)
      setAccessToken('')
    }
  }, [source])
  useEffect(() => {
    if (settings) {
      setFilterTags(settings.filterTags.join(', '))
      setFilterExcludeTags(settings.filterExcludeTags.join(', '))
      setMaxDetailFetches(settings.maxDetailFetches || 50)
    }
  }, [settings])

  const submit = () => {
    if (!source) return
    update.mutate(
      {
        id: source.id,
        name: name.trim() !== source.name ? name.trim() : undefined,
        intercomConfig: {
          region: '',
          accessToken: accessToken.trim(),
          filterTags: splitTagList(filterTags),
          filterExcludeTags: splitTagList(filterExcludeTags),
          // States aren't exposed in this form — re-submit stored ones.
          filterStates: settings?.filterStates ?? [],
          maxDetailFetches,
        },
      },
      {
        onSuccess: () => {
          toast.success(t('inbound_sources.edit.saved'))
          onClose()
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const settingsPending = source !== null && detail.isPending

  return (
    <Dialog open={source !== null} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings2 className="h-4 w-4" />
            {t('inbound_sources.edit.title')}
          </DialogTitle>
          <DialogDescription>{t('inbound_sources.edit.body')}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1.5">
            <Label htmlFor="edit-src-name">{t('inbound_sources.create.name_field')}</Label>
            <Input id="edit-src-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="edit-src-tags">
              {t('inbound_sources.create.intercom.filter_tags')}
            </Label>
            <Input
              id="edit-src-tags"
              value={filterTags}
              onChange={(e) => setFilterTags(e.target.value)}
              placeholder={t('inbound_sources.create.intercom.filter_tags_help')}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="edit-src-exclude">
              {t('inbound_sources.create.intercom.filter_exclude_tags')}
            </Label>
            <Input
              id="edit-src-exclude"
              value={filterExcludeTags}
              onChange={(e) => setFilterExcludeTags(e.target.value)}
              placeholder={t('inbound_sources.create.intercom.filter_exclude_tags_help')}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="edit-src-budget">
              {t('inbound_sources.create.intercom.max_detail_fetches')}
            </Label>
            <Input
              id="edit-src-budget"
              type="number"
              min={1}
              max={500}
              value={maxDetailFetches}
              onChange={(e) => setMaxDetailFetches(Number(e.target.value) || 50)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="edit-src-token">{t('inbound_sources.edit.rotate_token')}</Label>
            <Input
              id="edit-src-token"
              type="password"
              value={accessToken}
              onChange={(e) => setAccessToken(e.target.value)}
              placeholder={t('inbound_sources.edit.rotate_token_help')}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={update.isPending}>
            {t('common.cancel')}
          </Button>
          <Button onClick={submit} disabled={update.isPending || settingsPending}>
            {(update.isPending || settingsPending) && (
              <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
            )}
            {t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
