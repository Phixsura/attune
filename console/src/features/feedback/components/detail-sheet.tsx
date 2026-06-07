import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  type FeedbackDetail,
  feedbackDetailQuery,
} from '@/features/feedback/api/get-feedback-detail'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { Dimension } from '@/proto/attune/v1/common'

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
  const config = useQuery(enrichConfigQuery())
  const dims = config.data?.dimensions ?? []
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>
            <span className="inline-flex items-center gap-2">
              <UrgentDot urgent={detail.data?.isUrgent} />
              {detail.data?.enrichedTitle || `#${id ?? '?'}`}
            </span>
          </SheetTitle>
          <SheetDescription>
            {detail.data && (
              <span className="flex flex-wrap items-center gap-2 text-xs">
                {dims.map((dim) => (
                  <DimensionChips
                    key={dim.name}
                    dim={dim}
                    value={
                      (detail.data?.enrichedAttrs as Record<string, unknown> | undefined)?.[
                        dim.name
                      ]
                    }
                    emptyDash={false}
                  />
                ))}
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
          {detail.data && <DetailBody data={detail.data} dims={dims} />}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function DetailBody({ data, dims }: { data: FeedbackDetail; dims: Dimension[] }) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const attrs = (data.enrichedAttrs ?? {}) as Record<string, unknown>
  return (
    <div className="space-y-6">
      <Section label={t('feedback.detail.raw_content')}>
        <p className="whitespace-pre-wrap break-words">{data.content}</p>
      </Section>

      {data.enrichedRationale ? (
        <Section label={t('feedback.detail.ai_rationale')}>
          <p className="rounded-md border border-border bg-muted/40 p-3 whitespace-pre-wrap break-words text-muted-foreground">
            {data.enrichedRationale}
          </p>
        </Section>
      ) : null}

      {dims.length > 0 && (
        <Section label={t('feedback.detail.attrs')}>
          <dl className="space-y-2">
            {dims.map((dim) => (
              <div key={dim.name} className="flex items-start gap-3">
                <dt className="w-28 shrink-0 text-xs text-muted-foreground">
                  {displayOf(dim.displayName) || dim.name}
                </dt>
                <dd className="flex-1 text-sm">
                  <DimensionChips dim={dim} value={attrs[dim.name]} />
                </dd>
              </div>
            ))}
          </dl>
        </Section>
      )}

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
