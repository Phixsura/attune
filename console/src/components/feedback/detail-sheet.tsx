import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { feedbackDetailQuery } from '@/api/queries'
import { KindBadge, SeverityBadge } from '@/components/feedback/badges'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

export function FeedbackDetailSheet({
  id,
  onOpenChange,
}: {
  id: string | null
  onOpenChange: (v: boolean) => void
}) {
  const { t } = useTranslation()
  const open = id !== null
  const detail = useQuery({ ...feedbackDetailQuery(id ?? ''), enabled: open })
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{detail.data?.enrichedTitle || `#${id ?? '?'}`}</SheetTitle>
          <SheetDescription>
            {detail.data && (
              <span className="flex items-center gap-2 text-xs">
                <KindBadge kind={detail.data.enrichedKind} />
                <SeverityBadge severity={detail.data.enrichedSeverity} />
                <span className="text-muted-foreground">
                  {format(new Date(detail.data.createdAt), 'PPP HH:mm', { locale: zhCN })}
                </span>
              </span>
            )}
          </SheetDescription>
        </SheetHeader>
        <div className="px-1 py-6 text-sm">
          {detail.isPending && (
            <div className="flex justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          )}
          {detail.data && <DetailBody data={detail.data} />}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function DetailBody({ data }: { data: import('@/api/queries').FeedbackDetail }) {
  const { t } = useTranslation()
  return (
    <div className="space-y-6">
      <Section label={t('feedback.detail.raw_content')}>
        <p className="whitespace-pre-wrap break-words">{data.content}</p>
      </Section>

      {data.enrichedModules && data.enrichedModules.length > 0 ? (
        <Section label={t('feedback.detail.modules')}>
          <div className="flex flex-wrap gap-1">
            {data.enrichedModules.map((m) => (
              <span
                key={m}
                className="rounded-md border border-border bg-muted px-1.5 py-0.5 text-xs"
              >
                {m}
              </span>
            ))}
          </div>
        </Section>
      ) : null}

      <Section label={t('feedback.detail.source')}>
        <p className="font-mono text-xs text-muted-foreground">
          {data.source} · userId={data.userId || '—'}
          {data.pageUrl && ` · ${data.pageUrl}`}
        </p>
      </Section>

      {data.sourceMeta && Object.keys(data.sourceMeta).length > 0 ? (
        <Section label={t('feedback.detail.sourceMeta')}>
          <pre className="overflow-x-auto rounded-md border bg-muted/40 p-2 text-xs">
            {JSON.stringify(data.sourceMeta, null, 2)}
          </pre>
        </Section>
      ) : null}

      {data.attachments && data.attachments.length > 0 ? (
        <Section label={t('feedback.detail.attachments')}>
          <pre className="overflow-x-auto rounded-md border bg-muted/40 p-2 text-xs">
            {JSON.stringify(data.attachments, null, 2)}
          </pre>
        </Section>
      ) : null}

      {data.enrichmentError ? (
        <Section label={t('feedback.detail.enrichmentError')}>
          <p className="rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive">
            {data.enrichmentError}
          </p>
        </Section>
      ) : null}

      {data.enrichedAt ? (
        <Section label={t('feedback.detail.enrichedAt')}>
          <p className="text-xs text-muted-foreground">
            {format(new Date(data.enrichedAt), 'PPP HH:mm:ss', { locale: zhCN })}
          </p>
        </Section>
      ) : null}
    </div>
  )
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </h4>
      {children}
    </div>
  )
}
