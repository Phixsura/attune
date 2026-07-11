import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  Check,
  EyeOff,
  Loader2,
  RotateCcw,
  Search,
  ShieldCheck,
  ShieldX,
  Undo2,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useEffect, useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  approveModerationSubject,
  getPublicRequestProfile,
  hideModerationSubject,
  ModerationState,
  type ModerationSubject,
  markModerationSubjectSpam,
  moderationSubjectsQuery,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  publicVisibilityPolicyQuery,
  publicVisibilityQueryKeys,
  rejectModerationSubject,
  restoreModerationSubject,
  type UpsertPublicRequestProfileRequest,
  updatePublicVisibilityPolicy,
  upsertPublicRequestProfile,
} from '@/features/public-visibility/api/public-visibility'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useDocumentTitle } from '@/hooks/use-document-title'

type PolicyForm = {
  portalAccessMode: PublicAccessMode
  searchIndexingEnabled: boolean
  requestsEnabled: boolean
  commentsEnabled: boolean
  roadmapEnabled: boolean
  changelogEnabled: boolean
  submissionWriteMode: PublicWriteMode
  commentWriteMode: PublicWriteMode
  voteWriteMode: PublicWriteMode
  defaultRequestState: ModerationState
  defaultCommentState: ModerationState
  submitterIdentityMode: PublicIdentityMode
  showVoteCount: boolean
  showCommentCount: boolean
  showSubmitterDisplay: boolean
  hidePublicTimestamps: boolean
}

type RequestProfileForm = {
  requestId: string
  publicSlug: string
  publicTitle: string
  publicSummary: string
  publicState: string
  roadmapColumn: string
  includedInPortal: boolean
  includedInRoadmap: boolean
  submittedByDisplay: string
}

type QueueView = 'pending' | 'approved' | 'blocked' | 'all'
type ModerateAction = 'approve' | 'reject' | 'hide' | 'spam' | 'restore'

type ModerationDialogState = {
  subject: ModerationSubject
  action: ModerateAction
  reasonCode: string
  reasonNote: string
}

