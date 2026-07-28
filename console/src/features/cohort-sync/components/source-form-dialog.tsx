import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { CohortSource } from '../api/cohort-sync'

export interface SourceFormData {
  provider: string
  name: string
  credential?: string
  pullCredential?: string
  baseUrl?: string
  enabled?: boolean
}

export function SourceFormDialog({
  mode,
  open,
  onOpenChange,
  pending,
  source,
  onSubmit,
}: {
  mode: 'create' | 'edit'
  open: boolean
  onOpenChange: (v: boolean) => void
  pending: boolean
  source?: CohortSource
  onSubmit: (data: SourceFormData) => void
}) {
  const { t } = useTranslation()
  const [provider, setProvider] = useState(source?.provider ?? 'amplitude')
  const [name, setName] = useState(source?.name ?? '')
  const [credential, setCredential] = useState('')
  const [pullCredential, setPullCredential] = useState('')
  const [baseUrl, setBaseUrl] = useState(source?.baseUrl ?? '')

  // Reset form when dialog opens/closes.
  useEffect(() => {
    if (open) {
      setProvider(source?.provider ?? 'amplitude')
      setName(source?.name ?? '')
      setCredential('')
      setPullCredential('')
      setBaseUrl(source?.baseUrl ?? '')
    }
  }, [open, source])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (mode === 'create') {
      onSubmit({
        provider,
        name,
        credential,
        pullCredential: pullCredential || undefined,
        baseUrl: baseUrl || undefined,
      })
    } else {
      onSubmit({
        provider,
        name,
        credential: credential || undefined,
        pullCredential: pullCredential || undefined,
        baseUrl: baseUrl || undefined,
        enabled: source?.enabled,
      })
    }
  }

  const isValid = name.trim() !== '' && (mode === 'edit' || credential.trim() !== '')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {mode === 'create' ? t('cohort_sync.source.create') : t('cohort_sync.source.edit')}
            </DialogTitle>
            <DialogDescription>{t('cohort_sync.subtitle')}</DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div>
              <Label htmlFor="cs-provider">{t('cohort_sync.source.provider')}</Label>
              <Select
                value={provider}
                onValueChange={setProvider}
                disabled={mode === 'edit' || pending}
              >
                <SelectTrigger id="cs-provider">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="amplitude">Amplitude</SelectItem>
                  <SelectItem value="mixpanel">Mixpanel</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div>
              <Label htmlFor="cs-name">{t('cohort_sync.source.name')}</Label>
              <Input
                id="cs-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={pending}
                placeholder={t('cohort_sync.source.name_placeholder')}
              />
            </div>

            <div>
              <Label htmlFor="cs-credential">
                {t('cohort_sync.source.credential')}
                {mode === 'edit' && (
                  <span className="ml-1 text-xs text-muted-foreground">
                    ({t('common.optional')})
                  </span>
                )}
              </Label>
              <Input
                id="cs-credential"
                type="password"
                value={credential}
                onChange={(e) => setCredential(e.target.value)}
                disabled={pending}
                placeholder={
                  mode === 'edit' ? t('cohort_sync.source.credential_edit_placeholder') : undefined
                }
              />
              <p className="mt-1 text-xs text-muted-foreground">
                {t('cohort_sync.source.credential_help')}
              </p>
            </div>

            <div>
              <Label htmlFor="cs-pull-credential">{t('cohort_sync.source.pull_credential')}</Label>
              <Input
                id="cs-pull-credential"
                type="password"
                value={pullCredential}
                onChange={(e) => setPullCredential(e.target.value)}
                disabled={pending}
                placeholder={provider === 'amplitude' ? 'api_key:secret_key' : 'username:secret'}
              />
              <p className="mt-1 text-xs text-muted-foreground">
                {provider === 'amplitude'
                  ? t('cohort_sync.source.pull_credential_help_amplitude')
                  : t('cohort_sync.source.pull_credential_help_mixpanel')}
              </p>
            </div>

            <div>
              <Label htmlFor="cs-base-url">
                {t('cohort_sync.source.base_url')}{' '}
                <span className="text-xs text-muted-foreground">({t('common.optional')})</span>
              </Label>
              <Input
                id="cs-base-url"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                disabled={pending}
                placeholder={
                  provider === 'amplitude' ? 'https://amplitude.com' : 'https://mixpanel.com'
                }
              />
            </div>
          </div>

          <DialogFooter className="mt-6">
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={!isValid || pending}>
              {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {mode === 'create' ? t('common.create') : t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
