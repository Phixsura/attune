import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function WebhookUrlsDisplay({
  urls,
  provider,
  compact,
}: {
  urls: string[]
  provider: string
  compact?: boolean
}) {
  const { t } = useTranslation()
  const amplitudeKeys = [
    'cohort_sync.source.webhook_url_labels.amplitude_create',
    'cohort_sync.source.webhook_url_labels.amplitude_add',
    'cohort_sync.source.webhook_url_labels.amplitude_remove',
  ]

  if (urls.length === 0) return <span className="text-xs text-muted-foreground">—</span>

  if (compact) {
    return <CopyableUrl url={urls[0]} label={t('cohort_sync.source.webhook_urls')} />
  }

  return (
    <div className="space-y-2">
      {urls.map((url, i) => (
        <CopyableUrl
          key={url}
          url={url}
          label={
            provider === 'amplitude'
              ? t(amplitudeKeys[i] ?? 'cohort_sync.source.webhook_url_labels.default')
              : t('cohort_sync.source.webhook_url_labels.default')
          }
        />
      ))}
    </div>
  )
}

function CopyableUrl({ url, label }: { url: string; label: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // clipboard not available in some contexts
    }
  }

  return (
    <div className="flex items-center gap-2">
      <div className="min-w-0 flex-1">
        {label && <div className="text-xs font-medium text-muted-foreground">{label}</div>}
        <code className="block max-w-[24rem] truncate text-xs" title={url}>
          {url}
        </code>
      </div>
      <Button variant="ghost" size="sm" onClick={copy} className="shrink-0">
        {copied ? (
          <>
            <Check className="mr-1 h-3 w-3" />
            {t('common.copied')}
          </>
        ) : (
          <>
            <Copy className="mr-1 h-3 w-3" />
            {t('common.copy')}
          </>
        )}
      </Button>
    </div>
  )
}