export function PublicVisibilityPage() {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const queryClient = useQueryClient()
  useDocumentTitle(t('nav.public_visibility'))

  const canViewPolicy = permissions.can('public_policy:view')
  const canEditPolicy = permissions.can('public_policy:edit')
  const canViewModeration = permissions.can('moderation:view')
  const canTriage = permissions.can('moderation:triage')
  const canEnforce = permissions.can('moderation:enforce')

  const policyQuery = useQuery({
    ...publicVisibilityPolicyQuery(),
    enabled: canViewPolicy,
  })
  const moderationQuery = useQuery({
    ...moderationSubjectsQuery(),
    enabled: canViewModeration,
  })
  const [form, setForm] = useState<PolicyForm>(() => defaultForm())
  const [profileForm, setProfileForm] = useState<RequestProfileForm>(() => defaultProfileForm())
  const [loadedPublication, setLoadedPublication] = useState<PublicRequestPublication | null>(null)
  const [queueView, setQueueView] = useState<QueueView>('pending')
  const [moderationDialog, setModerationDialog] = useState<ModerationDialogState | null>(null)

  useEffect(() => {
    if (policyQuery.data) {
      setForm(formFromPolicy(policyQuery.data))
    }
  }, [policyQuery.data])

  const updatePolicy = useMutation({
    mutationFn: updatePublicVisibilityPolicy,
    onSuccess: async () => {
      toast.success(t('public_visibility.policy_saved'))
      await queryClient.invalidateQueries({ queryKey: publicVisibilityQueryKeys.policy() })
    },
    onError: (err) => toast.error(messageOf(err)),
  })

  const moderate = useMutation({
    mutationFn: async ({
      id,
      action,
      reasonCode,
      reasonNote,
    }: {
      id: string
      action: ModerateAction
      reasonCode: string
      reasonNote: string
    }) => {
      const body = { id, reasonCode, reasonNote }
      switch (action) {
        case 'approve':
          return approveModerationSubject(body)
        case 'reject':
          return rejectModerationSubject(body)
        case 'hide':
          return hideModerationSubject(body)
        case 'spam':
          return markModerationSubjectSpam(body)
        default:
          return restoreModerationSubject(body)
      }
    },
    onSuccess: async (subject) => {
      setModerationDialog(null)
      setLoadedPublication((current) => syncPublicationModeration(current, subject))
      toast.success(t('public_visibility.moderation_saved'))
      await queryClient.invalidateQueries({ queryKey: publicVisibilityQueryKeys.moderation() })
    },
    onError: (err) => toast.error(messageOf(err)),
  })

  const loadProfile = useMutation({
    mutationFn: (requestId: string) => getPublicRequestProfile(requestId.trim()),
    onSuccess: (publication) => {
      setLoadedPublication(publication)
      setProfileForm(profileFormFromPublication(publication, profileForm.requestId))
    },
    onError: (err) => toast.error(messageOf(err)),
  })

  const saveProfile = useMutation({
    mutationFn: upsertPublicRequestProfile,
    onSuccess: async (publication) => {
      setLoadedPublication(publication)
      setProfileForm(profileFormFromPublication(publication, profileForm.requestId))
      toast.success(t('public_visibility.profile_saved'))
      await queryClient.invalidateQueries({ queryKey: publicVisibilityQueryKeys.moderation() })
    },
    onError: (err) => toast.error(messageOf(err)),
  })

  const subjects = moderationQuery.data ?? []
  const counts = useMemo(() => countStates(subjects), [subjects])
  const visibleSubjects = useMemo(() => filterSubjects(subjects, queueView), [subjects, queueView])
  const policySaving = updatePolicy.isPending
  const openModerationDialog = (subject: ModerationSubject, action: ModerateAction) => {
    setModerationDialog({
      subject,
      action,
      reasonCode: reasonCodeForAction(action),
      reasonNote: '',
    })
  }

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.public_visibility')}
        subtitle={t('public_visibility.subtitle')}
        actions={
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              if (canViewPolicy) {
                void policyQuery.refetch()
              }
              if (canViewModeration) {
                void moderationQuery.refetch()
              }
            }}
          >
            <RotateCcw className="h-4 w-4" />
            {t('public_visibility.refresh')}
          </Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('public_visibility.metrics.pending')}
              value={String(counts.pending)}
              hint={t('public_visibility.metrics.pending_hint')}
            />
            <PageHeroMetric
              label={t('public_visibility.metrics.approved')}
              value={String(counts.approved)}
              hint={t('public_visibility.metrics.approved_hint')}
            />
            <PageHeroMetric
              label={t('public_visibility.metrics.hidden')}
              value={String(counts.hidden)}
              hint={t('public_visibility.metrics.hidden_hint')}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <div className="space-y-6">
          {canViewPolicy && (
            <Card className="border-border/60 shadow-none">
              <CardHeader>
                <CardTitle className="text-base">{t('public_visibility.policy_title')}</CardTitle>
                <CardDescription>{t('public_visibility.policy_help')}</CardDescription>
              </CardHeader>
              <CardContent className="space-y-5 pt-6">
                {policyQuery.isLoading ? (
                  <Loading />
                ) : (
                  <>
                    <div className="grid gap-4 md:grid-cols-2">
                      <Field label={t('public_visibility.access_mode')}>
                        <Select
                          value={form.portalAccessMode}
                          disabled={!canEditPolicy}
                          onValueChange={(value) =>
                            setForm((prev) => ({
                              ...prev,
                              portalAccessMode: value as PublicAccessMode,
                            }))
                          }
                        >
                          <SelectTrigger
                            className="w-full"
                            aria-label={t('public_visibility.access_mode')}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value={PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED}>
                              {t('public_visibility.access.disabled')}
                            </SelectItem>
                            <SelectItem value={PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC}>
                              {t('public_visibility.access.public')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>
                      <Field label={t('public_visibility.default_state')}>
                        <Select
                          value={form.defaultRequestState}
                          disabled={!canEditPolicy}
                          onValueChange={(value) =>
                            setForm((prev) => ({
                              ...prev,
                              defaultRequestState: value as ModerationState,
                            }))
                          }
                        >
                          <SelectTrigger
                            className="w-full"
                            aria-label={t('public_visibility.default_state')}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value={ModerationState.MODERATION_STATE_PENDING}>
                              {t('public_visibility.states.pending')}
                            </SelectItem>
                            <SelectItem value={ModerationState.MODERATION_STATE_APPROVED}>
                              {t('public_visibility.states.approved')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>
                      <Field label={t('public_visibility.submissions')}>
                        <WriteModeSelect
                          ariaLabel={t('public_visibility.submissions')}
                          value={form.submissionWriteMode}
                          disabled={!canEditPolicy}
                          onChange={(value) =>
                            setForm((prev) => ({ ...prev, submissionWriteMode: value }))
                          }
                        />
                      </Field>
                      <Field label={t('public_visibility.comments')}>
                        <WriteModeSelect
                          ariaLabel={t('public_visibility.comments')}
                          value={form.commentWriteMode}
                          disabled={!canEditPolicy}
                          onChange={(value) =>
                            setForm((prev) => ({ ...prev, commentWriteMode: value }))
                          }
                        />
                      </Field>
                      <Field label={t('public_visibility.votes')}>
                        <WriteModeSelect
                          ariaLabel={t('public_visibility.votes')}
                          value={form.voteWriteMode}
                          disabled={!canEditPolicy}
                          onChange={(value) =>
                            setForm((prev) => ({ ...prev, voteWriteMode: value }))
                          }
                        />
                      </Field>
                      <Field label={t('public_visibility.default_comment_state')}>
                        <Select
                          value={form.defaultCommentState}
                          disabled={!canEditPolicy}
                          onValueChange={(value) =>
                            setForm((prev) => ({
                              ...prev,
                              defaultCommentState: value as ModerationState,
                            }))
                          }
                        >
                          <SelectTrigger
                            className="w-full"
                            aria-label={t('public_visibility.default_comment_state')}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value={ModerationState.MODERATION_STATE_PENDING}>
                              {t('public_visibility.states.pending')}
                            </SelectItem>
                            <SelectItem value={ModerationState.MODERATION_STATE_APPROVED}>
                              {t('public_visibility.states.approved')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>
                      <Field label={t('public_visibility.submitter_identity')}>
                        <IdentityModeSelect
                          ariaLabel={t('public_visibility.submitter_identity')}
                          value={form.submitterIdentityMode}
                          disabled={!canEditPolicy}
                          onChange={(value) =>
                            setForm((prev) => ({ ...prev, submitterIdentityMode: value }))
                          }
                        />
                      </Field>
                    </div>

                    <div className="grid gap-3 sm:grid-cols-2">
                      <Toggle
                        label={t('public_visibility.toggles.requests')}
                        checked={form.requestsEnabled}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, requestsEnabled: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.comments')}
                        checked={form.commentsEnabled}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, commentsEnabled: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.roadmap')}
                        checked={form.roadmapEnabled}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, roadmapEnabled: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.changelog')}
                        checked={form.changelogEnabled}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, changelogEnabled: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.indexing')}
                        checked={form.searchIndexingEnabled}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, searchIndexingEnabled: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.hide_timestamps')}
                        checked={form.hidePublicTimestamps}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, hidePublicTimestamps: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.show_vote_count')}
                        checked={form.showVoteCount}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, showVoteCount: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.show_comment_count')}
                        checked={form.showCommentCount}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, showCommentCount: checked }))
                        }
                      />
                      <Toggle
                        label={t('public_visibility.toggles.show_submitter')}
                        checked={form.showSubmitterDisplay}
                        disabled={!canEditPolicy}
                        onChange={(checked) =>
                          setForm((prev) => ({ ...prev, showSubmitterDisplay: checked }))
                        }
                      />
                    </div>

                    {canEditPolicy && (
                      <div className="flex justify-end">
                        <Button
                          type="button"
                          disabled={policySaving}
                          onClick={() => updatePolicy.mutate(policyRequestFromForm(form))}
                        >
                          {policySaving ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <ShieldCheck className="h-4 w-4" />
                          )}
                          {t('public_visibility.save_policy')}
                        </Button>
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          )}

          {canEditPolicy && (
            <RequestProfileCard
              form={profileForm}
              publication={loadedPublication}
              loading={loadProfile.isPending}
              saving={saveProfile.isPending}
              onChange={setProfileForm}
              onLoad={() => loadProfile.mutate(profileForm.requestId)}
              onSave={() => saveProfile.mutate(profileRequestFromForm(profileForm))}
            />
          )}
        </div>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="text-base">{t('public_visibility.queue_title')}</CardTitle>
            <CardDescription>{t('public_visibility.queue_help')}</CardDescription>
          </CardHeader>
          <CardContent className="pt-6">
            <div className="mb-4 flex flex-wrap gap-2">
              <QueueViewButton
                active={queueView === 'pending'}
                onClick={() => setQueueView('pending')}
              >
                {t('public_visibility.queue_views.pending')} ({counts.pending})
              </QueueViewButton>
              <QueueViewButton
                active={queueView === 'approved'}
                onClick={() => setQueueView('approved')}
              >
                {t('public_visibility.queue_views.approved')} ({counts.approved})
              </QueueViewButton>
              <QueueViewButton
                active={queueView === 'blocked'}
                onClick={() => setQueueView('blocked')}
              >
                {t('public_visibility.queue_views.blocked')} ({counts.hidden})
              </QueueViewButton>
              <QueueViewButton active={queueView === 'all'} onClick={() => setQueueView('all')}>
                {t('public_visibility.queue_views.all')} ({subjects.length})
              </QueueViewButton>
            </div>
            {moderationQuery.isLoading ? (
              <Loading />
            ) : visibleSubjects.length === 0 ? (
              <EmptyState
                title={t('public_visibility.empty_title')}
                description={t('public_visibility.empty_description')}
              />
            ) : (
              <ul className="space-y-3">
                {visibleSubjects.map((subject) => (
                  <li
                    key={subject.id}
                    className="grid min-w-0 gap-3 rounded-md border border-border/70 p-3 text-sm lg:grid-cols-[minmax(0,0.75fr)_minmax(0,0.75fr)_minmax(0,1.25fr)_minmax(0,1fr)_minmax(0,0.9fr)_auto]"
                  >
                    <ModerationField
                      label={t('public_visibility.table.surface')}
                      value={formatSurface(t, subject.surface)}
                    />
                    <ModerationField
                      label={t('public_visibility.table.state')}
                      value={formatState(t, subject.state)}
                    />
                    <ModerationField
                      label={t('public_visibility.table.subject')}
                      value={subject.subjectId}
                      mono
                    />
                    <ModerationField
                      label={t('public_visibility.table.updated')}
                      value={formatDate(subject.updatedAt)}
                    />
                    <ModerationField
                      label={t('public_visibility.table.reason')}
                      value={subject.reasonCode || t('public_visibility.reason_none')}
                    />
                    <div className="min-w-0">
                      <div className="mb-1 text-xs font-medium text-muted-foreground">
                        {t('public_visibility.table.actions')}
                      </div>
                      <div className="flex flex-wrap gap-2 lg:justify-end">
                        {canTriage &&
                          subject.state === ModerationState.MODERATION_STATE_PENDING && (
                            <>
                              <IconAction
                                label={t('public_visibility.actions.approve')}
                                disabled={moderate.isPending}
                                onClick={() => openModerationDialog(subject, 'approve')}
                              >
                                <Check className="h-4 w-4" />
                              </IconAction>
                              <IconAction
                                label={t('public_visibility.actions.reject')}
                                disabled={moderate.isPending}
                                onClick={() => openModerationDialog(subject, 'reject')}
                              >
                                <ShieldX className="h-4 w-4" />
                              </IconAction>
                            </>
                          )}
                        {canEnforce &&
                          subject.state === ModerationState.MODERATION_STATE_APPROVED && (
                            <IconAction
                              label={t('public_visibility.actions.hide')}
                              disabled={moderate.isPending}
                              onClick={() => openModerationDialog(subject, 'hide')}
                            >
                              <EyeOff className="h-4 w-4" />
                            </IconAction>
                          )}
                        {canEnforce && subject.state !== ModerationState.MODERATION_STATE_SPAM && (
                          <IconAction
                            label={t('public_visibility.actions.spam')}
                            disabled={moderate.isPending}
                            onClick={() => openModerationDialog(subject, 'spam')}
                          >
                            <ShieldX className="h-4 w-4" />
                          </IconAction>
                        )}
                        {canEnforce &&
                          subject.state !== ModerationState.MODERATION_STATE_PENDING &&
                          subject.state !== ModerationState.MODERATION_STATE_APPROVED && (
                            <IconAction
                              label={t('public_visibility.actions.restore')}
                              disabled={moderate.isPending}
                              onClick={() => openModerationDialog(subject, 'restore')}
                            >
                              <Undo2 className="h-4 w-4" />
                            </IconAction>
                          )}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
      <ModerationDecisionDialog
        value={moderationDialog}
        pending={moderate.isPending}
        onChange={setModerationDialog}
        onClose={() => setModerationDialog(null)}
        onSubmit={(value) =>
          moderate.mutate({
            id: value.subject.id,
            action: value.action,
            reasonCode: value.reasonCode,
            reasonNote: value.reasonNote,
          })
        }
      />
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function ModerationDecisionDialog({
  value,
  pending,
  onChange,
  onClose,
  onSubmit,
}: {
  value: ModerationDialogState | null
  pending: boolean
  onChange: (value: ModerationDialogState | null) => void
  onClose: () => void
  onSubmit: (value: ModerationDialogState) => void
}) {
  const { t } = useTranslation()
  const reasonID = useId()
  const noteID = useId()
  const reasonOptions = value ? reasonOptionsForAction(value.action) : []
  const reasonRequired = value ? actionRequiresReason(value.action) : false
  const reasonMissing = reasonRequired && !value?.reasonCode.trim()

  return (
    <Dialog open={value !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {value
              ? t('public_visibility.decision.title', {
                  action: t(`public_visibility.actions.${value.action}`),
                })
              : t('public_visibility.decision.title_empty')}
          </DialogTitle>
          <DialogDescription>{t('public_visibility.decision.description')}</DialogDescription>
        </DialogHeader>
        {value ? (
          <div className="space-y-4">
            <div className="rounded-md border bg-muted/20 p-3 text-sm">
              <div className="text-xs font-medium text-muted-foreground">
                {t('public_visibility.table.subject')}
              </div>
              <div className="mt-1 break-all font-mono text-xs">{value.subject.subjectId}</div>
            </div>
            <div className="space-y-2">
              <Label htmlFor={reasonID}>{t('public_visibility.decision.reason_code')}</Label>
              <Select
                value={value.reasonCode}
                onValueChange={(reasonCode) => onChange({ ...value, reasonCode })}
              >
                <SelectTrigger id={reasonID} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {reasonOptions.map((reasonCode) => (
                    <SelectItem key={reasonCode} value={reasonCode}>
                      {formatReasonCode(t, reasonCode)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor={noteID}>{t('public_visibility.decision.reason_note')}</Label>
              <textarea
                id={noteID}
                className="min-h-24 w-full resize-y rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
                maxLength={1000}
                value={value.reasonNote}
                onChange={(event) => onChange({ ...value, reasonNote: event.target.value })}
                placeholder={t('public_visibility.decision.reason_note_placeholder')}
              />
            </div>
          </div>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={pending} onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            type="button"
            disabled={!value || pending || reasonMissing}
            onClick={() => value && onSubmit(value)}
          >
            {pending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {t('public_visibility.decision.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function QueueViewButton({
  active,
  children,
  onClick,
}: {
  active: boolean
  children: ReactNode
  onClick: () => void
}) {
  return (
    <Button type="button" size="sm" variant={active ? 'default' : 'outline'} onClick={onClick}>
      {children}
    </Button>
  )
}

function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string
  checked: boolean
  disabled: boolean
  onChange: (checked: boolean) => void
}) {
  const id = useId()

  return (
    <div className="flex min-h-10 items-center gap-3 rounded-md border border-border/70 px-3 py-2 text-sm">
      <Checkbox
        id={id}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onChange(value === true)}
      />
      <Label htmlFor={id} className="font-normal">
        {label}
      </Label>
    </div>
  )
}

function WriteModeSelect({
  ariaLabel,
  value,
  disabled,
  onChange,
}: {
  ariaLabel: string
  value: PublicWriteMode
  disabled: boolean
  onChange: (value: PublicWriteMode) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => onChange(next as PublicWriteMode)}
    >
      <SelectTrigger className="w-full" aria-label={ariaLabel}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED}>
          {t('public_visibility.write.disabled')}
        </SelectItem>
        <SelectItem value={PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS}>
          {t('public_visibility.write.anonymous')}
        </SelectItem>
        <SelectItem value={PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED}>
          {t('public_visibility.write.identified')}
        </SelectItem>
      </SelectContent>
    </Select>
  )
}

function IdentityModeSelect({
  ariaLabel,
  value,
  disabled,
  onChange,
}: {
  ariaLabel: string
  value: PublicIdentityMode
  disabled: boolean
  onChange: (value: PublicIdentityMode) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={value}
      disabled={disabled}
      onValueChange={(next) => onChange(next as PublicIdentityMode)}
    >
      <SelectTrigger className="w-full" aria-label={ariaLabel}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS}>
          {t('public_visibility.identity.anonymous')}
        </SelectItem>
        <SelectItem value={PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME}>
          {t('public_visibility.identity.display_name')}
        </SelectItem>
        <SelectItem value={PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION}>
          {t('public_visibility.identity.organization')}
        </SelectItem>
      </SelectContent>
    </Select>
  )
}

function ModerationField({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">{label}</div>
      <div
        className={
          mono ? 'break-all font-mono text-xs text-foreground' : 'break-words text-foreground'
        }
      >
        {value}
      </div>
    </div>
  )
}

function RequestProfileCard({
  form,
  publication,
  loading,
  saving,
  onChange,
  onLoad,
  onSave,
}: {
  form: RequestProfileForm
  publication: PublicRequestPublication | null
  loading: boolean
  saving: boolean
  onChange: (next: RequestProfileForm) => void
  onLoad: () => void
  onSave: () => void
}) {
  const { t } = useTranslation()
  const profile = publication?.profile
  const moderation = publication?.moderation
  const requestIDReady = form.requestId.trim() !== ''
  const saveReady =
    requestIDReady && form.publicSlug.trim() !== '' && form.publicTitle.trim() !== ''

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('public_visibility.profile_title')}</CardTitle>
        <CardDescription>{t('public_visibility.profile_help')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        <Field label={t('public_visibility.profile.request_id')}>
          <div className="flex gap-2">
            <Input
              value={form.requestId}
              onChange={(event) => onChange({ ...form, requestId: event.target.value })}
              placeholder={t('public_visibility.profile.request_id_placeholder')}
            />
            <Button
              type="button"
              variant="outline"
              disabled={!requestIDReady || loading}
              onClick={onLoad}
            >
              {loading ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Search className="h-4 w-4" />
              )}
              {t('public_visibility.load_profile')}
            </Button>
          </div>
        </Field>

        <div className="grid gap-4 md:grid-cols-2">
          <Field label={t('public_visibility.profile.public_slug')}>
            <Input
              value={form.publicSlug}
              onChange={(event) => onChange({ ...form, publicSlug: event.target.value })}
              placeholder="pricing-api"
            />
          </Field>
          <Field label={t('public_visibility.profile.public_state')}>
            <Input
              value={form.publicState}
              onChange={(event) => onChange({ ...form, publicState: event.target.value })}
              placeholder={t('public_visibility.profile.public_state_placeholder')}
            />
          </Field>
        </div>

        <Field label={t('public_visibility.profile.public_title')}>
          <Input
            value={form.publicTitle}
            onChange={(event) => onChange({ ...form, publicTitle: event.target.value })}
            placeholder={t('public_visibility.profile.public_title_placeholder')}
          />
        </Field>

        <Field label={t('public_visibility.profile.public_summary')}>
          <textarea
            className="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
            value={form.publicSummary}
            onChange={(event) => onChange({ ...form, publicSummary: event.target.value })}
            placeholder={t('public_visibility.profile.public_summary_placeholder')}
          />
        </Field>

        <div className="grid gap-4 md:grid-cols-2">
          <Field label={t('public_visibility.profile.roadmap_column')}>
            <Input
              value={form.roadmapColumn}
              onChange={(event) => onChange({ ...form, roadmapColumn: event.target.value })}
              placeholder={t('public_visibility.profile.roadmap_column_placeholder')}
            />
          </Field>
          <Field label={t('public_visibility.profile.submitted_by_display')}>
            <Input
              value={form.submittedByDisplay}
              onChange={(event) => onChange({ ...form, submittedByDisplay: event.target.value })}
              placeholder={t('public_visibility.profile.submitted_by_display_placeholder')}
            />
          </Field>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Toggle
            label={t('public_visibility.profile.included_in_portal')}
            checked={form.includedInPortal}
            disabled={saving}
            onChange={(checked) => onChange({ ...form, includedInPortal: checked })}
          />
          <Toggle
            label={t('public_visibility.profile.included_in_roadmap')}
            checked={form.includedInRoadmap}
            disabled={saving}
            onChange={(checked) => onChange({ ...form, includedInRoadmap: checked })}
          />
        </div>

        {(profile || moderation) && (
          <div className="rounded-md border border-border/70 px-3 py-2 text-sm text-muted-foreground">
            {profile && (
              <div>
                {t('public_visibility.profile.current_slug')}: {profile.publicSlug}
              </div>
            )}
            {moderation && (
              <div>
                {t('public_visibility.profile.current_state')}: {formatState(t, moderation.state)}
              </div>
            )}
          </div>
        )}

        <div className="flex justify-end">
          <Button type="button" disabled={!saveReady || saving} onClick={onSave}>
            {saving ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <ShieldCheck className="h-4 w-4" />
            )}
            {t('public_visibility.save_profile')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function IconAction({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string
  disabled: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function defaultForm(): PolicyForm {
  return {
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED,
    searchIndexingEnabled: false,
    requestsEnabled: false,
    commentsEnabled: false,
    roadmapEnabled: false,
    changelogEnabled: false,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS,
    showVoteCount: true,
    showCommentCount: true,
    showSubmitterDisplay: false,
    hidePublicTimestamps: false,
  }
}

function defaultProfileForm(): RequestProfileForm {
  return {
    requestId: '',
    publicSlug: '',
    publicTitle: '',
    publicSummary: '',
    publicState: '',
    roadmapColumn: '',
    includedInPortal: true,
    includedInRoadmap: false,
    submittedByDisplay: '',
  }
}

function profileFormFromPublication(
  publication: PublicRequestPublication,
  fallbackRequestId: string,
): RequestProfileForm {
  const profile = publication.profile
  if (!profile) {
    return { ...defaultProfileForm(), requestId: fallbackRequestId }
  }
  return {
    requestId: profile.requestId,
    publicSlug: profile.publicSlug,
    publicTitle: profile.publicTitle,
    publicSummary: profile.publicSummary,
    publicState: profile.publicState,
    roadmapColumn: profile.roadmapColumn,
    includedInPortal: profile.includedInPortal,
    includedInRoadmap: profile.includedInRoadmap,
    submittedByDisplay: publication.moderation?.submittedByDisplay ?? '',
  }
}

function profileRequestFromForm(form: RequestProfileForm): UpsertPublicRequestProfileRequest {
  return {
    requestId: form.requestId.trim(),
    publicSlug: form.publicSlug.trim(),
    publicTitle: form.publicTitle.trim(),
    publicSummary: form.publicSummary.trim(),
    publicState: form.publicState.trim(),
    roadmapColumn: form.roadmapColumn.trim(),
    includedInPortal: form.includedInPortal,
    includedInRoadmap: form.includedInRoadmap,
    submittedByDisplay: form.submittedByDisplay.trim(),
  }
}

function formFromPolicy(policy: PublicVisibilityPolicy): PolicyForm {
  return {
    portalAccessMode: policy.portalAccessMode,
    searchIndexingEnabled: policy.searchIndexingEnabled,
    requestsEnabled: policy.requestsEnabled,
    commentsEnabled: policy.commentsEnabled,
    roadmapEnabled: policy.roadmapEnabled,
    changelogEnabled: policy.changelogEnabled,
    submissionWriteMode: policy.submissionWriteMode,
    commentWriteMode: policy.commentWriteMode,
    voteWriteMode: policy.voteWriteMode,
    defaultRequestState: policy.defaultRequestState,
    defaultCommentState: policy.defaultCommentState,
    submitterIdentityMode: policy.submitterIdentityMode,
    showVoteCount: policy.showVoteCount,
    showCommentCount: policy.showCommentCount,
    showSubmitterDisplay: policy.showSubmitterDisplay,
    hidePublicTimestamps: policy.hidePublicTimestamps,
  }
}

function policyRequestFromForm(form: PolicyForm) {
  return form
}

function countStates(subjects: ModerationSubject[]) {
  return {
    pending: subjects.filter(
      (subject) => subject.state === ModerationState.MODERATION_STATE_PENDING,
    ).length,
    approved: subjects.filter(
      (subject) => subject.state === ModerationState.MODERATION_STATE_APPROVED,
    ).length,
    hidden: subjects.filter(
      (subject) =>
        subject.state === ModerationState.MODERATION_STATE_HIDDEN ||
        subject.state === ModerationState.MODERATION_STATE_REJECTED ||
        subject.state === ModerationState.MODERATION_STATE_SPAM,
    ).length,
  }
}

function filterSubjects(subjects: ModerationSubject[], view: QueueView) {
  switch (view) {
    case 'pending':
      return subjects.filter(
        (subject) => subject.state === ModerationState.MODERATION_STATE_PENDING,
      )
    case 'approved':
      return subjects.filter(
        (subject) => subject.state === ModerationState.MODERATION_STATE_APPROVED,
      )
    case 'blocked':
      return subjects.filter(
        (subject) =>
          subject.state === ModerationState.MODERATION_STATE_HIDDEN ||
          subject.state === ModerationState.MODERATION_STATE_REJECTED ||
          subject.state === ModerationState.MODERATION_STATE_SPAM,
      )
    default:
      return subjects
  }
}

function reasonCodeForAction(action: ModerateAction) {
  switch (action) {
    case 'approve':
      return 'operator.approved'
    case 'reject':
      return 'operator.rejected'
    case 'hide':
      return 'operator.hidden'
    case 'spam':
      return 'operator.spam'
    case 'restore':
      return 'operator.restored'
  }
}

function actionRequiresReason(action: ModerateAction) {
  return action === 'reject' || action === 'hide' || action === 'spam'
}

function reasonOptionsForAction(action: ModerateAction) {
  switch (action) {
    case 'approve':
      return ['operator.approved', 'policy.safe', 'policy.redacted']
    case 'reject':
      return ['operator.rejected', 'policy.sensitive', 'policy.out_of_scope', 'policy.low_quality']
    case 'hide':
      return ['operator.hidden', 'policy.sensitive', 'policy.outdated', 'policy.private']
    case 'spam':
      return ['operator.spam', 'abuse.spam', 'abuse.automation', 'abuse.malicious']
    case 'restore':
      return ['operator.restored', 'policy.corrected', 'appeal.accepted']
  }
}

function formatReasonCode(t: TFunction, reasonCode: string) {
  return t(`public_visibility.reason_codes.${reasonCode}`, { defaultValue: reasonCode })
}

function formatSurface(t: TFunction, surface: string) {
  return t(`public_visibility.surfaces.${surface}`, { defaultValue: surface })
}

function formatState(t: TFunction, state: string) {
  return t(`public_visibility.states.${state}`, { defaultValue: state })
}

function formatDate(raw: string) {
  if (!raw) return ''
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return raw
  return date.toLocaleString()
}

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : 'failed'
}

export function syncPublicationModeration(
  current: PublicRequestPublication | null,
  subject: ModerationSubject,
): PublicRequestPublication | null {
  if (current?.moderation?.id !== subject.id) {
    return current
  }
  return { ...current, moderation: subject }
}

export const publicVisibilityPageTestables = {
  actionRequiresReason,
  countStates,
  defaultForm,
  defaultProfileForm,
  filterSubjects,
  formatDate,
  formatReasonCode,
  formatState,
  formatSurface,
  formFromPolicy,
  messageOf,
  policyRequestFromForm,
  profileFormFromPublication,
  profileRequestFromForm,
  reasonCodeForAction,
  reasonOptionsForAction,
}
