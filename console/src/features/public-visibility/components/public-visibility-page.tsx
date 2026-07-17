import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  ArrowRight,
  Bookmark,
  Check,
  ExternalLink,
  EyeOff,
  GitMerge,
  Loader2,
  RotateCcw,
  Save,
  Search,
  ShieldCheck,
  ShieldX,
  Trash2,
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
  PortalSubmissionFieldKind,
  type PortalSubmissionFormConfig,
  PublicAccessMode,
  type PublicCustomerRequestSummary,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  publicRequestDetailQuery,
  publicVisibilityPolicyQuery,
  publicVisibilityQueryKeys,
  rejectModerationSubject,
  restoreModerationSubject,
  type UpsertPublicRequestProfileRequest,
  updatePublicVisibilityPolicy,
  upsertPublicRequestProfile,
} from '@/features/public-visibility/api/public-visibility'
import {
  createPublicVisibilitySavedView,
  deletePublicVisibilitySavedView,
  publicVisibilitySavedViewsQuery,
  publicVisibilitySavedViewsQueryKey,
  updatePublicVisibilitySavedView,
} from '@/features/public-visibility/api/saved-views'
import { meQuery } from '@/features/session/api/get-me'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useDocumentTitle } from '@/hooks/use-document-title'
import type {
  PublicVisibilityViewState,
  RoadmapStatusMapping,
  SavedPublicVisibilityView,
} from '@/proto/attune/v1/public_visibility'

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
  roadmapStatusMapping: RoadmapStatusMapping[]
  portalSubmissionForm: PortalSubmissionFormState
}

type PortalSubmissionFormState = {
  headline: string
  description: string
  acknowledgement: string
  submitButtonLabel: string
  showPageUrl: boolean
  fields: PortalSubmissionFieldState[]
}

