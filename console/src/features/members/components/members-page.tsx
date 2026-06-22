import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, Plus, Search, Trash2, Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
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
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useInviteMember } from '@/features/members/api/invite-member'
import { useMembers } from '@/features/members/api/list-members'
import { useRemoveMember } from '@/features/members/api/remove-member'
import { useUpdateMemberRole } from '@/features/members/api/update-member-role'
import { RoleBadge } from '@/features/session/components/auth/role-badge'
import { type Role, usePermissions } from '@/features/session/hooks/use-permissions'
import type { Member } from '@/proto/attune/v1/member'

const ROLES: Role[] = ['admin', 'member', 'viewer']

const EMAIL_RE = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/

function isValidEmail(email: string): boolean {
  return EMAIL_RE.test(email)
}

export function MembersPage() {
  const { t, i18n } = useTranslation()
  const { data: members, isPending } = useMembers()
  const updateRole = useUpdateMemberRole()
  const removeMember = useRemoveMember()
  const inviteMember = useInviteMember()
  const { userId, role, isAdmin } = usePermissions()

  const [removeTarget, setRemoveTarget] = useState<Member | null>(null)
  const [inviteOpen, setInviteOpen] = useState(false)
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState<Role>('member')
  const [query, setQuery] = useState('')
  const [scopeFilter, setScopeFilter] = useState<'all' | 'active' | 'pending'>('all')
  const [roleFilter, setRoleFilter] = useState<'all' | Role>('all')
  const [sourceFilter, setSourceFilter] = useState<'all' | 'bootstrap' | 'idp' | 'manual'>('all')
  const allMembers = members ?? []
  const memberStats = buildMemberStats(allMembers)
  const activeAdminCount = allMembers.filter(
    (member) => member.memberType !== 'invite' && member.role === 'admin',
  ).length
  const filteredMembers = filterMembers(allMembers, {
    query,
    scopeFilter,
    roleFilter,
    sourceFilter,
    t,
  })
  const activeMembers = filteredMembers.filter((member) => member.memberType !== 'invite')
  const pendingInvites = filteredMembers.filter((member) => member.memberType === 'invite')
  const hasIdPManagedMembers = allMembers.some((member) => member.roleSource === 'idp')
  const removeTargetLabel = removeTarget ? getMemberPrimaryLabel(removeTarget, t) : ''

  const handleRoleChange = (member: Member, newRole: string) => {
    updateRole.mutate(
      { id: member.id, role: newRole },
      {
        onSuccess: () => toast.success(t('members.role_updated')),
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const handleRemove = () => {
    if (!removeTarget) return
    removeMember.mutate(removeTarget.id, {
      onSuccess: () => {
        setRemoveTarget(null)
        toast.success(t('members.removed'))
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
    })
  }

  const handleInvite = () => {
    const email = inviteEmail.trim().toLowerCase()
    if (!email) {
      toast.error(t('members.email_required'))
      return
    }
    if (!isValidEmail(email)) {
      toast.error(t('members.invalid_email'))
      return
    }
    inviteMember.mutate(
      { email, role: inviteRole },
      {
        onSuccess: () => {
          setInviteOpen(false)
          setInviteEmail('')
          setInviteRole('member')
          toast.success(t('members.invited'))
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : 'failed'),
      },
    )
  }

  const locale = i18n.language.startsWith('zh') ? zhCN : undefined

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t('members.title')}</h1>
          <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t('members.subtitle')}</p>
        </div>
        {isAdmin && (
          <Button onClick={() => setInviteOpen(true)}>
            <Plus className="mr-2 h-4 w-4" />
            {t('members.invite_button')}
          </Button>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <MetricCard
          label={t('members.metrics.total')}
          value={String(memberStats.total)}
          hint={t('members.count', { count: memberStats.total })}
        />
        <MetricCard
          label={t('members.metrics.active')}
          value={String(memberStats.active)}
          hint={t('members.metrics.active_hint')}
        />
        <MetricCard
          label={t('members.metrics.pending')}
          value={String(memberStats.pending)}
          hint={t('members.metrics.pending_hint')}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
        <Card>
          <CardHeader className="gap-3">
            <div>
              <CardTitle>{t('members.filters_title')}</CardTitle>
              <CardDescription>{t('members.filters_description')}</CardDescription>
            </div>
            <div className="flex flex-wrap gap-2">
              {(['all', 'active', 'pending'] as const).map((scope) => (
                <Button
                  key={scope}
                  type="button"
                  variant={scopeFilter === scope ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setScopeFilter(scope)}
                >
                  {t(`members.scope.${scope}`)}
                </Button>
              ))}
            </div>
          </CardHeader>
          <CardContent className="grid gap-3 md:grid-cols-3">
            <div className="relative md:col-span-3">
              <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t('members.search_placeholder')}
                className="pl-9"
                aria-label={t('members.search_aria')}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="members-role-filter">{t('members.filter_role')}</Label>
              <Select
                value={roleFilter}
                onValueChange={(value) => setRoleFilter(value as 'all' | Role)}
              >
                <SelectTrigger id="members-role-filter" aria-label={t('members.filter_role')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t('members.filter_all_roles')}</SelectItem>
                  {ROLES.map((candidateRole) => (
                    <SelectItem key={candidateRole} value={candidateRole}>
                      {t(`role.${candidateRole}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="members-source-filter">{t('members.filter_source')}</Label>
              <Select
                value={sourceFilter}
                onValueChange={(value) =>
                  setSourceFilter(value as 'all' | 'bootstrap' | 'idp' | 'manual')
                }
              >
                <SelectTrigger id="members-source-filter" aria-label={t('members.filter_source')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">{t('members.filter_all_sources')}</SelectItem>
                  {(['bootstrap', 'idp', 'manual'] as const).map((source) => (
                    <SelectItem key={source} value={source}>
                      {t(`members.source.${source}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-end">
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setQuery('')
                  setScopeFilter('all')
                  setRoleFilter('all')
                  setSourceFilter('all')
                }}
              >
                {t('members.reset_filters')}
              </Button>
            </div>
          </CardContent>
        </Card>

        <div className="grid gap-4">
          <GovernanceCallout
            title={t('members.guardrails_title')}
            body={
              activeAdminCount <= 1
                ? t('members.guardrails_last_admin')
                : t('members.guardrails_healthy', { count: activeAdminCount })
            }
            tone={activeAdminCount <= 1 ? 'warning' : 'neutral'}
          />
          {hasIdPManagedMembers ? (
            <GovernanceCallout
              title={t('members.idp_title')}
              body={t('members.idp_body')}
              tone="neutral"
            />
          ) : null}
        </div>
      </div>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t('members.active_title')}</CardTitle>
            <CardDescription>
              {t('members.active_description', {
                visible: activeMembers.length,
                total: memberStats.active,
                admins: memberStats.admins,
              })}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          {isPending ? (
            <MembersTableSkeleton loadingLabel={t('app.loading')} />
          ) : activeMembers.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('members.col_user')}</TableHead>
                  <TableHead>{t('members.col_status')}</TableHead>
                  <TableHead>{t('members.col_role')}</TableHead>
                  <TableHead>{t('members.col_source')}</TableHead>
                  <TableHead>{t('members.col_joined')}</TableHead>
                  <TableHead className="w-24 text-right">{t('members.col_actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {activeMembers.map((m) => {
                  const lockReason = getMemberLockReason({
                    member: m,
                    activeAdminCount,
                    actorRole: role,
                    actorUserId: userId ?? '',
                    isAdmin,
                    t,
                  })

                  return (
                    <TableRow key={m.id}>
                      <TableCell className="min-w-[18rem]">
                        <div className="space-y-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-medium">{getMemberPrimaryLabel(m, t)}</span>
                            {m.userId === userId && (
                              <InlineBadge tone="neutral">{t('members.you')}</InlineBadge>
                            )}
                            {m.memberType === 'invite' && (
                              <InlineBadge tone="warning">{t('members.pending')}</InlineBadge>
                            )}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {getMemberSecondaryLabel(m, t)}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="space-y-1">
                          <InlineBadge tone={m.memberType === 'invite' ? 'warning' : 'success'}>
                            {m.memberType === 'invite'
                              ? t('members.status.pending')
                              : t('members.status.active')}
                          </InlineBadge>
                          <div className="text-xs text-muted-foreground">
                            {t(`members.type.${m.memberType}`)}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        {lockReason === null ? (
                          <Select
                            value={m.role}
                            onValueChange={(v) => handleRoleChange(m, v)}
                            disabled={updateRole.isPending}
                          >
                            <SelectTrigger
                              className="w-32"
                              aria-label={t('members.role_select_aria')}
                            >
                              <SelectValue>
                                <RoleBadge role={m.role as Role} />
                              </SelectValue>
                            </SelectTrigger>
                            <SelectContent>
                              {ROLES.map((r) => (
                                <SelectItem key={r} value={r}>
                                  <RoleBadge role={r} />
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        ) : (
                          <div className="space-y-1">
                            <RoleBadge role={m.role as Role} />
                            <div className="text-xs text-muted-foreground">{lockReason}</div>
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="space-y-1">
                          <span className="text-sm">{t(`members.source.${m.roleSource}`)}</span>
                          <span className="block text-xs text-muted-foreground">
                            {t(`members.source_hint.${m.roleSource}`)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="space-y-1">
                          <span className="text-sm text-muted-foreground">
                            {m.acceptedAt && m.acceptedAt !== '0'
                              ? formatDistanceToNow(new Date(Number(m.acceptedAt) * 1000), {
                                  addSuffix: true,
                                  locale,
                                })
                              : t('members.pending')}
                          </span>
                          {m.memberType === 'invite' && m.invitedAt && m.invitedAt !== '0' ? (
                            <span className="block text-xs text-muted-foreground">
                              {t('members.invited_at', {
                                time: formatDistanceToNow(new Date(Number(m.invitedAt) * 1000), {
                                  addSuffix: true,
                                  locale,
                                }),
                              })}
                            </span>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        {lockReason === null ? (
                          <Button
                            variant="ghost"
                            size="icon"
                            disabled={removeMember.isPending}
                            onClick={() => setRemoveTarget(m)}
                            aria-label={t('members.remove_member_aria', {
                              user: getMemberPrimaryLabel(m, t),
                            })}
                            title={t('members.remove_member_title')}
                          >
                            <Trash2 className="h-4 w-4 text-destructive" />
                          </Button>
                        ) : (
                          <span className="text-xs text-muted-foreground" title={lockReason}>
                            {t('members.no_actions')}
                          </span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : (
            <EmptyState
              icon={Users}
              title={
                query || roleFilter !== 'all' || sourceFilter !== 'all' || scopeFilter !== 'all'
                  ? t('members.empty_filtered_title')
                  : t('members.empty_title')
              }
              description={
                query || roleFilter !== 'all' || sourceFilter !== 'all' || scopeFilter !== 'all'
                  ? t('members.empty_filtered_body')
                  : t('members.empty_body')
              }
              action={
                isAdmin
                  ? { label: t('members.invite_button'), onClick: () => setInviteOpen(true) }
                  : undefined
              }
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t('members.pending_title')}</CardTitle>
            <CardDescription>
              {t('members.pending_description', {
                visible: pendingInvites.length,
                total: memberStats.pending,
              })}
            </CardDescription>
          </div>
          {memberStats.pending > 0 ? (
            <InlineBadge tone="warning">{t('members.metrics.pending')}</InlineBadge>
          ) : null}
        </CardHeader>
        <CardContent>
          {isPending ? (
            <MembersTableSkeleton loadingLabel={t('app.loading')} />
          ) : pendingInvites.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('members.col_user')}</TableHead>
                  <TableHead>{t('members.col_role')}</TableHead>
                  <TableHead>{t('members.col_source')}</TableHead>
                  <TableHead>{t('members.col_joined')}</TableHead>
                  <TableHead className="w-24 text-right">{t('members.col_actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pendingInvites.map((invite) => (
                  <TableRow key={invite.id}>
                    <TableCell className="min-w-[18rem]">
                      <div className="space-y-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{getMemberPrimaryLabel(invite, t)}</span>
                          <InlineBadge tone="warning">{t('members.status.pending')}</InlineBadge>
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {getMemberSecondaryLabel(invite, t)}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="space-y-1">
                        <RoleBadge role={invite.role as Role} />
                        <div className="text-xs text-muted-foreground">
                          {t('members.pending_role_hint')}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="space-y-1">
                        <span className="text-sm">{t(`members.source.${invite.roleSource}`)}</span>
                        <span className="block text-xs text-muted-foreground">
                          {t(`members.source_hint.${invite.roleSource}`)}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {invite.invitedAt && invite.invitedAt !== '0'
                        ? formatDistanceToNow(new Date(Number(invite.invitedAt) * 1000), {
                            addSuffix: true,
                            locale,
                          })
                        : t('common.never')}
                    </TableCell>
                    <TableCell className="text-right">
                      {isAdmin ? (
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={removeMember.isPending}
                          onClick={() => setRemoveTarget(invite)}
                        >
                          {t('members.revoke_invite')}
                        </Button>
                      ) : (
                        <span className="text-xs text-muted-foreground">
                          {t('members.no_actions')}
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState
              icon={Users}
              title={t('members.pending_empty_title')}
              description={t('members.pending_empty_body')}
            />
          )}
        </CardContent>
      </Card>

      <Dialog open={!!removeTarget} onOpenChange={(open) => !open && setRemoveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {removeTarget?.memberType === 'invite'
                ? t('members.revoke_title')
                : t('members.remove_title')}
            </DialogTitle>
            <DialogDescription>
              {removeTarget?.memberType === 'invite'
                ? t('members.revoke_confirm', {
                    user: removeTargetLabel,
                  })
                : t('members.remove_confirm', {
                    user: removeTargetLabel,
                  })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoveTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleRemove} disabled={removeMember.isPending}>
              {removeMember.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {removeTarget?.memberType === 'invite'
                ? t('members.revoke_button')
                : t('members.remove_button')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={inviteOpen} onOpenChange={setInviteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('members.invite_title')}</DialogTitle>
            <DialogDescription>{t('members.invite_description')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="invite-email">{t('members.invite_email')}</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="user@example.com"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label>{t('members.invite_role')}</Label>
              <Select value={inviteRole} onValueChange={(v) => setInviteRole(v as Role)}>
                <SelectTrigger>
                  <SelectValue>
                    <RoleBadge role={inviteRole} />
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {ROLES.map((r) => (
                    <SelectItem key={r} value={r}>
                      <RoleBadge role={r} />
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setInviteOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleInvite} disabled={inviteMember.isPending || !inviteEmail.trim()}>
              {inviteMember.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t('members.invite_submit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function buildMemberStats(members: Member[]) {
  return {
    total: members.length,
    active: members.filter((member) => member.memberType !== 'invite').length,
    pending: members.filter((member) => member.memberType === 'invite').length,
    admins: members.filter((member) => member.role === 'admin').length,
    members: members.filter((member) => member.role === 'member').length,
    viewers: members.filter((member) => member.role === 'viewer').length,
  }
}

function filterMembers(
  members: Member[],
  {
    query,
    scopeFilter,
    roleFilter,
    sourceFilter,
    t,
  }: {
    query: string
    scopeFilter: 'all' | 'active' | 'pending'
    roleFilter: 'all' | Role
    sourceFilter: 'all' | 'bootstrap' | 'idp' | 'manual'
    t: ReturnType<typeof useTranslation>['t']
  },
): Member[] {
  const normalizedQuery = query.trim().toLowerCase()

  return members.filter((member) => {
    if (scopeFilter === 'active' && member.memberType === 'invite') return false
    if (scopeFilter === 'pending' && member.memberType !== 'invite') return false
    if (roleFilter !== 'all' && member.role !== roleFilter) return false
    if (sourceFilter !== 'all' && member.roleSource !== sourceFilter) return false
    if (!normalizedQuery) return true

    const haystack = [
      getMemberPrimaryLabel(member, t),
      getMemberSecondaryLabel(member, t),
      member.role,
      member.roleSource,
      member.memberType,
      t(`members.type.${member.memberType}`),
      t(`members.source.${member.roleSource}`),
      t(`role.${member.role}`),
    ]
      .join(' ')
      .toLowerCase()

    return haystack.includes(normalizedQuery)
  })
}

function getMemberLockReason({
  member,
  activeAdminCount,
  actorRole,
  actorUserId,
  isAdmin,
  t,
}: {
  member: Member
  activeAdminCount: number
  actorRole: Role
  actorUserId: string
  isAdmin: boolean
  t: ReturnType<typeof useTranslation>['t']
}): string | null {
  if (!isAdmin) return t('members.role_locked_privilege')
  if (member.userId === actorUserId) return t('members.role_locked_self')
  const hierarchy: Record<Role, number> = { viewer: 0, member: 1, admin: 2 }
  if (hierarchy[actorRole] <= hierarchy[member.role as Role]) {
    return t('members.role_locked_privilege')
  }
  if (member.role === 'admin' && activeAdminCount <= 1) {
    return t('members.role_locked_last_admin')
  }
  return null
}

function getMemberPrimaryLabel(member: Member, t: ReturnType<typeof useTranslation>['t']): string {
  if (member.email) return member.email
  if (member.memberType === 'admin' || member.roleSource === 'bootstrap') {
    return t('members.bootstrap_identity')
  }
  if (member.userId) return t('members.fallback_identity', { shortId: abbreviateId(member.userId) })
  return t('members.unknown_identity')
}

function getMemberSecondaryLabel(
  member: Member,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  if (member.userId && member.email) return member.userId
  if (member.userId) return t('members.user_id_value', { id: member.userId })
  if (member.email) return t('members.invite_email_value', { email: member.email })
  return t('members.no_secondary_identity')
}

function abbreviateId(value: string): string {
  if (value.length <= 14) return value
  return `${value.slice(0, 8)}…${value.slice(-4)}`
}

function MembersTableSkeleton({ loadingLabel }: { loadingLabel: string }) {
  const rows = ['member-skeleton-a', 'member-skeleton-b', 'member-skeleton-c', 'member-skeleton-d']
  return (
    <div className="space-y-4" role="status" aria-live="polite">
      <div className="grid gap-3">
        {rows.map((rowKey) => (
          <div
            key={rowKey}
            className="grid gap-3 rounded-lg border border-border/60 px-4 py-3 md:grid-cols-[2.1fr_1fr_1fr_1.2fr_1.2fr_0.6fr]"
          >
            <div className="space-y-2">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-56" />
            </div>
            <div className="space-y-2">
              <Skeleton className="h-5 w-16 rounded-full" />
              <Skeleton className="h-3 w-20" />
            </div>
            <Skeleton className="h-9 w-24" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-16" />
              <Skeleton className="h-3 w-24" />
            </div>
            <div className="space-y-2">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-3 w-24" />
            </div>
            <Skeleton className="ml-auto h-9 w-9" />
          </div>
        ))}
      </div>
      <div className="flex items-center justify-center text-sm text-muted-foreground">
        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
        {loadingLabel}
      </div>
    </div>
  )
}

function InlineBadge({
  children,
  tone,
}: {
  children: string
  tone: 'neutral' | 'success' | 'warning'
}) {
  const toneClass =
    tone === 'success'
      ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400'
      : tone === 'warning'
        ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400'
        : 'border-border bg-muted/50 text-muted-foreground'

  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${toneClass}`}
    >
      {children}
    </span>
  )
}

function GovernanceCallout({
  title,
  body,
  tone,
}: {
  title: string
  body: string
  tone: 'neutral' | 'warning'
}) {
  return (
    <Card
      className={tone === 'warning' ? 'border-amber-500/40 bg-amber-500/5' : 'border-border/70'}
    >
      <CardHeader className="space-y-1 pb-3">
        <CardTitle className="text-base">{title}</CardTitle>
        <CardDescription>{body}</CardDescription>
      </CardHeader>
    </Card>
  )
}

function MetricCard({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <Card role="group" aria-label={label} className="border-border/70 bg-muted/15 shadow-none">
      <CardHeader className="space-y-1 pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-2xl">{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  )
}
