import { Copy, FileDown, FileText } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  buildReplayWorksheetMarkdown,
  replayWorksheetDownloadHref,
  replayWorksheetDownloadName,
} from '../replay-worksheet'

export function ReplayWorksheetCard({
  tenantName,
  dashboardHref,
}: {
  tenantName: string
  dashboardHref: string
}) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const worksheetMarkdown = buildReplayWorksheetMarkdown(tenantName, dashboardHref)
  const downloadHref = replayWorksheetDownloadHref(tenantName, dashboardHref)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(worksheetMarkdown)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Card className="border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(249,250,251,0.98))] shadow-none">
      <CardHeader className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <CardTitle className="flex items-center gap-2 text-base">
              <FileText className="h-4 w-4 text-muted-foreground" />
              {t('reliability.replay_workspace.title', 'Replay 工作区')}
            </CardTitle>
            <CardDescription className="mt-1">
              {t(
                'reliability.replay_workspace.description',
                '直接复制或下载当前 tenant 的 replay 工作表，并保留完整的上下文占位符。',
              )}
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" size="sm" variant="outline" onClick={handleCopy}>
              <Copy className="mr-1.5 h-3.5 w-3.5" />
              {copied
                ? t('common.copied', '已复制')
                : t('reliability.replay_workspace.copy', '复制工作表')}
            </Button>
            <Button asChild size="sm" variant="default">
              <a href={downloadHref} download={replayWorksheetDownloadName}>
                <FileDown className="mr-1.5 h-3.5 w-3.5" />
                {t('reliability.replay_workspace.download', '下载 markdown')}
              </a>
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-2 rounded-[0.9rem] border border-border/60 bg-muted/15 p-3 text-sm text-muted-foreground md:grid-cols-3">
          <div>
            <div className="text-[11px] font-semibold tracking-[0.14em] uppercase">
              {t('reliability.replay_workspace.tenant', '当前 tenant')}
            </div>
            <div className="mt-1 text-foreground">{tenantName}</div>
          </div>
          <div>
            <div className="text-[11px] font-semibold tracking-[0.14em] uppercase">
              {t('reliability.replay_workspace.dashboard', 'Dashboard')}
            </div>
            <div className="mt-1 break-all text-foreground">{dashboardHref}</div>
          </div>
          <div>
            <div className="text-[11px] font-semibold tracking-[0.14em] uppercase">
              {t('reliability.replay_workspace.mode', '导出模式')}
            </div>
            <div className="mt-1 text-foreground">
              {t('reliability.replay_workspace.mode_value', '可复制 / 可下载 / 可审阅')}
            </div>
          </div>
        </div>
        <div className="overflow-hidden rounded-[1rem] border border-border/60 bg-background/92">
          <div className="flex items-center justify-between gap-3 border-b border-border/60 bg-muted/20 px-4 py-2.5">
            <div className="text-xs font-semibold tracking-[0.14em] text-muted-foreground uppercase">
              {t('reliability.replay_workspace.preview', 'Markdown 预览')}
            </div>
            <div className="text-xs text-muted-foreground">
              {t('reliability.replay_workspace.preview_hint', '这就是下载文件的完整内容')}
            </div>
          </div>
          <textarea
            aria-label={t(
              'reliability.replay_workspace.preview_label',
              'Replay 工作表 Markdown 预览',
            )}
            className="h-[28rem] w-full resize-none overflow-auto border-0 bg-background px-4 py-3 font-mono text-xs leading-5 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            readOnly
            value={worksheetMarkdown}
          />
        </div>
      </CardContent>
    </Card>
  )
}
