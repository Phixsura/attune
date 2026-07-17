import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
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
import { useRestoreFocusOnClose } from '@/hooks/use-restore-focus-on-close'
import { restoreFocusWhenReady } from '@/lib/focus'
import type { CreateMCPClientRequest, MCPClient } from '../api/types'

const AVAILABLE_SCOPES = [
  { id: 'mcp:read', label: 'Read', description: 'Read feedback, tags, workflow states' },
  { id: 'mcp:write', label: 'Write', description: 'Update status, add/remove tags' },
  { id: 'mcp:ingest', label: 'Ingest', description: 'Submit new feedback' },
]

export function CreateMCPClientDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (params: CreateMCPClientRequest) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [redirectUrisText, setRedirectUrisText] = useState('')
  const [scopes, setScopes] = useState<string[]>(['mcp:read'])
  const parsedRedirectURIs = parseRedirectURIs(redirectUrisText)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      await onSubmit({
        name: name.trim(),
        redirect_uris: parsedRedirectURIs,
        scopes,
      })
    } catch {
      return
    }
    setName('')
    setRedirectUrisText('')
    setScopes(['mcp:read'])
  }

  const toggleScope = (scope: string) => {
    setScopes((prev) => (prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope]))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t('mcp_clients.create_dialog.title')}</DialogTitle>
            <DialogDescription>{t('mcp_clients.create_dialog.description')}</DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-4">
            <div className="grid gap-2">
              <Label htmlFor="name">{t('mcp_clients.create_dialog.name_label')}</Label>
              <Input
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="claude-code-agent"
                required
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="redirect_uris">
                {t('mcp_clients.create_dialog.redirect_uri_label')}
              </Label>
              <p className="text-xs leading-5 text-muted-foreground">
                {t('mcp_clients.create_dialog.redirect_uri_hint')}
              </p>
              <textarea
                id="redirect_uris"
                value={redirectUrisText}
                onChange={(e) => setRedirectUrisText(e.target.value)}
                placeholder={t('mcp_clients.create_dialog.redirect_uri_placeholder')}
                rows={4}
                className="min-h-24 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs transition-[color,box-shadow] outline-none placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 dark:bg-input/30 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40"
                required
              />
            </div>

            <div className="grid gap-2">
              <Label>{t('mcp_clients.create_dialog.scopes_label')}</Label>
              <div className="space-y-2">
                {AVAILABLE_SCOPES.map((scope) => (
                  <div key={scope.id} className="flex items-center space-x-2">
                    <Checkbox
                      id={scope.id}
                      checked={scopes.includes(scope.id)}
                      onCheckedChange={() => toggleScope(scope.id)}
                    />
                    <label
                      htmlFor={scope.id}
                      className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                    >
                      {scope.label}
                      <span className="ml-2 text-muted-foreground font-normal">
                        — {scope.description}
                      </span>
                    </label>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              disabled={
                pending || !name.trim() || parsedRedirectURIs.length === 0 || scopes.length === 0
              }
            >
              {pending && <Loader2 aria-hidden="true" className="mr-2 h-4 w-4 animate-spin" />}
              {t('mcp_clients.create_button')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function parseRedirectURIs(value: string): string[] {
  const seen = new Set<string>()
  const redirects: string[] = []

  for (const item of value.split(/[\n,]+/)) {
    const normalized = item.trim()
    if (!normalized || seen.has(normalized)) {
      continue
    }
    seen.add(normalized)
    redirects.push(normalized)
  }

  return redirects
}

export function RevokeMCPClientDialog({
  target,
  onCancel,
  onConfirm,
  pending,
  restoreFocusRef,
}: {
  target: MCPClient | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
  restoreFocusRef?: React.RefObject<HTMLElement | null>
}) {
  const { t } = useTranslation()
  const open = !!target
  useRestoreFocusOnClose(open, restoreFocusRef)

  return (
    <Dialog open={open} onOpenChange={(open: boolean) => !open && onCancel()}>
      <DialogContent
        onCloseAutoFocus={(event) => {
          const restoreFocusTo = restoreFocusRef?.current
          if (!restoreFocusTo?.isConnected) return
          event.preventDefault()
          restoreFocusWhenReady(restoreFocusTo)
        }}
      >
        <DialogHeader>
          <DialogTitle>{t('mcp_clients.revoke_dialog.title')}</DialogTitle>
          <DialogDescription>
            {t('mcp_clients.revoke_dialog.description', { name: target?.name })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 aria-hidden="true" className="mr-2 h-4 w-4 animate-spin" />}
            {t('mcp_clients.revoke_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
