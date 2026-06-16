import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, Plus, Trash2, Users } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { RoleBadge } from '@/components/auth/role-badge'
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
import { type Role, usePermissions } from '@/lib/hooks/use-permissions'
import type { Member } from '@/proto/attune/v1/member'

const ROLES: Role[] = ['admin', 'member', 'viewer']

export function MembersPage() {
  const { t, i18n } = useTranslation()
  const { data: members, isPending } = useMembers()
  const updateRole = useUpdateMemberRole()
  const removeMember = useRemoveMember()
  const inviteMember = useInviteMember()
  const { userId, role, isAdmin } = usePermissions()

  const canManageMember = (targetRole: string) => {
    if (!isAdmin) return false
    const hierarchy: Record<string, number> = { viewer: 0, member: 1, admin: 2 }
    return hierarchy[role] > hierarchy[targetRole]
  }

  const [removeTarget, setRemoveTarget] = useState<Member | null>(null)
  const [inviteOpen, setInviteOpen] = useState(false)
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState<Role>('member')

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
    if (!inviteEmail.trim()) return
    inviteMember.mutate(
      { email: inviteEmail.trim(), role: inviteRole },
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
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t('members.title')}</CardTitle>
            <CardDescription>{t('members.count', { count: members?.length ?? 0 })}</CardDescription>
          </div>
          {isAdmin && (
            <Button onClick={() => setInviteOpen(true)}>
              <Plus className="mr-2 h-4 w-4" />
              {t('members.invite_button')}
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {isPending ? (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : members && members.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('members.col_user')}</TableHead>
                  <TableHead>{t('members.col_type')}</TableHead>
                  <TableHead>{t('members.col_role')}</TableHead>
                  <TableHead>{t('members.col_source')}</TableHead>
                  <TableHead>{t('members.col_joined')}</TableHead>
                  <TableHead className="w-16" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.map((m) => (
                  <TableRow key={m.id}>
                    <TableCell className="font-mono text-sm">
                      {m.email || m.userId}
                      {m.userId === userId && (
                        <span className="ml-2 text-xs text-muted-foreground">
                          ({t('members.you')})
                        </span>
                      )}
                      {m.memberType === 'invite' && (
                        <span className="ml-2 text-xs text-muted-foreground">
                          ({t('members.pending')})
                        </span>
                      )}
                    </TableCell>
                    <TableCell>
                      <span className="rounded bg-muted px-1.5 py-0.5 text-xs">{m.memberType}</span>
                    </TableCell>
                    <TableCell>
                      <Select
                        value={m.role}
                        onValueChange={(v) => handleRoleChange(m, v)}
                        disabled={
                          updateRole.isPending || m.userId === userId || !canManageMember(m.role)
                        }
                      >
                        <SelectTrigger className="w-28">
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
                    </TableCell>
                    <TableCell>
                      <span className="text-xs text-muted-foreground">{m.roleSource}</span>
                    </TableCell>
                    <TableCell>
                      <span className="text-sm text-muted-foreground">
                        {m.acceptedAt && m.acceptedAt !== '0'
                          ? formatDistanceToNow(new Date(Number(m.acceptedAt) * 1000), {
                              addSuffix: true,
                              locale,
                            })
                          : t('members.pending')}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={
                          removeMember.isPending || m.userId === userId || !canManageMember(m.role)
                        }
                        onClick={() => setRemoveTarget(m)}
                      >
                        <Trash2 className="h-4 w-4 text-destructive" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState
              icon={Users}
              title={t('members.empty_title')}
              description={t('members.empty_body')}
            />
          )}
        </CardContent>
      </Card>

      <Dialog open={!!removeTarget} onOpenChange={(open) => !open && setRemoveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('members.remove_title')}</DialogTitle>
            <DialogDescription>
              {t('members.remove_confirm', { user: removeTarget?.userId || removeTarget?.email })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRemoveTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleRemove} disabled={removeMember.isPending}>
              {removeMember.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t('members.remove_button')}
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
