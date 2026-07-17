import { Loader2 } from 'lucide-react'
import { type RefObject, useState } from 'react'
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
import type { CreateServiceAccountParams } from '@/features/api-keys/api/create-service-account'
import { useRestoreFocusOnClose } from '@/hooks/use-restore-focus-on-close'
import { restoreFocusWhenReady } from '@/lib/focus'

export function CreateServiceAccountDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
  restoreFocusRef,
  restoreFocusOnClose = true,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (params: CreateServiceAccountParams) => Promise<unknown>
  pending: boolean
  restoreFocusRef?: RefObject<HTMLElement | null>
  restoreFocusOnClose?: boolean
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  useRestoreFocusOnClose(open, restoreFocusRef, restoreFocusOnClose)

  const reset = () => {
    setName('')
    setDescription('')
  }

  const handleSubmit = () => {
    const trimmedName = name.trim()
    if (!trimmedName) return
    const trimmedDescription = description.trim()
    void onSubmit({
      name: trimmedName,
      description: trimmedDescription || undefined,
    })
      .then(() => {
        reset()
        onOpenChange(false)
      })
      .catch(() => undefined)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) {
          reset()
        }
      }}
    >
      <DialogContent
        className="sm:max-w-lg"
        onCloseAutoFocus={(event) => {
          /* v8 ignore next -- @preserve: parent dialogs opt out of Radix focus restore in browser-only flows. */
          if (!restoreFocusOnClose) {
            event.preventDefault()
            return
          }
          const restoreFocusTo = restoreFocusRef?.current
          if (!restoreFocusTo?.isConnected) return
          event.preventDefault()
          restoreFocusWhenReady(restoreFocusTo)
        }}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            handleSubmit()
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('api_keys.service_accounts.create_dialog.title')}</DialogTitle>
            <DialogDescription>
              {t(
                'api_keys.service_accounts.create_dialog.description',
                '把自动化身份与人类登录分开，方便后续审计和轮换。',
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="service-account-name">
                {t('api_keys.service_accounts.create_dialog.name_field', '名称')}
              </Label>
              <Input
                id="service-account-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t(
                  'api_keys.service_accounts.create_dialog.name_placeholder',
                  '例如：ci-bot',
                )}
                maxLength={200}
                autoFocus
                disabled={pending}
              />
              <p className="text-xs text-muted-foreground">
                {t(
                  'api_keys.service_accounts.create_dialog.name_help',
                  '名称应能让操作员看出它的用途。',
                )}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="service-account-description">
                {t('api_keys.service_accounts.create_dialog.description_field', '说明')}
              </Label>
              <Input
                id="service-account-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t(
                  'api_keys.service_accounts.create_dialog.description_placeholder',
                  '例如：部署流水线与定时任务',
                )}
                maxLength={500}
                disabled={pending}
              />
              <p className="text-xs text-muted-foreground">
                {t(
                  'api_keys.service_accounts.create_dialog.description_help',
                  '可选。写清楚负责人、用途或自动化场景，后续更容易审计。',
                )}
              </p>
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
              data-testid="create-service-account-cancel"
            >
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              disabled={pending || !name.trim()}
              data-testid="create-service-account-submit"
            >
              {pending && <Loader2 aria-hidden="true" className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
