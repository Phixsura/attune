import { FlaskConical, History, Loader2, RotateCcw, Route, Save } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type {
  DryRunFeedbackAssignmentPolicyRequest,
  DryRunFeedbackAssignmentPolicyResponse,
  FeedbackAssignmentPolicy,
  FeedbackAssignmentPolicyRevision,
  FeedbackAssignmentPolicyRule,
  RestoreFeedbackAssignmentPolicyRequest,
  UpdateFeedbackAssignmentPolicyRequest,
} from '@/proto/attune/v1/ingest'
import type { Member } from '@/proto/attune/v1/member'

const NO_OWNER = 'none'
const MAX_SLA_HOURS = 720

interface AssignmentPolicyPanelProps {
  policy?: FeedbackAssignmentPolicy
  members: Member[]
  canEdit: boolean
  isLoading: boolean
  isMembersLoading: boolean
  isSaving: boolean
  isPreviewing: boolean
  isRestoring: boolean
  previewFeedbackIds: string[]
  dryRun?: DryRunFeedbackAssignmentPolicyResponse
  revisions?: FeedbackAssignmentPolicyRevision[]
  onSave: (request: UpdateFeedbackAssignmentPolicyRequest) => void
  onDryRun: (request: DryRunFeedbackAssignmentPolicyRequest) => void
  onRestore: (request: RestoreFeedbackAssignmentPolicyRequest) => void
}