type PortalSubmissionFieldState = {
  key: string
  label: string
  kind: PortalSubmissionFieldKind
  required: boolean
  placeholder: string
  options: string[]
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

type PublicVisibilityModerationFilters = {
  queueView: QueueView
  surfaces: PublicSurface[]
}

type SavedViewSaveRequest =
  | {
      kind: 'create'
      name: string
      state: PublicVisibilityViewState
    }
  | {
      kind: 'update'
      id: string
      name: string
      state: PublicVisibilityViewState
    }

type SavedViewSelection = {
  selectedID: string
  filters: PublicVisibilityModerationFilters | null
}

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
  const me = useQuery(meQuery())
  useDocumentTitle(t('nav.public_visibility'))

  const canViewPolicy = permissions.can('public_policy:view')
  const canEditPolicy = permissions.can('public_policy:edit')
  const canViewModeration = permissions.can('moderation:view')
  const canTriage = permissions.can('moderation:triage')
  const canEnforce = permissions.can('moderation:enforce')
  const canViewCustomerRequests = permissions.can('customer_request:view')
  const canMergeCustomerRequests = permissions.can('customer_request:merge')

  const policyQuery = useQuery({
    ...publicVisibilityPolicyQuery(),
    enabled: canViewPolicy,
  })
  const [form, setForm] = useState<PolicyForm>(() => defaultForm())
  const [profileForm, setProfileForm] = useState<RequestProfileForm>(() => defaultProfileForm())
  const [loadedPublication, setLoadedPublication] = useState<PublicRequestPublication | null>(null)
  const [queueView, setQueueView] = useState<QueueView>('pending')
  const [surfaceFilters, setSurfaceFilters] = useState<PublicSurface[]>([])
  const [moderationDialog, setModerationDialog] = useState<ModerationDialogState | null>(null)
  const moderationQuery = useQuery({
    ...moderationSubjectsQuery({ surfaces: surfaceFilters }),
    enabled: canViewModeration,
  })
  const tenantSlug = me.data?.tenant?.slug ?? null
  const portalHref = tenantSlug ? `/portal/${encodeURIComponent(tenantSlug)}` : null
  const boardHref = tenantSlug ? `/portal/${encodeURIComponent(tenantSlug)}/requests` : null
  const roadmapHref = tenantSlug ? `/portal/${encodeURIComponent(tenantSlug)}/roadmap` : null

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
      if (tenantSlug && publication.profile?.publicSlug) {
        await queryClient.invalidateQueries({
          queryKey: publicVisibilityQueryKeys.publicRequestDetail(
            tenantSlug,
            publication.profile.publicSlug,
          ),
        })
      }
      await queryClient.invalidateQueries({ queryKey: publicVisibilityQueryKeys.moderation() })
    },
    onError: (err) => toast.error(messageOf(err)),
  })

  const subjects = moderationQuery.data ?? []
  const counts = useMemo(() => countStates(subjects), [subjects])
  const visibleSubjects = useMemo(() => filterSubjects(subjects, queueView), [subjects, queueView])
  const loadedPublicSlug = loadedPublication?.profile?.publicSlug ?? ''
  const customerRequestsHref =
    canViewCustomerRequests && loadedPublication?.profile?.requestId
      ? `/feedback/customer-requests?request_id=${encodeURIComponent(
          loadedPublication.profile.requestId,
        )}`
      : null
  const similarRequestsReady = Boolean(
    canEditPolicy &&
      tenantSlug &&
      loadedPublicSlug &&
      loadedPublication?.profile?.includedInPortal &&
      loadedPublication?.moderation?.state === ModerationState.MODERATION_STATE_APPROVED &&
      policyQuery.data?.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC &&
      policyQuery.data?.requestsEnabled,
  )
  const similarRequestsQuery = useQuery({
    ...publicRequestDetailQuery(tenantSlug ?? '', loadedPublicSlug),
    enabled: similarRequestsReady,
  })
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
                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
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

                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
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

                    <RoadmapStatusMappingCard
                      mappings={form.roadmapStatusMapping}
                      canEdit={canEditPolicy}
                      onChange={(next) =>
                        setForm((prev) => ({
                          ...prev,
                          roadmapStatusMapping: next,
                        }))
                      }
                    />

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

          {canViewPolicy && (
            <PortalSubmissionFormCard
              form={form.portalSubmissionForm}
              writeMode={form.submissionWriteMode}
              identityMode={form.submitterIdentityMode}
              canEdit={canEditPolicy}
              saving={policySaving}
              portalHref={portalHref}
              boardHref={boardHref}
              roadmapHref={roadmapHref}
              onChange={(next) =>
                setForm((prev) => ({
                  ...prev,
                  portalSubmissionForm: next,
                }))
              }
            />
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

          {canEditPolicy && loadedPublication ? (
            <SimilarRequestsCard
              boardHref={boardHref}
              customerRequestsHref={customerRequestsHref}
              canMergeCustomerRequests={canMergeCustomerRequests}
              publication={loadedPublication}
              loading={similarRequestsQuery.isPending}
              requests={similarRequestsQuery.data?.similarRequests ?? []}
              error={similarRequestsQuery.isError}
              ready={similarRequestsReady}
            />
          ) : null}
        </div>

        <div className="space-y-4">
          {canViewModeration ? (
            <PublicVisibilitySavedViewsBar
              filters={{ queueView, surfaces: surfaceFilters }}
              onApply={(next) => {
                setQueueView(next.queueView)
                setSurfaceFilters(next.surfaces)
              }}
            />
          ) : null}

          <Card className="border-border/60 shadow-none">
            <CardHeader>
              <CardTitle className="text-base">{t('public_visibility.queue_title')}</CardTitle>
              <CardDescription>{t('public_visibility.queue_help')}</CardDescription>
            </CardHeader>
            <CardContent className="pt-6">
              <ModerationSurfaceFilterRow value={surfaceFilters} onChange={setSurfaceFilters} />
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
                          {canEnforce &&
                            subject.state !== ModerationState.MODERATION_STATE_SPAM && (
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

function PublicVisibilitySavedViewsBar({
  filters,
  onApply,
}: {
  filters: PublicVisibilityModerationFilters
  onApply: (filters: PublicVisibilityModerationFilters) => void
}) {
  const { t } = useTranslation()
  const inputID = useId()
  const queryClient = useQueryClient()
  const viewsQuery = useQuery(publicVisibilitySavedViewsQuery())
  const create = useMutation({
    mutationFn: createPublicVisibilitySavedView,
    onSuccess: async (response) => {
      if (response.view?.id) {
        setSelectedID(response.view.id)
      }
      setSaveOpen(false)
      await queryClient.invalidateQueries({ queryKey: publicVisibilitySavedViewsQueryKey })
    },
    onError: (err) => toast.error(messageOf(err)),
  })
  const update = useMutation({
    mutationFn: updatePublicVisibilitySavedView,
    onSuccess: async (response) => {
      if (response.view?.id) {
        setSelectedID(response.view.id)
      }
      setSaveOpen(false)
      await queryClient.invalidateQueries({ queryKey: publicVisibilitySavedViewsQueryKey })
    },
    onError: (err) => toast.error(messageOf(err)),
  })
  const remove = useMutation({
    mutationFn: deletePublicVisibilitySavedView,
    onSuccess: async () => {
      setSelectedID('')
      await queryClient.invalidateQueries({ queryKey: publicVisibilitySavedViewsQueryKey })
    },
    onError: (err) => toast.error(messageOf(err)),
  })
  const [selectedID, setSelectedID] = useState('')
  const [saveOpen, setSaveOpen] = useState(false)
  const [name, setName] = useState('')
  const views = viewsQuery.data?.views ?? []
  const selected = views.find((view) => view.id === selectedID) ?? null
  const selectedMatchesCurrent = selected
    ? savedViewStateSignature(selected.state) === moderationFiltersSignature(filters)
    : false
  const selectedDeleteID = savedViewDeleteID(selected)
  const isSaving = create.isPending || update.isPending

  const openSaveDialog = () => {
    setName(selected?.name ?? suggestSavedViewName(filters, t))
    setSaveOpen(true)
  }

  const saveCurrent = () => {
    const request = savedViewSaveRequest(selected, name, filters)
    if (request?.kind === 'update') {
      update.mutate({ id: request.id, name: request.name, state: request.state })
      return
    }
    if (request?.kind === 'create') {
      create.mutate({ name: request.name, state: request.state })
    }
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader className="pb-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base">
              <Bookmark className="size-4 text-primary" />
              {t('public_visibility.saved_views_title')}
            </CardTitle>
            <CardDescription>{t('public_visibility.saved_views_help')}</CardDescription>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={openSaveDialog}>
              <Save className="size-4" />
              {t('public_visibility.saved_views_save')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-9"
              disabled={!selectedDeleteID || remove.isPending}
              aria-label={t('public_visibility.saved_views_delete')}
              onClick={selectedDeleteID ? () => remove.mutate(selectedDeleteID) : undefined}
            >
              {remove.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Trash2 className="size-4" />
              )}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-0">
        <div className="rounded-md border border-border/70 bg-muted/20 p-3 text-sm">
          <div className="font-medium">
            {selected
              ? t('public_visibility.saved_views.current_bound', { name: selected.name })
              : t('public_visibility.saved_views.current_unbound')}
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {selected
              ? selectedMatchesCurrent
                ? t('public_visibility.saved_views.current_in_sync')
                : t('public_visibility.saved_views.current_dirty')
              : t('public_visibility.saved_views.current_unbound_hint')}
          </div>
        </div>

        <div className="flex flex-col gap-3 md:flex-row md:items-center">
          <Select
            value={selectedID || SAVED_VIEW_NONE}
            disabled={viewsQuery.isPending}
            onValueChange={(value) => {
              const next = savedViewSelectionFromValue(views, value)
              setSelectedID(next.selectedID)
              if (next.filters) onApply(next.filters)
            }}
          >
            <SelectTrigger
              className="min-w-0 md:w-72"
              aria-label={t('public_visibility.saved_views_label')}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={SAVED_VIEW_NONE}>
                {t('public_visibility.saved_views_current')}
              </SelectItem>
              {views.length === 0 ? (
                <SelectItem value="__empty__" disabled>
                  {t('public_visibility.saved_views_empty')}
                </SelectItem>
              ) : null}
              {views.map((view) => (
                <SelectItem key={view.id} value={view.id}>
                  {view.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="min-w-0 flex-1 text-xs leading-5 text-muted-foreground">
            {describeSavedViewState(filters, t)}
          </div>
        </div>

        <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('public_visibility.saved_views_save_title')}</DialogTitle>
              <DialogDescription className="sr-only">
                {t('public_visibility.saved_views_save')}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-2">
              <Label htmlFor={inputID}>{t('public_visibility.saved_views_name')}</Label>
              <Input
                id={inputID}
                value={name}
                maxLength={80}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setSaveOpen(false)}>
                {t('common.cancel')}
              </Button>
              <Button disabled={isSaving || name.trim().length === 0} onClick={saveCurrent}>
                {isSaving ? <Loader2 className="size-4 animate-spin" /> : null}
                {t('common.save')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}

function ModerationSurfaceFilterRow({
  value,
  onChange,
}: {
  value: PublicSurface[]
  onChange: (surfaces: PublicSurface[]) => void
}) {
  const { t } = useTranslation()
  const selected = normalizeSurfaceSelection(value)

  return (
    <div className="mb-4 rounded-md border border-border/70 bg-background p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">
          {t('public_visibility.surface_filters_title')}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 px-2"
          disabled={selected.length === 0}
          onClick={() => onChange([])}
        >
          {t('public_visibility.surface_filters_clear')}
        </Button>
      </div>
      <div className="flex flex-wrap gap-2">
        {surfaceFilterOptions.map((surface) => {
          const active = selected.includes(surface)
          return (
            <Button
              key={surface}
              type="button"
              size="sm"
              variant={active ? 'default' : 'outline'}
              className="h-8 rounded-full px-3"
              onClick={() =>
                onChange(
                  active
                    ? selected.filter((item) => item !== surface)
                    : normalizeSurfaceSelection([...selected, surface]),
                )
              }
            >
              {formatSurface(t, surface)}
            </Button>
          )
        })}
      </div>
    </div>
  )
}

const SAVED_VIEW_NONE = '__current__'

const surfaceFilterOptions: PublicSurface[] = [
  PublicSurface.PUBLIC_SURFACE_REQUEST,
  PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
  PublicSurface.PUBLIC_SURFACE_ROADMAP_ITEM,
  PublicSurface.PUBLIC_SURFACE_CHANGELOG_POST,
  PublicSurface.PUBLIC_SURFACE_PORTAL_SUBMISSION,
]

function savedViewStateFromFilters(
  filters: PublicVisibilityModerationFilters,
): PublicVisibilityViewState {
  return {
    queueView: normalizeQueueView(filters.queueView),
    surfaces: normalizeSurfaceSelection(filters.surfaces),
  }
}

function savedViewStateToFilters(
  state?: PublicVisibilityViewState,
): PublicVisibilityModerationFilters {
  if (!state) {
    return { queueView: 'pending', surfaces: [] }
  }
  return {
    queueView: normalizeQueueView(state.queueView),
    surfaces: normalizeSurfaceSelection(state.surfaces ?? []),
  }
}

function savedViewSaveRequest(
  selected: SavedPublicVisibilityView | null,
  name: string,
  filters: PublicVisibilityModerationFilters,
): SavedViewSaveRequest | null {
  const trimmedName = name.trim()
  if (!trimmedName) return null
  const state = savedViewStateFromFilters(filters)
  if (selected) {
    return { kind: 'update', id: selected.id, name: trimmedName, state }
  }
  return { kind: 'create', name: trimmedName, state }
}

function savedViewDeleteID(selected: SavedPublicVisibilityView | null): string | null {
  return selected?.id || null
}

function savedViewSelectionFromValue(
  views: SavedPublicVisibilityView[],
  value: string,
): SavedViewSelection {
  if (value === SAVED_VIEW_NONE) {
    return { selectedID: '', filters: null }
  }
  const view = views.find((item) => item.id === value)
  if (!view) {
    return { selectedID: '', filters: null }
  }
  return { selectedID: view.id, filters: savedViewStateToFilters(view.state) }
}

function moderationFiltersSignature(filters: PublicVisibilityModerationFilters) {
  return `${normalizeQueueView(filters.queueView)}|${normalizeSurfaceSelection(filters.surfaces).join(',')}`
}

function savedViewStateSignature(state?: PublicVisibilityViewState) {
  if (!state) return moderationFiltersSignature({ queueView: 'pending', surfaces: [] })
  return `${normalizeQueueView(state.queueView)}|${normalizeSurfaceSelection(state.surfaces ?? []).join(',')}`
}

function normalizeQueueView(value: string) {
  switch (value.trim().toLowerCase()) {
    case 'approved':
      return 'approved'
    case 'blocked':
      return 'blocked'
    case 'all':
      return 'all'
    default:
      return 'pending'
  }
}

function normalizeSurfaceSelection(values: PublicSurface[]) {
  if (values.length === 0) return []
  const selected = new Set<PublicSurface>(
    values.filter((surface) => surface !== PublicSurface.UNRECOGNIZED),
  )
  return surfaceFilterOptions.filter((surface) => selected.has(surface))
}

function describeSavedViewState(filters: PublicVisibilityModerationFilters, t: TFunction) {
  const queueLabel = t(`public_visibility.queue_views.${normalizeQueueView(filters.queueView)}`)
  const surfaces = normalizeSurfaceSelection(filters.surfaces).map((surface) =>
    formatSurface(t, surface),
  )
  if (surfaces.length === 0) {
    return t('public_visibility.saved_views.summary_queue_only', { queue: queueLabel })
  }
  return t('public_visibility.saved_views.summary', {
    queue: queueLabel,
    surfaces: surfaces.join(' · '),
  })
}

function suggestSavedViewName(filters: PublicVisibilityModerationFilters, t: TFunction) {
  const queueLabel = t(`public_visibility.queue_views.${normalizeQueueView(filters.queueView)}`)
  const surfaces = normalizeSurfaceSelection(filters.surfaces).map((surface) =>
    formatSurface(t, surface),
  )
  if (surfaces.length === 0) {
    return queueLabel
  }
  return `${queueLabel} · ${surfaces.slice(0, 2).join(' · ')}`
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
              value={form.roadmapColumn || t('public_visibility.profile.roadmap_column_auto')}
              readOnly
              disabled
              placeholder={t('public_visibility.profile.roadmap_column_auto')}
            />
            <p className="text-xs leading-5 text-muted-foreground">
              {t('public_visibility.profile.roadmap_column_help')}
            </p>
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

function RoadmapStatusMappingCard({
  mappings,
  canEdit,
  onChange,
}: {
  mappings: RoadmapStatusMapping[]
  canEdit: boolean
  onChange: (next: RoadmapStatusMapping[]) => void
}) {
  const { t } = useTranslation()
  const rows = normalizeRoadmapStatusMappingsForForm(mappings)

  const updateRow = (status: string, patch: Partial<RoadmapStatusMapping>) => {
    onChange(
      rows.map((row) =>
        row.status === status
          ? {
              ...row,
              ...patch,
            }
          : row,
      ),
    )
  }

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/20 p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <div className="text-sm font-semibold text-foreground">
            {t('public_visibility.roadmap_mapping_title')}
          </div>
          <div className="text-sm leading-6 text-muted-foreground">
            {t('public_visibility.roadmap_mapping_help')}
          </div>
        </div>
        {canEdit ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onChange(defaultRoadmapStatusMappings())}
          >
            {t('public_visibility.roadmap_mapping_reset')}
          </Button>
        ) : null}
      </div>

      <div className="mt-4 space-y-3">
        {rows.map((mapping) => (
          <div
            key={mapping.status}
            className="grid gap-3 rounded-xl border border-border/60 bg-background p-3 md:grid-cols-[minmax(0,0.85fr)_minmax(0,1.3fr)_120px_170px]"
          >
            <div className="space-y-1">
              <div className="text-[11px] font-semibold uppercase tracking-[0.2em] text-muted-foreground">
                {t('public_visibility.roadmap_mapping.status')}
              </div>
              <div className="font-mono text-sm text-foreground">{mapping.status}</div>
              <div className="text-xs leading-5 text-muted-foreground">
                {roadmapStatusDefaultLabel(mapping.status)}
              </div>
            </div>

            <Field label={t('public_visibility.roadmap_mapping.column_label')}>
              <Input
                value={mapping.label}
                disabled={!canEdit}
                aria-label={t('public_visibility.roadmap_mapping.column_label')}
                onChange={(event) => updateRow(mapping.status, { label: event.target.value })}
                placeholder={roadmapStatusDefaultLabel(mapping.status)}
              />
            </Field>

            <Field label={t('public_visibility.roadmap_mapping.order_label')}>
              <Input
                type="number"
                min={1}
                step={1}
                value={mapping.order}
                disabled={!canEdit}
                aria-label={t('public_visibility.roadmap_mapping.order_label')}
                onChange={(event) =>
                  updateRow(mapping.status, {
                    order: numberFromInput(event.target.value, mapping.order),
                  })
                }
              />
            </Field>

            <div className="flex items-end">
              <Toggle
                label={t('public_visibility.roadmap_mapping.included')}
                checked={mapping.included}
                disabled={!canEdit}
                onChange={(included) => updateRow(mapping.status, { included })}
              />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function SimilarRequestsCard({
  boardHref,
  customerRequestsHref,
  canMergeCustomerRequests,
  publication,
  loading,
  requests,
  error,
  ready,
}: {
  boardHref: string | null
  customerRequestsHref: string | null
  canMergeCustomerRequests: boolean
  publication: PublicRequestPublication
  loading: boolean
  requests: PublicCustomerRequestSummary[]
  error: boolean
  ready: boolean
}) {
  const { t } = useTranslation()
  const title =
    publication.profile?.publicTitle || publication.profile?.publicSlug || t('common.untitled')

  return (
    <Card className="border-amber-200/70 bg-amber-50/30 shadow-none">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base">
              <Search className="h-4 w-4 text-amber-600" />
              {t('public_visibility.profile.similar_title')}
            </CardTitle>
            <CardDescription>
              {t('public_visibility.profile.similar_help', { title })}
            </CardDescription>
          </div>
          {customerRequestsHref ? (
            <Button asChild size="sm" variant="outline" className="shrink-0">
              <a href={customerRequestsHref}>
                <ArrowRight className="size-4" />
                {t('public_visibility.profile.open_customer_request')}
              </a>
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-6">
        {!ready ? (
          <div className="rounded-md border border-dashed border-amber-200 bg-white px-4 py-5 text-sm text-muted-foreground">
            {t('public_visibility.profile.similar_locked')}
          </div>
        ) : loading && requests.length === 0 ? (
          <Loading />
        ) : error && requests.length === 0 ? (
          <div className="rounded-md border border-dashed border-amber-200 bg-white px-4 py-5 text-sm text-muted-foreground">
            {t('public_visibility.profile.similar_error')}
          </div>
        ) : requests.length === 0 ? (
          <div className="rounded-md border border-dashed border-amber-200 bg-white px-4 py-5 text-sm text-muted-foreground">
            {t('public_visibility.profile.similar_empty')}
          </div>
        ) : (
          <ul className="space-y-3">
            {requests.map((request) => {
              const href = boardHref ? `${boardHref}/${encodeURIComponent(request.slug)}` : null
              const mergeHref =
                customerRequestsHref && canMergeCustomerRequests
                  ? `${customerRequestsHref}&merge_target_id=${encodeURIComponent(request.id)}`
                  : null
              const mergeLabel = t('public_visibility.profile.merge_target_action')
              return (
                <li
                  key={request.id}
                  className="rounded-2xl border border-amber-200/70 bg-white px-4 py-4 shadow-[0_12px_24px_-22px_rgba(217,119,6,0.28)]"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="text-sm font-semibold text-foreground">
                        {href ? (
                          <a
                            href={href}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 hover:underline"
                          >
                            <span className="break-words">
                              {request.title || request.slug || t('common.untitled')}
                            </span>
                            <ExternalLink className="size-3.5 shrink-0" />
                          </a>
                        ) : (
                          <span className="break-words">
                            {request.title || request.slug || t('common.untitled')}
                          </span>
                        )}
                      </div>
                      {request.summary ? (
                        <div className="break-words text-sm leading-6 text-muted-foreground">
                          {request.summary}
                        </div>
                      ) : null}
                    </div>
                    <div className="flex shrink-0 flex-col items-end gap-2 text-xs">
                      <span className="rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 font-medium text-amber-800">
                        {request.state || t('public_visibility.profile.similar_state_unknown')}
                      </span>
                      {request.roadmapColumn ? (
                        <span className="rounded-full border border-border/70 bg-muted/30 px-2.5 py-1 text-muted-foreground">
                          {request.roadmapColumn}
                        </span>
                      ) : null}
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted-foreground">
                    {typeof request.voteCount === 'number' ? (
                      <span className="rounded-full bg-muted/50 px-2.5 py-1">
                        {t('public_visibility.profile.similar_votes', { count: request.voteCount })}
                      </span>
                    ) : null}
                    {typeof request.commentCount === 'number' ? (
                      <span className="rounded-full bg-muted/50 px-2.5 py-1">
                        {t('public_visibility.profile.similar_comments', {
                          count: request.commentCount,
                        })}
                      </span>
                    ) : null}
                    {request.submittedByDisplay ? (
                      <span className="rounded-full bg-muted/50 px-2.5 py-1">
                        {t('public_visibility.profile.similar_submitter', {
                          display: request.submittedByDisplay,
                        })}
                      </span>
                    ) : null}
                    {mergeHref ? (
                      <Button asChild size="sm" variant="outline" className="h-7 shrink-0">
                        <a href={mergeHref}>
                          <GitMerge className="size-3.5" />
                          {mergeLabel}
                        </a>
                      </Button>
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}

function PortalSubmissionFormCard({
  form,
  writeMode,
  identityMode,
  canEdit,
  saving,
  portalHref,
  boardHref,
  roadmapHref,
  onChange,
}: {
  form: PortalSubmissionFormState
  writeMode: PublicWriteMode
  identityMode: PublicIdentityMode
  canEdit: boolean
  saving: boolean
  portalHref: string | null
  boardHref: string | null
  roadmapHref: string | null
  onChange: (next: PortalSubmissionFormState) => void
}) {
  const { t } = useTranslation()

  const updateField = (
    index: number,
    mutate: (field: PortalSubmissionFieldState) => PortalSubmissionFieldState,
  ) => {
    onChange({
      ...form,
      fields: form.fields.map((field, currentIndex) =>
        currentIndex === index ? mutate(field) : field,
      ),
    })
  }

  const addField = () => {
    onChange({
      ...form,
      fields: [...form.fields, defaultPortalSubmissionField(form.fields.length + 1)],
    })
  }

  const removeField = (index: number) => {
    onChange({
      ...form,
      fields: form.fields.filter((_, currentIndex) => currentIndex !== index),
    })
  }

  const moveField = (index: number, offset: number) => {
    const nextIndex = index + offset
    if (nextIndex < 0 || nextIndex >= form.fields.length) return
    const next = [...form.fields]
    const [field] = next.splice(index, 1)
    next.splice(nextIndex, 0, field)
    onChange({ ...form, fields: next })
  }

  return (
    <Card className="border-border/60 shadow-none">
      <CardHeader>
        <CardTitle className="text-base">{t('public_visibility.portal_title')}</CardTitle>
        <CardDescription>{t('public_visibility.portal_help')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-6">
        <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
          <div className="space-y-4">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <Field label={t('public_visibility.portal.headline')}>
                <Input
                  value={form.headline}
                  disabled={!canEdit || saving}
                  aria-label={t('public_visibility.portal.headline')}
                  onChange={(event) => onChange({ ...form, headline: event.target.value })}
                  placeholder={t('public_visibility.portal.headline_placeholder')}
                />
              </Field>
              <Field label={t('public_visibility.portal.submit_button_label')}>
                <Input
                  value={form.submitButtonLabel}
                  disabled={!canEdit || saving}
                  aria-label={t('public_visibility.portal.submit_button_label')}
                  onChange={(event) => onChange({ ...form, submitButtonLabel: event.target.value })}
                  placeholder={t('public_visibility.portal.submit_button_placeholder')}
                />
              </Field>
            </div>

            <Field label={t('public_visibility.portal.description')}>
              <textarea
                className="min-h-28 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
                value={form.description}
                disabled={!canEdit || saving}
                aria-label={t('public_visibility.portal.description')}
                onChange={(event) => onChange({ ...form, description: event.target.value })}
                placeholder={t('public_visibility.portal.description_placeholder')}
              />
            </Field>

            <Field label={t('public_visibility.portal.acknowledgement')}>
              <textarea
                className="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
                value={form.acknowledgement}
                disabled={!canEdit || saving}
                aria-label={t('public_visibility.portal.acknowledgement')}
                onChange={(event) => onChange({ ...form, acknowledgement: event.target.value })}
                placeholder={t('public_visibility.portal.acknowledgement_placeholder')}
              />
            </Field>

            <Toggle
              label={t('public_visibility.portal.show_page_url')}
              checked={form.showPageUrl}
              disabled={!canEdit || saving}
              onChange={(checked) => onChange({ ...form, showPageUrl: checked })}
            />

            <div className="rounded-2xl border border-border/70 bg-background/85 p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-foreground">
                    {t('public_visibility.portal.fields_title')}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t('public_visibility.portal.fields_help')}
                  </div>
                </div>
                {canEdit && (
                  <Button type="button" variant="outline" size="sm" onClick={addField}>
                    {t('public_visibility.portal.add_field')}
                  </Button>
                )}
              </div>

              <div className="mt-4 space-y-3">
                {form.fields.length === 0 ? (
                  <div className="rounded-xl border border-dashed border-border/70 bg-muted/20 px-4 py-8 text-sm text-muted-foreground">
                    {t('public_visibility.portal.fields_empty')}
                  </div>
                ) : (
                  form.fields.map((field, index) => (
                    <PortalSubmissionFieldEditor
                      key={field.key || field.label || 'field'}
                      field={field}
                      index={index}
                      canEdit={canEdit}
                      saving={saving}
                      onChange={(next) => updateField(index, () => next)}
                      onRemove={() => removeField(index)}
                      onMoveUp={() => moveField(index, -1)}
                      onMoveDown={() => moveField(index, 1)}
                    />
                  ))
                )}
              </div>
            </div>

            {canEdit ? (
              <div className="rounded-2xl border border-amber-200 bg-amber-50/80 px-4 py-3 text-sm text-amber-900">
                {t('public_visibility.portal.save_hint')}
              </div>
            ) : null}
          </div>

          <PortalSubmissionPreview
            form={form}
            writeMode={writeMode}
            identityMode={identityMode}
            fieldCount={form.fields.length}
            portalHref={portalHref}
            boardHref={boardHref}
            roadmapHref={roadmapHref}
          />
        </div>
      </CardContent>
    </Card>
  )
}

function PortalSubmissionFieldEditor({
  field,
  index,
  canEdit,
  saving,
  onChange,
  onRemove,
  onMoveUp,
  onMoveDown,
}: {
  field: PortalSubmissionFieldState
  index: number
  canEdit: boolean
  saving: boolean
  onChange: (next: PortalSubmissionFieldState) => void
  onRemove: () => void
  onMoveUp: () => void
  onMoveDown: () => void
}) {
  const { t } = useTranslation()
  const kindOptions = portalSubmissionFieldKindOptions(t)
  const kindNeedsOptions =
    field.kind === PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT ||
    field.kind === PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT

  return (
    <div className="rounded-2xl border border-border/70 bg-[linear-gradient(180deg,rgba(255,255,255,0.95),rgba(247,249,252,0.98))] p-4 shadow-[0_14px_36px_-30px_rgba(15,23,42,0.25)]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-foreground">
            {t('public_visibility.portal.field.title', { index: index + 1 })}
          </div>
          <div className="text-xs text-muted-foreground">
            {t(
              `public_visibility.portal.field.kind_values.${portalSubmissionFieldKindName(field.kind)}`,
            )}
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!canEdit || saving || index === 0}
            onClick={onMoveUp}
          >
            {t('public_visibility.portal.field.move_up')}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!canEdit || saving}
            onClick={onMoveDown}
          >
            {t('public_visibility.portal.field.move_down')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={!canEdit || saving}
            onClick={onRemove}
          >
            {t('public_visibility.portal.field.remove')}
          </Button>
        </div>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field label={t('public_visibility.portal.field.key')}>
          <Input
            value={field.key}
            disabled={!canEdit || saving}
            aria-label={t('public_visibility.portal.field.key')}
            onChange={(event) => onChange({ ...field, key: event.target.value })}
            placeholder={t('public_visibility.portal.field.key_placeholder')}
          />
        </Field>
        <Field label={t('public_visibility.portal.field.label')}>
          <Input
            value={field.label}
            disabled={!canEdit || saving}
            aria-label={t('public_visibility.portal.field.label')}
            onChange={(event) => onChange({ ...field, label: event.target.value })}
            placeholder={t('public_visibility.portal.field.label_placeholder')}
          />
        </Field>
        <Field label={t('public_visibility.portal.field.kind_label')}>
          <Select
            value={field.kind}
            disabled={!canEdit || saving}
            onValueChange={(value) =>
              onChange({
                ...field,
                kind: value as PortalSubmissionFieldKind,
                options:
                  value === PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN ||
                  value === PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT ||
                  value === PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA
                    ? []
                    : field.options,
              })
            }
          >
            <SelectTrigger
              className="w-full"
              aria-label={t('public_visibility.portal.field.kind_label')}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {kindOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t('public_visibility.portal.field.placeholder')}>
          <Input
            value={field.placeholder}
            disabled={!canEdit || saving}
            aria-label={t('public_visibility.portal.field.placeholder')}
            onChange={(event) => onChange({ ...field, placeholder: event.target.value })}
            placeholder={t('public_visibility.portal.field.placeholder_placeholder')}
          />
        </Field>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,0.8fr)]">
        <Toggle
          label={t('public_visibility.portal.field.required')}
          checked={field.required}
          disabled={!canEdit || saving}
          onChange={(checked) => onChange({ ...field, required: checked })}
        />
        <div className="rounded-xl border border-border/50 bg-background px-3 py-2 text-xs text-muted-foreground">
          {t('public_visibility.portal.field.help', {
            kind: t(
              `public_visibility.portal.field.kind_values.${portalSubmissionFieldKindName(field.kind)}`,
            ),
          })}
        </div>
      </div>

      {kindNeedsOptions && (
        <Field label={t('public_visibility.portal.field.options')}>
          <textarea
            className="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-50"
            value={field.options.join('\n')}
            disabled={!canEdit || saving}
            aria-label={t('public_visibility.portal.field.options')}
            onChange={(event) =>
              onChange({ ...field, options: parsePortalFieldOptions(event.target.value) })
            }
            placeholder={t('public_visibility.portal.field.options_placeholder')}
          />
        </Field>
      )}
    </div>
  )
}

function PortalSubmissionPreview({
  form,
  writeMode,
  identityMode,
  fieldCount,
  portalHref,
  boardHref,
  roadmapHref,
}: {
  form: PortalSubmissionFormState
  writeMode: PublicWriteMode
  identityMode: PublicIdentityMode
  fieldCount: number
  portalHref: string | null
  boardHref: string | null
  roadmapHref: string | null
}) {
  const { t } = useTranslation()

  return (
    <div className="rounded-[1.75rem] border border-border/70 bg-[radial-gradient(circle_at_top,rgba(31,111,235,0.09),transparent_36%),linear-gradient(180deg,rgba(245,247,252,0.98),rgba(255,255,255,0.99))] p-4 shadow-[0_22px_48px_-42px_rgba(15,23,42,0.35)]">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-xs font-semibold uppercase tracking-[0.24em] text-muted-foreground">
            {t('public_visibility.portal.preview_label')}
          </div>
          <div className="mt-1 text-sm text-muted-foreground">
            {t('public_visibility.portal.preview_help')}
          </div>
        </div>
        <div className="rounded-full border border-border/70 bg-background px-3 py-1 text-xs font-medium text-muted-foreground">
          {t('public_visibility.portal.preview.field_count', { count: fieldCount })}
        </div>
      </div>

      {boardHref || portalHref || roadmapHref ? (
        <div className="mt-3 flex flex-wrap justify-end gap-2">
          {boardHref ? (
            <Button asChild variant="secondary" size="sm" className="h-8 gap-1.5">
              <a href={boardHref} target="_blank" rel="noreferrer">
                <ExternalLink className="size-4" />
                {t('public_visibility.portal.preview.open_board')}
              </a>
            </Button>
          ) : null}
          {roadmapHref ? (
            <Button asChild variant="secondary" size="sm" className="h-8 gap-1.5">
              <a href={roadmapHref} target="_blank" rel="noreferrer">
                <ExternalLink className="size-4" />
                {t('public_visibility.portal.preview.open_roadmap')}
              </a>
            </Button>
          ) : null}
          {portalHref ? (
            <Button asChild variant="outline" size="sm" className="h-8 gap-1.5">
              <a href={portalHref} target="_blank" rel="noreferrer">
                <ExternalLink className="size-4" />
                {t('public_visibility.portal.preview.open_portal')}
              </a>
            </Button>
          ) : null}
        </div>
      ) : null}

      <div className="mt-4 rounded-[1.6rem] border border-border/70 bg-white px-5 py-5 shadow-[0_16px_34px_-30px_rgba(15,23,42,0.22)]">
        <div className="flex flex-wrap gap-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground">
          <span className="rounded-full border border-primary/20 bg-primary/10 px-3 py-1 text-primary">
            {t(`public_visibility.write.${portalWriteModeName(writeMode)}`)}
          </span>
          <span className="rounded-full bg-slate-100 px-3 py-1 text-slate-700">
            {t(`public_visibility.identity.${portalIdentityModeName(identityMode)}`)}
          </span>
          <span className="rounded-full bg-emerald-50 px-3 py-1 text-emerald-700">
            {form.showPageUrl
              ? t('public_visibility.portal.preview.page_url_on')
              : t('public_visibility.portal.preview.page_url_off')}
          </span>
        </div>

        <div className="mt-4">
          <p className="text-xs font-semibold uppercase tracking-[0.22em] text-primary">
            {t('public_visibility.portal.preview.eyebrow')}
          </p>
          <h2 className="mt-2 text-2xl font-semibold tracking-tight text-foreground">
            {form.headline || t('public_visibility.portal.preview.headline_fallback')}
          </h2>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">
            {form.description || t('public_visibility.portal.preview.description_fallback')}
          </p>
        </div>

        <div className="mt-5 space-y-3 rounded-2xl border border-border/60 bg-[linear-gradient(180deg,rgba(249,250,251,0.98),rgba(255,255,255,1))] p-4">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <PortalPreviewChip label={t('public_visibility.portal.preview.kind.request')} active />
            <PortalPreviewChip label={t('public_visibility.portal.preview.kind.bug')} />
            <PortalPreviewChip label={t('public_visibility.portal.preview.kind.general')} />
          </div>

          {form.showPageUrl && (
            <PortalPreviewRow
              label={t('public_visibility.portal.preview.page_url')}
              value="https://app.example.com/..."
            />
          )}

          {form.fields.length > 0 ? (
            <div className="space-y-3">
              {form.fields.map((field) => (
                <PortalPreviewField key={`${field.key}-${field.label}`} field={field} />
              ))}
            </div>
          ) : (
            <div className="rounded-xl border border-dashed border-border/70 px-4 py-6 text-sm text-muted-foreground">
              {t('public_visibility.portal.preview.no_fields')}
            </div>
          )}
        </div>

        <div className="mt-4 rounded-2xl border border-emerald-200 bg-emerald-50/70 px-4 py-3 text-sm text-emerald-900">
          {form.acknowledgement || t('public_visibility.portal.preview.acknowledgement_fallback')}
        </div>

        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
          <div className="text-sm text-muted-foreground">
            {t('public_visibility.portal.preview.identity_hint', {
              value: t(`public_visibility.identity.${portalIdentityModeName(identityMode)}`),
            })}
          </div>
          <Button type="button" disabled>
            {form.submitButtonLabel || t('public_visibility.portal.preview.submit_fallback')}
          </Button>
        </div>
      </div>
    </div>
  )
}

function PortalPreviewChip({ label, active = false }: { label: string; active?: boolean }) {
  return (
    <div
      className={`rounded-xl border px-3 py-2 text-sm font-medium ${
        active
          ? 'border-primary/25 bg-primary/10 text-primary'
          : 'border-border/70 bg-muted/20 text-muted-foreground'
      }`}
    >
      {label}
    </div>
  )
}

function PortalPreviewRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/60 bg-background px-3 py-3">
      <div className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-sm text-foreground">{value}</div>
    </div>
  )
}

function PortalPreviewField({ field }: { field: PortalSubmissionFieldState }) {
  const { t } = useTranslation()
  return (
    <div className="rounded-xl border border-border/60 bg-white px-3 py-3">
      <div className="flex items-center justify-between gap-3">
        <div className="text-sm font-medium text-foreground">
          {field.label || field.key || 'Custom field'}
          {field.required ? <span className="text-rose-500"> *</span> : null}
        </div>
        <div className="text-xs text-muted-foreground">
          {portalSubmissionFieldKindLabel(field.kind, t)}
        </div>
      </div>
      <div className="mt-2 text-sm text-muted-foreground">{field.placeholder || ' '}</div>
      {field.options.length > 0 ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {field.options.slice(0, 4).map((option) => (
            <span
              key={option}
              className="rounded-full border border-border/60 bg-muted/20 px-2.5 py-1 text-xs text-foreground"
            >
              {option}
            </span>
          ))}
          {field.options.length > 4 ? (
            <span className="rounded-full border border-border/60 bg-muted/20 px-2.5 py-1 text-xs text-muted-foreground">
              +{field.options.length - 4}
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
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
    roadmapStatusMapping: defaultRoadmapStatusMappings(),
    portalSubmissionForm: defaultPortalSubmissionForm(),
  }
}

const roadmapStatusBlueprints = [
  { status: 'open', defaultLabel: 'under consideration' },
  { status: 'planned', defaultLabel: 'planned' },
  { status: 'in_progress', defaultLabel: 'in progress' },
  { status: 'shipped', defaultLabel: 'shipped' },
  { status: 'cancelled', defaultLabel: 'cancelled' },
] as const

function defaultRoadmapStatusMappings(): RoadmapStatusMapping[] {
  return roadmapStatusBlueprints.map((item, index) => ({
    status: item.status,
    label: item.defaultLabel,
    order: index + 1,
    included: item.status !== 'cancelled',
  }))
}

function normalizeRoadmapStatusMappingsForForm(
  mappings: RoadmapStatusMapping[] | undefined,
): RoadmapStatusMapping[] {
  const byStatus = new Map((mappings ?? []).map((mapping) => [mapping.status, mapping] as const))
  return roadmapStatusBlueprints.map((item, index) => {
    const mapping = byStatus.get(item.status)
    const order = mapping?.order ?? 0
    return {
      status: item.status,
      label: mapping?.label?.trim() || item.defaultLabel,
      order: Number.isFinite(order) && order > 0 ? order : index + 1,
      included: mapping?.included ?? item.status !== 'cancelled',
    }
  })
}

function roadmapStatusDefaultLabel(status: string) {
  return roadmapStatusBlueprints.find((item) => item.status === status)?.defaultLabel ?? status
}

function roadmapStatusMappingsRequestFromForm(mappings: RoadmapStatusMapping[]) {
  return normalizeRoadmapStatusMappingsForForm(mappings).map((mapping) => ({
    status: mapping.status,
    label: mapping.label.trim(),
    order: mapping.order,
    included: mapping.included,
  }))
}

function numberFromInput(raw: string, fallback: number) {
  const parsed = Number.parseInt(raw, 10)
  if (Number.isFinite(parsed) && parsed > 0) {
    return parsed
  }
  return fallback
}

function defaultPortalSubmissionForm(): PortalSubmissionFormState {
  return {
    headline: 'Send feedback',
    description: 'Share bugs, ideas, or anything blocking your work.',
    acknowledgement: 'Thanks. We will review your submission.',
    submitButtonLabel: 'Submit feedback',
    showPageUrl: true,
    fields: [],
  }
}

function defaultPortalSubmissionField(index = 1): PortalSubmissionFieldState {
  return {
    key: `custom_field_${index}`,
    label: `Custom field ${index}`,
    kind: PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
    required: false,
    placeholder: 'Additional context',
    options: [],
  }
}

function parsePortalFieldOptions(raw: string): string[] {
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
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
    roadmapStatusMapping: normalizeRoadmapStatusMappingsForForm(policy.roadmapStatusMapping),
    portalSubmissionForm: portalSubmissionFormFromPolicy(policy.portalSubmissionForm),
  }
}

function portalSubmissionFormFromPolicy(
  form: PortalSubmissionFormConfig | undefined,
): PortalSubmissionFormState {
  const defaults = defaultPortalSubmissionForm()
  if (!form) return defaults
  return {
    headline: form.headline || defaults.headline,
    description: form.description || defaults.description,
    acknowledgement: form.acknowledgement || defaults.acknowledgement,
    submitButtonLabel: form.submitButtonLabel || defaults.submitButtonLabel,
    showPageUrl: form.showPageUrl,
    fields: (form.fields ?? []).map((field) => ({
      key: field.key,
      label: field.label,
      kind: field.kind,
      required: field.required,
      placeholder: field.placeholder,
      options: [...(field.options ?? [])],
    })),
  }
}

function portalSubmissionFormRequestFromForm(
  form: PortalSubmissionFormState,
): PortalSubmissionFormConfig {
  return {
    headline: form.headline.trim(),
    description: form.description.trim(),
    acknowledgement: form.acknowledgement.trim(),
    submitButtonLabel: form.submitButtonLabel.trim(),
    showPageUrl: form.showPageUrl,
    fields: form.fields.map((field) => ({
      key: field.key.trim().toLowerCase(),
      label: field.label.trim(),
      kind: field.kind,
      required: field.required,
      placeholder: field.placeholder.trim(),
      options: field.options.map((option) => option.trim()).filter(Boolean),
    })),
  }
}

function policyRequestFromForm(form: PolicyForm) {
  return {
    portalAccessMode: form.portalAccessMode,
    searchIndexingEnabled: form.searchIndexingEnabled,
    requestsEnabled: form.requestsEnabled,
    commentsEnabled: form.commentsEnabled,
    roadmapEnabled: form.roadmapEnabled,
    changelogEnabled: form.changelogEnabled,
    submissionWriteMode: form.submissionWriteMode,
    commentWriteMode: form.commentWriteMode,
    voteWriteMode: form.voteWriteMode,
    defaultRequestState: form.defaultRequestState,
    defaultCommentState: form.defaultCommentState,
    submitterIdentityMode: form.submitterIdentityMode,
    showVoteCount: form.showVoteCount,
    showCommentCount: form.showCommentCount,
    showSubmitterDisplay: form.showSubmitterDisplay,
    hidePublicTimestamps: form.hidePublicTimestamps,
    roadmapStatusMapping: roadmapStatusMappingsRequestFromForm(form.roadmapStatusMapping),
    portalSubmissionForm: portalSubmissionFormRequestFromForm(form.portalSubmissionForm),
  }
}

function portalSubmissionFieldKindName(kind: PortalSubmissionFieldKind) {
  switch (kind) {
    case PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA:
      return 'textarea'
    case PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT:
      return 'select'
    case PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT:
      return 'multiselect'
    case PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN:
      return 'boolean'
    default:
      return 'text'
  }
}

function portalSubmissionFieldKindLabel(kind: PortalSubmissionFieldKind, t: TFunction) {
  return t(`public_visibility.portal.field.kind_values.${portalSubmissionFieldKindName(kind)}`)
}

function portalSubmissionFieldKindOptions(t: TFunction) {
  return [
    PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXT,
    PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_TEXTAREA,
    PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_SELECT,
    PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_MULTISELECT,
    PortalSubmissionFieldKind.PORTAL_SUBMISSION_FIELD_KIND_BOOLEAN,
  ].map((value) => ({
    value,
    label: portalSubmissionFieldKindLabel(value, t),
  }))
}

function portalWriteModeName(mode: PublicWriteMode) {
  switch (mode) {
    case PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS:
      return 'anonymous'
    case PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED:
      return 'identified'
    default:
      return 'disabled'
  }
}

function portalIdentityModeName(mode: PublicIdentityMode) {
  switch (mode) {
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME:
      return 'display_name'
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION:
      return 'organization'
    default:
      return 'anonymous'
  }
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
  defaultRoadmapStatusMappings,
  defaultPortalSubmissionForm,
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
  roadmapStatusDefaultLabel,
  roadmapStatusMappingsRequestFromForm,
  normalizeRoadmapStatusMappingsForForm,
  numberFromInput,
  describeSavedViewState,
  portalSubmissionFieldKindLabel,
  portalSubmissionFieldKindName,
  portalSubmissionFieldKindOptions,
  portalSubmissionFormFromPolicy,
  portalSubmissionFormRequestFromForm,
  savedViewStateFromFilters,
  savedViewDeleteID,
  savedViewSaveRequest,
  savedViewSelectionFromValue,
  savedViewStateSignature,
  savedViewStateToFilters,
  reasonCodeForAction,
  reasonOptionsForAction,
  suggestSavedViewName,
  normalizeQueueView,
  normalizeSurfaceSelection,
}
