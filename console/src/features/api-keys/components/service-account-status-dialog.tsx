import { Loader2 } from 'lucide-react'
import type { RefObject } from 'react'
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
import { useRestoreFocusOnClose } from '@/hooks/use-restore-focus-on-close'
import { restoreFocusWhenReady } from '@/lib/focus'

export function ServiceAccountStatusDialog({
  open,
  onOpenChange,
  serviceAccountName,
  nextActive,
  onConfirm,
  pending,
  restoreFocusRef,
  restoreFocusOnClose = true,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  serviceAccountName: string
  nextActive: boolean
  onConfirm: () => Promise<unknown>
  pending: boolean
  restoreFocusRef?: RefObject<HTMLElement | null>
  restoreFocusOnClose?: boolean
}) {
  const { t } = useTranslation()
  useRestoreFocusOnClose(open, restoreFocusRef, restoreFocusOnClose)

  const title = nextActive
    ? t('api_keys.service_accounts.toggle_dialog.title_enable', {
        name: serviceAccountName,
        defaultValue: '启用服务账号 {{name}}？',
      })
    : t('api_keys.service_accounts.toggle_dialog.title_disable', {
        name: serviceAccountName,
        defaultValue: '停用服务账号 {{name}}？',
      })
  const description = nextActive
    ? t('api_keys.service_accounts.toggle_dialog.description_enable', {
        name: serviceAccountName,
        defaultValue: '启用后，{{name}} 关联的 API key 会恢复通过鉴权。',
      })
    : t('api_keys.service_accounts.toggle_dialog.description_disable', {
        name: serviceAccountName,
        defaultValue: '停用后，{{name}} 关联的 API key 会立即拒绝鉴权，直到再次启用。',
      })
  const confirmLabel = nextActive
    ? t('api_keys.service_accounts.toggle_dialog.confirm_enable', '启用')
    : t('api_keys.service_accounts.toggle_dialog.confirm_disable', '停用')

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
      }}
    >
      <DialogContent
        role="alertdialog"
        showCloseButton={false}
        onCloseAutoFocus={(event) => {
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
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            variant={nextActive ? 'default' : 'destructive'}
            onClick={() => {
              void onConfirm()
                .then(() => {
                  onOpenChange(false)
                })
                .catch(() => undefined)
            }}
            disabled={pending}
          >
            {pending && <Loader2 aria-hidden="true" className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
