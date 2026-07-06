import { AlertTriangle, Loader2, Trash2 } from 'lucide-react'
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

export function ServiceAccountDeleteDialog({
  open,
  onOpenChange,
  serviceAccountName,
  onConfirm,
  pending,
  restoreFocusRef,
  restoreFocusOnClose = true,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  serviceAccountName: string
  onConfirm: () => Promise<unknown>
  pending: boolean
  restoreFocusRef?: RefObject<HTMLElement | null>
  restoreFocusOnClose?: boolean
}) {
  const { t } = useTranslation()
  useRestoreFocusOnClose(open, restoreFocusRef, restoreFocusOnClose)

  const title = t('api_keys.service_accounts.delete_dialog.title', {
    name: serviceAccountName,
    defaultValue: '删除服务账号 {{name}}？',
  })
  const description = t('api_keys.service_accounts.delete_dialog.description', {
    name: serviceAccountName,
    defaultValue: '删除后，{{name}} 将从目录中移除，关联的 API key 会自动解绑并继续可用。',
  })

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
          <DialogTitle className="flex items-center gap-2">
            <Trash2 className="h-5 w-5 text-destructive" aria-hidden="true" />
            {title}
          </DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
          <span>
            {t(
              'api_keys.service_accounts.delete_dialog.warning',
              '这会永久移除服务账号记录，但不会删除已签发的 API key。',
            )}
          </span>
        </div>
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
            variant="destructive"
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
            {t('common.delete')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
