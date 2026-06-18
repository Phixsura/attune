import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, Copy, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import type { CreateApiKeyParams, NewApiKey } from '@/features/api-keys/api/create-api-key'
import type { ApiKey } from '@/features/api-keys/api/list-api-keys'
import { presetsQuery, scopesQuery } from '@/features/api-keys/api/list-scopes'

export function CreateKeyDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (params: CreateApiKeyParams) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [label, setLabel] = useState('')
  const [selectedPreset, setSelectedPreset] = useState<string>('full_access')
  const [customScopes, setCustomScopes] = useState<Set<string>>(new Set())
  const [showAdvanced, setShowAdvanced] = useState(false)

  const { data: scopesData } = useQuery(scopesQuery())
  const { data: presetsData } = useQuery(presetsQuery())

  const presets = presetsData?.presets ?? []
  const scopes = scopesData?.scopes ?? []

  const getEffectiveScopes = (): string[] => {
    if (selectedPreset === 'custom') {
      return Array.from(customScopes)
    }
    const preset = presets.find((p) => p.id === selectedPreset)
    return preset?.scopes ?? []
  }

  const toggleScope = (scope: string) => {
    const next = new Set(customScopes)
    if (next.has(scope)) {
      next.delete(scope)
    } else {
      next.add(scope)
    }
    setCustomScopes(next)
  }

  const handleSubmit = () => {
    if (!label.trim()) return
    const scopes = getEffectiveScopes()
    void onSubmit({ label: label.trim(), scopes }).then(() => {
      setLabel('')
      setSelectedPreset('full_access')
      setCustomScopes(new Set())
      setShowAdvanced(false)
    })
  }

  const groupedScopes = scopes.reduce(
    (acc, s) => {
      if (!acc[s.resource]) acc[s.resource] = []
      acc[s.resource].push(s)
      return acc
    },
    {} as Record<string, typeof scopes>,
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            handleSubmit()
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('api_keys.create_dialog.title')}</DialogTitle>
            <DialogDescription>{t('api_keys.create_dialog.label_help')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="api-key-label">{t('api_keys.create_dialog.label_field')}</Label>
              <Input
                id="api-key-label"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                placeholder={t('api_keys.create_dialog.label_placeholder')}
                maxLength={200}
                autoFocus
                disabled={pending}
              />
            </div>

            <div className="space-y-2">
              <Label>{t('api_keys.create_dialog.scope_preset', '权限模板')}</Label>
              <Select value={selectedPreset} onValueChange={setSelectedPreset} disabled={pending}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {presets.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                    </SelectItem>
                  ))}
                  <SelectItem value="custom">
                    {t('api_keys.create_dialog.custom_scopes', '自定义权限')}
                  </SelectItem>
                </SelectContent>
              </Select>
              {selectedPreset !== 'custom' && (
                <p className="text-xs text-muted-foreground">
                  {presets.find((p) => p.id === selectedPreset)?.description}
                </p>
              )}
            </div>

            {selectedPreset === 'custom' && (
              <div className="space-y-2 rounded-md border p-3">
                <Label className="text-sm font-medium">
                  {t('api_keys.create_dialog.select_scopes', '选择权限')}
                </Label>
                <div className="max-h-48 space-y-3 overflow-y-auto">
                  {Object.entries(groupedScopes).map(([resource, resourceScopes]) => (
                    <div key={resource} className="space-y-1">
                      <p className="text-xs font-medium text-muted-foreground uppercase">
                        {resource}
                      </p>
                      {resourceScopes.map((s) => (
                        <label
                          key={s.scope}
                          htmlFor={`scope-${s.scope}`}
                          className="flex items-center gap-2 text-sm cursor-pointer hover:bg-muted/50 rounded px-1 py-0.5"
                        >
                          <Checkbox
                            id={`scope-${s.scope}`}
                            checked={customScopes.has(s.scope)}
                            onCheckedChange={() => toggleScope(s.scope)}
                            disabled={pending}
                          />
                          <span className="font-mono text-xs">{s.scope}</span>
                          <span className="text-xs text-muted-foreground">— {s.description}</span>
                        </label>
                      ))}
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="gap-1 text-xs text-muted-foreground"
                onClick={() => setShowAdvanced(!showAdvanced)}
              >
                <ChevronDown
                  className={`h-3 w-3 transition-transform ${showAdvanced ? 'rotate-180' : ''}`}
                />
                {t('api_keys.create_dialog.show_scopes', '查看生效权限')}
              </Button>
              {showAdvanced && (
                <div className="mt-2 rounded-md bg-muted/50 p-2">
                  <p className="mb-1 text-xs font-medium text-muted-foreground">
                    {t('api_keys.create_dialog.effective_scopes', '生效权限')}:
                  </p>
                  <div className="flex flex-wrap gap-1">
                    {getEffectiveScopes().length > 0 ? (
                      getEffectiveScopes().map((s) => (
                        <span
                          key={s}
                          className="inline-flex items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 font-mono text-xs"
                        >
                          <Check className="h-2.5 w-2.5 text-primary" />
                          {s}
                        </span>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground italic">
                        {t('api_keys.create_dialog.no_scopes', '无权限')}
                      </span>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
              data-testid="create-key-cancel"
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              disabled={pending || !label.trim()}
              data-testid="create-key-submit"
            >
              {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function SecretKeyDialog({
  issued,
  onClose,
}: {
  issued: NewApiKey | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const onCopy = () => {
    if (!issued) return
    void navigator.clipboard.writeText(issued.secret).then(() => {
      toast.success(t('api_keys.secret_dialog.copy_hint'))
    })
  }
  return (
    <Dialog open={issued !== null} onOpenChange={(v) => !v && onClose()}>
      <DialogContent onPointerDownOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{t('api_keys.secret_dialog.title')}</DialogTitle>
          <DialogDescription>{t('api_keys.secret_dialog.body')}</DialogDescription>
        </DialogHeader>
        {issued && (
          <div className="my-4 flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
            <code className="flex-1 break-all font-mono text-xs">{issued.secret}</code>
            <Button size="sm" variant="ghost" onClick={onCopy} data-testid="secret-copy">
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
        <DialogFooter>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function RevokeKeyDialog({
  target,
  onCancel,
  onConfirm,
  pending,
}: {
  target: ApiKey | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={target !== null} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('api_keys.revoke_confirm_title')}</DialogTitle>
          <DialogDescription>{t('api_keys.revoke_confirm_body')}</DialogDescription>
        </DialogHeader>
        {target && (
          <p className="my-2 font-mono text-sm text-muted-foreground">
            {target.keyPrefix}… &nbsp; · &nbsp; {target.label || '—'}
          </p>
        )}
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={onCancel}
            disabled={pending}
            data-testid="revoke-key-cancel"
          >
            {t('common.cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={pending}
            data-testid="revoke-key-confirm"
          >
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('api_keys.revoke_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