export function AssignmentPolicyPanel({
  policy,
  members,
  canEdit,
  isLoading,
  isMembersLoading,
  isSaving,
  isPreviewing,
  isRestoring,
  previewFeedbackIds,
  dryRun,
  revisions,
  onSave,
  onDryRun,
  onRestore,
}: AssignmentPolicyPanelProps) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<FeedbackAssignmentPolicyRule[]>([])
  const [note, setNote] = useState('')
  const assignableMembers = useMemo(
    () => members.filter((member) => member.memberType !== 'invite' && member.role !== 'viewer'),
    [members],
  )
  const ownerLabels = useMemo(() => memberLabelMap(assignableMembers), [assignableMembers])

  useEffect(() => {
    setRules(policy?.rules ?? [])
    setNote('')
  }, [policy])

  const saveDisabled = !canEdit || isLoading || isSaving || rules.length === 0
  const previewDisabled =
    !canEdit ||
    isLoading ||
    isSaving ||
    isPreviewing ||
    rules.length === 0 ||
    previewFeedbackIds.length === 0
  const recentRevisions = (revisions ?? []).slice(0, 3)
  const updateRule = (ruleKey: string, patch: Partial<FeedbackAssignmentPolicyRule>) => {
    setRules((current) =>
      current.map((rule) => (rule.ruleKey === ruleKey ? { ...rule, ...patch } : rule)),
    )
  }
  const save = () => {
    if (saveDisabled) return
    onSave({ rules, note: note.trim() })
  }
  const preview = () => {
    if (previewDisabled) return
    onDryRun({ rules, feedbackIds: previewFeedbackIds })
  }

  return (
    <Card className="rounded-[0.95rem] border-border/45 bg-card shadow-none">
      <CardHeader className="gap-0 px-4 py-4 sm:px-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Route className="size-4 text-primary" />
              {t('feedback.assignment_policy.title')}
            </CardTitle>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              {t('feedback.assignment_policy.description')}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {policy?.version ? (
              <span className="rounded-md border border-border/70 px-2 py-1 text-xs text-muted-foreground">
                {t('feedback.assignment_policy.version', { version: policy.version })}
              </span>
            ) : null}
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={preview}
              disabled={previewDisabled}
            >
              {isPreviewing ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <FlaskConical className="size-3.5" />
              )}
              {t('feedback.assignment_policy.preview')}
            </Button>
            <Button type="button" size="sm" onClick={save} disabled={saveDisabled}>
              {isSaving ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Save className="size-3.5" />
              )}
              {t('feedback.assignment_policy.save')}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 px-4 pb-4 sm:px-5">
        {isLoading ? (
          <div className="rounded-md border border-border bg-muted/20 px-3 py-4 text-sm text-muted-foreground">
            {t('feedback.assignment_policy.loading')}
          </div>
        ) : (
          <div className="space-y-2">
            {rules.map((rule) => (
              <div
                key={rule.ruleKey}
                className="grid min-w-0 gap-3 rounded-md border border-border/70 bg-muted/10 p-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,0.75fr)_minmax(0,7rem)_minmax(0,1fr)]"
              >
                <div className="flex min-w-0 items-start gap-2">
                  <Checkbox
                    checked={rule.enabled}
                    disabled={!canEdit || isSaving}
                    onCheckedChange={(checked) =>
                      updateRule(rule.ruleKey, { enabled: checked === true })
                    }
                    aria-label={t('feedback.assignment_policy.enabled_label', {
                      name: rule.ruleName,
                    })}
                  />
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-foreground">
                      {rule.ruleName}
                    </span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {rule.severity}
                    </span>
                  </span>
                </div>

                <Input
                  value={rule.ownerLane}
                  disabled={!canEdit || isSaving}
                  aria-label={t('feedback.assignment_policy.owner_lane', {
                    name: rule.ruleName,
                  })}
                  onChange={(event) =>
                    updateRule(rule.ruleKey, { ownerLane: event.target.value.trim() })
                  }
                />

                <Input
                  type="number"
                  min={1}
                  max={MAX_SLA_HOURS}
                  value={rule.slaHours}
                  disabled={!canEdit || isSaving}
                  aria-label={t('feedback.assignment_policy.sla_hours', {
                    name: rule.ruleName,
                  })}
                  onChange={(event) =>
                    updateRule(rule.ruleKey, { slaHours: Number(event.target.value) })
                  }
                />

                <Select
                  value={rule.defaultOwnerMemberId || NO_OWNER}
                  disabled={!canEdit || isSaving || isMembersLoading}
                  onValueChange={(value) =>
                    updateRule(rule.ruleKey, {
                      defaultOwnerMemberId: value === NO_OWNER ? undefined : value,
                    })
                  }
                >
                  <SelectTrigger
                    aria-label={t('feedback.assignment_policy.default_owner', {
                      name: rule.ruleName,
                    })}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_OWNER}>
                      {t('feedback.assignment_policy.no_default_owner')}
                    </SelectItem>
                    {assignableMembers.map((member) => (
                      <SelectItem key={member.id} value={member.id}>
                        {ownerLabels.get(member.id) ?? member.id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            ))}
          </div>
        )}

        <div className="grid gap-2">
          <label
            htmlFor="feedback-assignment-policy-note"
            className="text-xs font-medium text-muted-foreground"
          >
            {t('feedback.assignment_policy.note')}
          </label>
          <textarea
            id="feedback-assignment-policy-note"
            value={note}
            disabled={!canEdit || isSaving}
            maxLength={500}
            onChange={(event) => setNote(event.target.value)}
            className="min-h-16 w-full resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
            placeholder={t('feedback.assignment_policy.note_placeholder')}
          />
        </div>

        {dryRun ? (
          <div className="rounded-md border border-border/70 bg-muted/10 px-3 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="text-sm font-medium text-foreground">
                {t('feedback.assignment_policy.preview_summary', {
                  changed: dryRun.changed,
                  total: dryRun.totalMatched,
                })}
              </div>
              {dryRun.failed.length > 0 ? (
                <span className="text-xs text-muted-foreground">
                  {t('feedback.assignment_policy.preview_failed', { count: dryRun.failed.length })}
                </span>
              ) : null}
            </div>
            <div className="mt-2 grid gap-1.5">
              {dryRun.impacts.slice(0, 3).map((impact) => (
                <div
                  key={impact.feedbackId}
                  className="grid gap-1 text-xs text-muted-foreground sm:grid-cols-[5rem_1fr]"
                >
                  <span className="font-medium text-foreground">#{impact.feedbackId}</span>
                  <span className="min-w-0 truncate">
                    {impact.currentOwnerLane || impact.currentRuleKey || 'none'} /{' '}
                    {impact.currentSlaHours || 0}h {' -> '}{' '}
                    {impact.draftOwnerLane || impact.draftRuleKey || 'none'} /{' '}
                    {impact.draftSlaHours || 0}h
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : null}

        {canEdit && recentRevisions.length > 0 ? (
          <div className="rounded-md border border-border/70 bg-muted/10 px-3 py-3">
            <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
              <History className="size-3.5 text-primary" />
              {t('feedback.assignment_policy.history')}
            </div>
            <div className="grid gap-2">
              {recentRevisions.map((revision) => (
                <div
                  key={revision.version}
                  className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">
                      {t('feedback.assignment_policy.version', { version: revision.version })}
                    </span>
                    {revision.updatedBy ? <span> | {revision.updatedBy}</span> : null}
                    {revision.note ? <span className="block truncate">{revision.note}</span> : null}
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={
                      !canEdit || isSaving || isRestoring || revision.version === policy?.version
                    }
                    onClick={() => onRestore({ version: revision.version, note: '' })}
                  >
                    {isRestoring ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <RotateCcw className="size-3.5" />
                    )}
                    {t('feedback.assignment_policy.restore')}
                  </Button>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}

function memberLabelMap(members: Member[]) {
  const out = new Map<string, string>()
  for (const member of members) {
    out.set(member.id, member.email || member.userId || member.id)
  }
  return out
}
