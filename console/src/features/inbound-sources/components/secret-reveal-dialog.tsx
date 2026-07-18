import { Copy, ShieldAlert } from 'lucide-react'
import { useState } from 'react'
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

// SecretRevealDialog — the one-shot post-create / post-rotate reveal
// of a webhook secret. The raw secret is never stored client-side
// beyond this dialog's lifetime; closing the dialog clears it.
//
// Optional url + curlExample are shown when the server populated them
// (the tenant slug + base URL combo). Rotation responses do not carry
// the URL (it didn't change), so those props are optional.
export function SecretRevealDialog({
  open,
  onClose,
  url,
  secretHex,
  curlExample,
}: {
  open: boolean
  onClose: () => void
  url?: string
  secretHex: string
  curlExample?: string
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState<'secret' | 'url' | 'curl' | null>(null)

  const copy = async (which: 'secret' | 'url' | 'curl', value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(which)
      /* v8 ignore next -- @preserve: timer reset is browser time behavior; copy state assertion covers the user path. */
      setTimeout(() => setCopied(null), 1500)
    } catch {
      // Clipboard may be blocked (insecure context, denied permission).
      // The mono <code> block is still selectable manually.
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldAlert className="h-4 w-4 text-amber-600" />
            {t('inbound_sources.reveal.title')}
          </DialogTitle>
          <DialogDescription>{t('inbound_sources.reveal.body')}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {url && (
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">
                  {t('inbound_sources.reveal.url_label')}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => copy('url', url)}
                  className="h-7 gap-1 text-xs"
                >
                  <Copy className="h-3 w-3" />
                  {copied === 'url' ? t('common.copied') : t('common.copy')}
                </Button>
              </div>
              <code className="block break-all rounded-md border border-border bg-muted/40 px-2 py-1.5 font-mono text-xs">
                {url}
              </code>
            </div>
          )}

          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">
                {t('inbound_sources.reveal.secret_label')}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => copy('secret', secretHex)}
                className="h-7 gap-1 text-xs"
              >
                <Copy className="h-3 w-3" />
                {copied === 'secret' ? t('common.copied') : t('common.copy')}
              </Button>
            </div>
            <code className="block break-all rounded-md border border-border bg-muted/40 px-2 py-1.5 font-mono text-xs">
              {secretHex}
            </code>
          </div>

          {curlExample && (
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">
                  {t('inbound_sources.reveal.curl_label')}
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => copy('curl', curlExample)}
                  className="h-7 gap-1 text-xs"
                >
                  <Copy className="h-3 w-3" />
                  {copied === 'curl' ? t('common.copied') : t('common.copy')}
                </Button>
              </div>
              <pre className="overflow-x-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-xs">
                {curlExample}
              </pre>
            </div>
          )}

          <p className="rounded-md border border-amber-600/30 bg-amber-600/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-500">
            {t('inbound_sources.reveal.warning')}
          </p>
        </div>

        <DialogFooter>
          <Button onClick={onClose}>{t('inbound_sources.reveal.acknowledge')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
