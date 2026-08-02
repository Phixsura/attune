import { CalendarClock, Loader2, Sparkles } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type {
  ApplyFeedbackAssignmentRecommendationsRequest,
  FeedbackAssignmentRecommendation,
  RecommendFeedbackAssignmentResponse,
} from '@/proto/attune/v1/ingest'
import type { Member } from '@/proto/attune/v1/member'

const OWNER_KEEP = 'keep'
const MEMBER_PREFIX = 'member:'
const MAX_NOTE_LENGTH = 850

interface AssignmentRecommendationDialogProps {
  open: boolean
  count: number
  response?: RecommendFeedbackAssignmentResponse
  members: Member[]
  isMembersLoading: boolean
  isPreviewLoading: boolean
  isApplying: boolean
  onConfirm: (request: Omit<ApplyFeedbackAssignmentRecommendationsRequest, 'feedbackIds'>) => void
  onCancel: () => void
}

export function AssignmentRecommendationDialog({
  open,
  count,
  response,
  members,
  isMembersLoading,
  isPreviewLoading,
  isApplying,
  onConfirm,
  onCancel,
}: AssignmentRecommendationDialogProps) {
  const { t } = useTranslation()
  const [ownerMode, setOwnerMode] = useState(OWNER_KEEP)
  const [note, setNote] = useState('')
  const recommendations = response?.recommendations ?? []
  const failed = response?.failed ?? []
  const grouped = useMemo(() => groupRecommendations(recommendations), [recommendations])
  const assignableMembers = useMemo(
    () => members.filter((member) => member.memberType !== 'invite' && member.role !== 'viewer'),
    [members],
  )

  useEffect(() => {
    if (!open) return
    setOwnerMode(OWNER_KEEP)
    setNote('')
  }, [open])

  const canApply = !isPreviewLoading && !isApplying && recommendations.length > 0
  const confirm = () => {
    if (!canApply) return
    onConfirm({
      ownerMemberId: ownerPayload(ownerMode),
      note: note.trim(),
    })
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent showCloseButton={!isApplying} className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Sparkles className="h-5 w-5 text-primary" />
            {t('feedback.batch.recommend_title')}
          </DialogTitle>
          <DialogDescription>
            {t('feedback.batch.recommend_description', { count })}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {isPreviewLoading ? (
            <div className="flex items-center gap-2 rounded-md border border-border bg-muted/25 px-3 py-4 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              {t('feedback.batch.recommend_loading')}
            </div>
          ) : (
            <>
              <div className="grid gap-2 sm:grid-cols-3">
                <RecommendationMetric
                  label={t('feedback.batch.recommend_matched')}
                  value={String(response?.totalMatched ?? count)}
                />
                <RecommendationMetric
                  label={t('feedback.batch.recommend_ready')}
                  value={String(recommendations.length)}
                />
                <RecommendationMetric
                  label={t('feedback.batch.recommend_failed')}
                  value={String(failed.length)}
                />
              </div>

              {grouped.length > 0 ? (
                <div className="divide-y divide-border rounded-md border border-border">
                  {grouped.map((group) => (
                    <div key={group.key} className="grid gap-2 p-3 sm:grid-cols-[1fr_auto]">
                      <div className="min-w-0">
                        <div className="text-sm font-medium text-foreground">{group.name}</div>
                        <p className="mt-1 text-xs leading-5 text-muted-foreground">
                          {t('feedback.batch.recommend_rule_summary', {
                            count: group.count,
                            lane: group.ownerLane,
                            hours: group.slaHours,
                          })}
                        </p>
                      </div>
                      <div className="text-xs text-muted-foreground sm:text-right">
                        {t('feedback.batch.recommend_next_due', {
                          value: formatDate(group.nextDueAt),
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="rounded-md border border-dashed border-border px-3 py-6 text-center text-sm text-muted-foreground">
                  {t('feedback.batch.recommend_empty')}
                </div>
              )}

              {failed.length > 0 ? (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
                  {t('feedback.batch.recommend_failure_hint', { count: failed.length })}
                </div>
              ) : null}
            </>
          )}

          <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
            <div className="grid gap-2">
              <label
                htmlFor="feedback-policy-owner"
                className="text-xs font-medium text-muted-foreground"
              >
                {t('feedback.batch.recommend_owner')}
              </label>
              <Select
                value={ownerMode}
                onValueChange={setOwnerMode}
                disabled={isApplying || isMembersLoading}
              >
                <SelectTrigger
                  id="feedback-policy-owner"
                  aria-label={t('feedback.batch.recommend_owner')}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={OWNER_KEEP}>
                    {t('feedback.batch.recommend_owner_keep')}
                  </SelectItem>
                  {assignableMembers.map((member) => (
                    <SelectItem key={member.id} value={`${MEMBER_PREFIX}${member.id}`}>
                      {member.email || member.userId || member.id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-2">
              <label
                htmlFor="feedback-policy-note"
                className="text-xs font-medium text-muted-foreground"
              >
                {t('feedback.batch.recommend_note')}
              </label>
              <textarea
                id="feedback-policy-note"
                value={note}
                onChange={(event) => setNote(event.target.value)}
                disabled={isApplying}
                maxLength={MAX_NOTE_LENGTH}
                className="min-h-20 w-full resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
                placeholder={t('feedback.batch.recommend_note_placeholder')}
              />
            </div>
          </div>
        </div>

        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline" disabled={isApplying}>
              {t('common.cancel')}
            </Button>
          </DialogClose>
          <Button onClick={confirm} disabled={!canApply}>
            {isApplying ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {t('common.processing')}
              </>
            ) : (
              <>
                <CalendarClock className="mr-2 h-4 w-4" />
                {t('feedback.batch.recommend_apply')}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function RecommendationMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border bg-muted/20 px-3 py-2">
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold text-foreground">{value}</div>
    </div>
  )
}

function groupRecommendations(items: FeedbackAssignmentRecommendation[]) {
  const groups = new Map<
    string,
    {
      key: string
      name: string
      ownerLane: string
      slaHours: number
      count: number
      nextDueAt?: string
    }
  >()
  for (const item of items) {
    const key = item.ruleKey || item.ruleName
    const current = groups.get(key)
    const nextDueAt = earlierDate(current?.nextDueAt, item.recommendedSlaDueAt)
    groups.set(key, {
      key,
      name: item.ruleName || item.ruleKey,
      ownerLane: item.ownerLane,
      slaHours: Number(item.slaHours ?? 0),
      count: (current?.count ?? 0) + 1,
      nextDueAt,
    })
  }
  return Array.from(groups.values())
}

function earlierDate(current?: string, next?: string): string | undefined {
  if (!current) return next
  if (!next) return current
  return new Date(next).getTime() < new Date(current).getTime() ? next : current
}

function formatDate(value?: string): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

function ownerPayload(ownerMode: string): string | undefined {
  if (ownerMode.startsWith(MEMBER_PREFIX)) {
    return ownerMode.slice(MEMBER_PREFIX.length)
  }
  return undefined
}
