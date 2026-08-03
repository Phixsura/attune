import { CalendarClock, Loader2, UserRound } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { BatchAssignFeedbackRequest } from '@/proto/attune/v1/ingest'
import type { Member } from '@/proto/attune/v1/member'

const OWNER_KEEP = 'keep'
const OWNER_CLEAR = 'clear'
const SLA_KEEP = 'keep'
const SLA_CLEAR = 'clear'
const SLA_SET = 'set'
const MEMBER_PREFIX = 'member:'

interface BatchAssignmentDialogProps {
  open: boolean
  count: number
  members: Member[]
  isMembersLoading: boolean
  isLoading: boolean
  onConfirm: (request: Omit<BatchAssignFeedbackRequest, 'feedbackIds'>) => void
  onCancel: () => void
}

export function BatchAssignmentDialog({
  open,
  count,
  members,
  isMembersLoading,
  isLoading,
  onConfirm,
  onCancel,
}: BatchAssignmentDialogProps) {
  const { t } = useTranslation()
  const [ownerMode, setOwnerMode] = useState(OWNER_KEEP)
  const [slaMode, setSLAMode] = useState(SLA_KEEP)
  const [dueAt, setDueAt] = useState('')
  const [note, setNote] = useState('')
  const assignableMembers = useMemo(
    () => members.filter((member) => member.memberType !== 'invite' && member.role !== 'viewer'),
    [members],
  )

  useEffect(() => {
    if (!open) return
    setOwnerMode(OWNER_KEEP)
    setSLAMode(SLA_KEEP)
    setDueAt('')
    setNote('')
  }, [open])

  const canSubmit =
    !isLoading &&
    count > 0 &&
    (ownerMode !== OWNER_KEEP || slaMode !== SLA_KEEP || note.trim() !== '') &&
    (slaMode !== SLA_SET || dueAt !== '')

  const confirm = () => {
    if (!canSubmit) return
    onConfirm({
      ownerMemberIdSet: ownerMode !== OWNER_KEEP,
      ownerMemberId: ownerPayload(ownerMode),
      slaDueAtSet: slaMode !== SLA_KEEP,
      slaDueAt: slaPayload(slaMode, dueAt),
      note: note.trim(),
    })
  }

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent showCloseButton={!isLoading} className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserRound className="h-5 w-5 text-primary" />
            {t('feedback.batch.assign_title')}
          </DialogTitle>
          <DialogDescription>{t('feedback.batch.assign_description', { count })}</DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <label
              htmlFor="feedback-batch-owner"
              className="text-xs font-medium text-muted-foreground"
            >
              {t('feedback.batch.assign_owner')}
            </label>
            <Select
              value={ownerMode}
              onValueChange={setOwnerMode}
              disabled={isLoading || isMembersLoading}
            >
              <SelectTrigger
                id="feedback-batch-owner"
                aria-label={t('feedback.batch.assign_owner')}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={OWNER_KEEP}>{t('feedback.batch.assign_owner_keep')}</SelectItem>
                <SelectItem value={OWNER_CLEAR}>
                  {t('feedback.batch.assign_owner_clear')}
                </SelectItem>
                {assignableMembers.map((member) => (
                  <SelectItem key={member.id} value={`${MEMBER_PREFIX}${member.id}`}>
                    {member.email || member.userId || member.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {assignableMembers.length === 0 && !isMembersLoading ? (
              <p className="text-xs text-muted-foreground">
                {t('feedback.batch.assign_no_members')}
              </p>
            ) : null}
          </div>

          <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_15rem]">
            <div className="grid gap-2">
              <label
                htmlFor="feedback-batch-sla-mode"
                className="text-xs font-medium text-muted-foreground"
              >
                {t('feedback.batch.assign_sla')}
              </label>
              <Select value={slaMode} onValueChange={setSLAMode} disabled={isLoading}>
                <SelectTrigger
                  id="feedback-batch-sla-mode"
                  aria-label={t('feedback.batch.assign_sla')}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={SLA_KEEP}>{t('feedback.batch.assign_sla_keep')}</SelectItem>
                  <SelectItem value={SLA_CLEAR}>{t('feedback.batch.assign_sla_clear')}</SelectItem>
                  <SelectItem value={SLA_SET}>{t('feedback.batch.assign_sla_set')}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-2">
              <label
                htmlFor="feedback-batch-due-at"
                className="text-xs font-medium text-muted-foreground"
              >
                {t('feedback.batch.assign_due_at')}
              </label>
              <Input
                id="feedback-batch-due-at"
                type="datetime-local"
                value={dueAt}
                onChange={(event) => setDueAt(event.target.value)}
                disabled={isLoading || slaMode !== SLA_SET}
              />
            </div>
          </div>

          <div className="grid gap-2">
            <label
              htmlFor="feedback-batch-note"
              className="text-xs font-medium text-muted-foreground"
            >
              {t('feedback.batch.assign_note')}
            </label>
            <textarea
              id="feedback-batch-note"
              value={note}
              onChange={(event) => setNote(event.target.value)}
              disabled={isLoading}
              maxLength={1000}
              className="min-h-24 w-full resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
              placeholder={t('feedback.batch.assign_note_placeholder')}
            />
          </div>
        </div>

        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline" disabled={isLoading}>
              {t('common.cancel')}
            </Button>
          </DialogClose>
          <Button onClick={confirm} disabled={!canSubmit}>
            {isLoading ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {t('common.processing')}
              </>
            ) : (
              <>
                <CalendarClock className="mr-2 h-4 w-4" />
                {t('feedback.batch.assign_apply')}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ownerPayload(ownerMode: string): string | undefined {
  if (ownerMode === OWNER_KEEP) {
    return undefined
  }
  if (ownerMode === OWNER_CLEAR) {
    return ''
  }
  if (ownerMode.startsWith(MEMBER_PREFIX)) {
    return ownerMode.slice(MEMBER_PREFIX.length)
  }
  return undefined
}

function slaPayload(slaMode: string, dueAt: string): string | undefined {
  if (slaMode === SLA_KEEP) {
    return undefined
  }
  if (slaMode === SLA_CLEAR) {
    return ''
  }
  return localDateTimeToISOString(dueAt)
}

function localDateTimeToISOString(value: string): string {
  const normalized = value.length === 16 ? `${value}:00` : value
  return new Date(normalized).toISOString()
}
