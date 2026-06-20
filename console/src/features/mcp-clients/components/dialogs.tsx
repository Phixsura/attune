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
  const [redirectUri, setRedirectUri] = useState('')
  const [scopes, setScopes] = useState<string[]>(['mcp:read'])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    await onSubmit({
      name: name.trim(),
      redirect_uris: [redirectUri.trim()],
      scopes,
    })
    setName('')
    setRedirectUri('')
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
              <Label htmlFor="redirect_uri">
                {t('mcp_clients.create_dialog.redirect_uri_label')}
              </Label>
              <Input
                id="redirect_uri"
                value={redirectUri}
                onChange={(e) => setRedirectUri(e.target.value)}
                placeholder="http://localhost:8080/callback"
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
              disabled={pending || !name.trim() || !redirectUri.trim() || scopes.length === 0}
            >
              {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t('mcp_clients.create_button')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function RevokeMCPClientDialog({
  target,
  onCancel,
  onConfirm,
  pending,
}: {
  target: MCPClient | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()

  return (
    <Dialog open={!!target} onOpenChange={(open: boolean) => !open && onCancel()}>
      <DialogContent>
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
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t('mcp_clients.revoke_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
