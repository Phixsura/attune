import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertTriangle,
  Check,
  Clock,
  Copy,
  Key,
  Loader2,
  Lock,
  Plus,
  Shield,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  Unlock,
  X,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { cutoverToSSO, fallbackToHybrid, getAuthMode } from '../api/auth-mode'
import {
  type BreakGlassToken,
  issueBreakGlassToken,
  listBreakGlassTokens,
  revokeBreakGlassToken,
} from '../api/breakglass'

type TokenStatus = 'active' | 'used' | 'revoked' | 'expiring' | 'expired'

function getTokenStatus(token: BreakGlassToken): TokenStatus {
  if (token.revoked_at) return 'revoked'
  if (token.used_at) return 'used'
  const now = new Date()
  const expires = new Date(token.expires_at)
  if (expires < now) return 'expired'
  const hoursUntilExpiry = (expires.getTime() - now.getTime()) / (1000 * 60 * 60)
  if (hoursUntilExpiry <= 1) return 'expiring'
  return 'active'
}

function SecurityStat({
  label,
  value,
  tone = 'default',
  hint,
}: {
  label: string
  value: string
  tone?: 'default' | 'urgent' | 'active'
  hint?: string
}) {
  const toneClass =
    tone === 'urgent'
      ? 'border-amber-300/55 bg-amber-50/85 text-amber-900'
      : tone === 'active'
        ? 'border-emerald-300/55 bg-emerald-50/85 text-emerald-900'
        : 'border-border/60 bg-background/88 text-foreground'
  return (
    <div
      className={`rounded-[1.15rem] border px-4 py-3.5 shadow-[0_18px_38px_-34px_rgba(15,23,42,0.24)] ${toneClass}`}
    >
      <div className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1 text-[1.55rem] font-semibold tracking-tight [font-variant-numeric:tabular-nums]">
        {value}
      </div>
      {hint ? <div className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</div> : null}
    </div>
  )
}

function StatusDot({ status }: { status: TokenStatus }) {
  const color = {
    active: 'bg-emerald-500',
    expiring: 'bg-amber-500',
    used: 'bg-slate-400',
    revoked: 'bg-slate-400',
    expired: 'bg-red-500',
  }[status]
  return <span className={cn('h-2 w-2 rounded-full', color)} />
}

function formatTimeRemaining(
  expiresAt: string,
  t: (key: string, fallback: string, options?: Record<string, unknown>) => string,
): string {
  const remaining = new Date(expiresAt).getTime() - Date.now()
  if (remaining <= 0) return t('security.time.expired', 'Expired')
  const minutes = Math.floor(remaining / 60000)
  if (minutes < 60) return t('security.time.minutes', '{{count}}m left', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('security.time.hours', '{{count}}h left', { count: hours })
  const days = Math.floor(hours / 24)
  return t('security.time.days', '{{count}}d left', { count: days })
}

export function SecurityPage() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const locale = i18n.language === 'zh' ? zhCN : undefined

  const [showIssueDialog, setShowIssueDialog] = useState(false)
  const [showTokenDialog, setShowTokenDialog] = useState(false)
  const [showCutoverDialog, setShowCutoverDialog] = useState(false)
  const [showFallbackDialog, setShowFallbackDialog] = useState(false)
  const [issuedToken, setIssuedToken] = useState<{ token: string; expiresAt: string } | null>(null)
  const [skipBreakglass, setSkipBreakglass] = useState(false)
  const [issueEmail, setIssueEmail] = useState('')
  const [issueTTL, setIssueTTL] = useState('60')
  const [issueIPs, setIssueIPs] = useState('')
  const [cutoverResult, setCutoverResult] = useState<{
    success: boolean
    message: string
    preflight?: {
      status: string
      checks: Array<{ name: string; status: string; message: string; remediation?: string }>
    }
  } | null>(null)

  const { data: authMode } = useQuery({
    queryKey: ['auth-mode'],
    queryFn: getAuthMode,
  })

  const { data: tokens, isLoading: tokensLoading } = useQuery({
    queryKey: ['breakglass-tokens'],
    queryFn: listBreakGlassTokens,
  })

  const cutoverMutation = useMutation({
    mutationFn: () => cutoverToSSO(skipBreakglass),
    onSuccess: (result) => {
      setCutoverResult(result)
      if (result.success) {
        toast.success(t('security.cutover.success', 'Switched to SSO-only mode'))
        setShowCutoverDialog(false)
        queryClient.invalidateQueries({ queryKey: ['auth-mode'] })
      }
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error', 'Error')),
  })

  const fallbackMutation = useMutation({
    mutationFn: fallbackToHybrid,
    onSuccess: () => {
      toast.success(t('security.fallback.success', 'Switched to hybrid mode'))
      setShowFallbackDialog(false)
      queryClient.invalidateQueries({ queryKey: ['auth-mode'] })
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error', 'Error')),
  })

  const issueMutation = useMutation({
    mutationFn: () =>
      issueBreakGlassToken({
        admin_email: issueEmail,
        ttl_minutes: Number.parseInt(issueTTL, 10),
        allowed_ips: issueIPs
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean),
      }),
    onSuccess: (result) => {
      setIssuedToken({ token: result.raw_token, expiresAt: result.expires_at })
      setShowIssueDialog(false)
      setShowTokenDialog(true)
      setIssueEmail('')
      setIssueTTL('60')
      setIssueIPs('')
      queryClient.invalidateQueries({ queryKey: ['breakglass-tokens'] })
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error', 'Error')),
  })

  const revokeMutation = useMutation({
    mutationFn: revokeBreakGlassToken,
    onSuccess: () => {
      toast.success(t('security.token.revoked', 'Token revoked'))
      queryClient.invalidateQueries({ queryKey: ['breakglass-tokens'] })
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error', 'Error')),
  })

  const [page, setPage] = useState(1)
  const pageSize = 10

  const isHybrid = authMode?.mode === 'hybrid'
  const allTokens = tokens?.tokens ?? []
  const activeCount = allTokens.filter((tk) => getTokenStatus(tk) === 'active').length
  const expiringCount = allTokens.filter((tk) => getTokenStatus(tk) === 'expiring').length
  const usedCount = allTokens.filter((tk) => getTokenStatus(tk) === 'used').length
  const revokedCount = allTokens.filter((tk) => getTokenStatus(tk) === 'revoked').length

  const totalPages = Math.ceil(allTokens.length / pageSize)
  const paginatedTokens = allTokens.slice((page - 1) * pageSize, page * pageSize)

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
    toast.success(t('common.copied'))
  }

  return (
    <div className="space-y-5">
      {/* Hero Section */}
      <section className="rounded-[1.45rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,252,248,0.9),rgba(255,255,255,0.995)_24%,rgba(249,250,251,0.99))] p-2 shadow-[0_24px_70px_-58px_rgba(15,23,42,0.2)]">
        <div className="relative overflow-hidden rounded-[1.28rem] border border-border/55 bg-[radial-gradient(circle_at_top_left,rgba(255,247,237,0.96),rgba(255,255,255,0.985)_38%),linear-gradient(135deg,rgba(255,255,255,0.985),rgba(248,250,252,0.98))] px-5 py-4.5 sm:px-6 sm:py-5">
          <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />
          <div className="pointer-events-none absolute top-0 right-0 h-36 w-36 rounded-full bg-primary/[0.06] blur-3xl" />
          <div className="min-w-0">
            <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
              {t('shell.groups.administration')}
            </div>
            <div className="mt-2 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <div className="min-w-0">
                <h1 className="text-[2rem] font-semibold tracking-tight text-foreground text-balance sm:text-[2.25rem]">
                  {t('security.title', 'Security')}
                </h1>
                <p className="mt-2.5 max-w-2xl text-[13.5px] leading-[1.6rem] text-muted-foreground text-pretty">
                  {t(
                    'security.subtitle',
                    'Manage authentication mode and emergency access tokens.',
                  )}
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-2 lg:justify-end">
                <div className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/78 px-3 py-1.5 text-xs">
                  {isHybrid ? (
                    <ShieldAlert className="h-3.5 w-3.5 text-amber-500" />
                  ) : (
                    <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
                  )}
                  <span className="font-medium text-foreground">
                    {isHybrid
                      ? t('security.mode.hybrid', 'Hybrid')
                      : t('security.mode.sso_only', 'SSO Only')}
                  </span>
                </div>
              </div>
            </div>
            <div className="mt-4 grid gap-2 rounded-[1.15rem] border border-border/60 bg-background/80 p-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] sm:grid-cols-2 xl:grid-cols-4">
              <SecurityStat
                label={t('security.stats.active', 'Active')}
                value={String(activeCount)}
                tone={activeCount > 0 ? 'active' : 'default'}
                hint={t('security.stats.active_hint', 'Tokens ready for use')}
              />
              <SecurityStat
                label={t('security.stats.expiring', 'Expiring')}
                value={String(expiringCount)}
                tone={expiringCount > 0 ? 'urgent' : 'default'}
                hint={t('security.stats.expiring_hint', 'Expiring within 1 hour')}
              />
              <SecurityStat
                label={t('security.stats.used', 'Used')}
                value={String(usedCount)}
                hint={t('security.stats.used_hint', 'Tokens already consumed')}
              />
              <SecurityStat
                label={t('security.stats.revoked', 'Revoked')}
                value={String(revokedCount)}
                hint={t('security.stats.revoked_hint', 'Manually revoked')}
              />
            </div>
          </div>
        </div>

        {/* Actions Bar */}
        <Card className="mt-2 overflow-hidden rounded-[1.2rem] border-border/65 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-[0_16px_36px_-34px_rgba(15,23,42,0.14)]">
          <CardContent className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 sm:px-6">
            <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
              {t('security.actions', 'Actions')}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button size="sm" onClick={() => setShowIssueDialog(true)}>
                <Plus className="mr-1.5 h-3.5 w-3.5" />
                {t('security.action.issue', 'Issue Token')}
              </Button>
              {isHybrid ? (
                <Button size="sm" variant="outline" onClick={() => setShowCutoverDialog(true)}>
                  <Lock className="mr-1.5 h-3.5 w-3.5" />
                  {t('security.action.enforce_sso', 'Enforce SSO')}
                </Button>
              ) : (
                <Button size="sm" variant="outline" onClick={() => setShowFallbackDialog(true)}>
                  <Unlock className="mr-1.5 h-3.5 w-3.5" />
                  {t('security.action.enable_password', 'Enable Password')}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      </section>

      {/* Tokens Table */}
      <Card className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(249,250,251,0.985))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.22)]">
        <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.82),rgba(255,255,255,0.92))] px-5 py-4 sm:px-6">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <CardTitle className="text-[1.18rem] tracking-tight">
                {t('security.tokens.title', 'Break-Glass Tokens')}
              </CardTitle>
              <CardDescription className="mt-1 text-sm leading-[1.35rem]">
                {t('security.tokens.description', 'Emergency access when SSO is unavailable')}
              </CardDescription>
            </div>
            <div className="text-sm text-muted-foreground">
              {t('security.tokens.count', '{{count}} tokens', { count: allTokens.length })}
            </div>
          </div>
        </CardHeader>
        <CardContent className="p-0">
          {tokensLoading ? (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('common.loading', 'Loading...')}
            </div>
          ) : allTokens.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="pl-5 sm:pl-6">{t('security.col.admin')}</TableHead>
                    <TableHead>{t('security.col.status')}</TableHead>
                    <TableHead>{t('security.col.validity')}</TableHead>
                    <TableHead>{t('security.col.issued')}</TableHead>
                    <TableHead className="w-[60px]" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {paginatedTokens.map((token) => {
                    const status = getTokenStatus(token)
                    const isActionable = status === 'active' || status === 'expiring'
                    return (
                      <TableRow key={token.id}>
                        <TableCell className="pl-5 font-medium sm:pl-6">
                          {token.admin_email}
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-2">
                            <StatusDot status={status} />
                            <span className="text-sm text-muted-foreground">
                              {t(`security.status.${status}`, status)}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell>
                          {isActionable ? (
                            <span className="flex items-center gap-1.5 text-sm">
                              <Clock className="h-3.5 w-3.5 text-muted-foreground" />
                              {formatTimeRemaining(token.expires_at, t)}
                            </span>
                          ) : (
                            <span className="text-sm text-muted-foreground">
                              {formatDistanceToNow(new Date(token.expires_at), {
                                addSuffix: true,
                                locale,
                              })}
                            </span>
                          )}
                        </TableCell>
                        <TableCell className="text-sm text-muted-foreground">
                          {formatDistanceToNow(new Date(token.issued_at), {
                            addSuffix: true,
                            locale,
                          })}
                        </TableCell>
                        <TableCell>
                          {isActionable && (
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-muted-foreground hover:text-destructive"
                              onClick={() => revokeMutation.mutate(token.id)}
                              disabled={revokeMutation.isPending}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
              {totalPages > 1 && (
                <div className="flex items-center justify-between border-t border-border/55 px-5 py-3 sm:px-6">
                  <div className="text-sm text-muted-foreground">
                    {t('security.tokens.page_info', { current: page, total: totalPages })}
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPage((p) => Math.max(1, p - 1))}
                      disabled={page === 1}
                    >
                      {t('security.pagination.prev')}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                      disabled={page === totalPages}
                    >
                      {t('security.pagination.next')}
                    </Button>
                  </div>
                </div>
              )}
            </>
          ) : (
            <div className="py-12">
              <EmptyState
                icon={Key}
                title={t('security.tokens.empty_title')}
                description={t('security.tokens.empty_desc')}
                action={{
                  label: t('security.action.issue'),
                  onClick: () => setShowIssueDialog(true),
                }}
              />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Issue Dialog */}
      <Dialog open={showIssueDialog} onOpenChange={setShowIssueDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('security.issue.title', 'Issue Break-Glass Token')}</DialogTitle>
            <DialogDescription>
              {t('security.issue.description', 'Create a one-time emergency access token.')}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="admin-email">{t('security.issue.email', 'Admin Email')}</Label>
              <Input
                id="admin-email"
                type="email"
                placeholder="admin@example.com"
                value={issueEmail}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setIssueEmail(e.target.value)}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ttl">{t('security.issue.validity', 'Validity')}</Label>
              <Select value={issueTTL} onValueChange={setIssueTTL}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="15">{t('security.ttl.15m', '15 minutes')}</SelectItem>
                  <SelectItem value="30">{t('security.ttl.30m', '30 minutes')}</SelectItem>
                  <SelectItem value="60">{t('security.ttl.1h', '1 hour')}</SelectItem>
                  <SelectItem value="120">{t('security.ttl.2h', '2 hours')}</SelectItem>
                  <SelectItem value="240">{t('security.ttl.4h', '4 hours')}</SelectItem>
                  <SelectItem value="480">{t('security.ttl.8h', '8 hours')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="allowed-ips">
                {t('security.issue.ip_allowlist', 'IP Allowlist')}
                <span className="ml-1 font-normal text-muted-foreground">
                  ({t('common.optional', 'optional')})
                </span>
              </Label>
              <Input
                id="allowed-ips"
                placeholder="192.168.1.0/24, 10.0.0.1"
                value={issueIPs}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) => setIssueIPs(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                {t('security.issue.ip_hint', 'Comma-separated CIDRs. Empty = any IP.')}
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowIssueDialog(false)}>
              {t('common.cancel', 'Cancel')}
            </Button>
            <Button
              onClick={() => issueMutation.mutate()}
              disabled={!issueEmail || issueMutation.isPending}
            >
              {issueMutation.isPending
                ? t('common.processing', 'Processing...')
                : t('security.action.issue', 'Issue Token')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Token Display Dialog */}
      <Dialog open={showTokenDialog} onOpenChange={setShowTokenDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Check className="h-5 w-5 text-emerald-500" />
              {t('security.issued.title', 'Token Issued')}
            </DialogTitle>
            <DialogDescription>
              {t('security.issued.description', 'Copy now. This will not be shown again.')}
            </DialogDescription>
          </DialogHeader>
          <Alert variant="destructive">
            <AlertTriangle className="h-4 w-4" />
            <AlertTitle>{t('security.issued.warning_title', 'Security Warning')}</AlertTitle>
            <AlertDescription>
              {t('security.issued.warning_desc', 'This grants full admin access. Send securely.')}
            </AlertDescription>
          </Alert>
          <div className="space-y-2">
            <Label>{t('security.issued.url', 'Login URL')}</Label>
            <div className="flex gap-2">
              <Input
                readOnly
                value={`${window.location.origin}/console/breakglass?token=${issuedToken?.token ?? ''}`}
                className="font-mono text-xs"
              />
              <Button
                variant="outline"
                size="icon"
                onClick={() =>
                  copyToClipboard(
                    `${window.location.origin}/console/breakglass?token=${issuedToken?.token ?? ''}`,
                  )
                }
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
            {issuedToken?.expiresAt && (
              <p className="text-xs text-muted-foreground">
                {t('security.issued.expires', 'Expires')}:{' '}
                {formatTimeRemaining(issuedToken.expiresAt, t)}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button onClick={() => setShowTokenDialog(false)}>{t('common.done', 'Done')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* SSO Cutover Dialog */}
      <Dialog
        open={showCutoverDialog}
        onOpenChange={(open) => {
          setShowCutoverDialog(open)
          if (!open) {
            setCutoverResult(null)
            setSkipBreakglass(false)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('security.cutover.title', 'Switch to SSO-Only')}</DialogTitle>
            <DialogDescription>
              {t(
                'security.cutover.description',
                'Disables password login. Ensure SSO is configured.',
              )}
            </DialogDescription>
          </DialogHeader>

          {activeCount === 0 && expiringCount === 0 && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{t('security.cutover.no_tokens_title', 'No active tokens')}</AlertTitle>
              <AlertDescription>
                {t('security.cutover.no_tokens_desc', 'Issue a break-glass token first.')}
              </AlertDescription>
            </Alert>
          )}

          {cutoverResult && !cutoverResult.success && cutoverResult.preflight && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{t('security.cutover.preflight_failed', 'Preflight failed')}</AlertTitle>
              <AlertDescription>
                <ul className="mt-2 space-y-1">
                  {cutoverResult.preflight.checks.map((check) => (
                    <li key={check.name} className="flex items-start gap-2 text-sm">
                      {check.status === 'pass' ? (
                        <Check className="h-4 w-4 text-emerald-500 mt-0.5 shrink-0" />
                      ) : (
                        <X className="h-4 w-4 text-red-500 mt-0.5 shrink-0" />
                      )}
                      <span>{check.message}</span>
                    </li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}

          <div className="flex items-center gap-2">
            <Checkbox
              id="skip-breakglass"
              checked={skipBreakglass}
              onCheckedChange={(checked) => setSkipBreakglass(checked === true)}
            />
            <Label htmlFor="skip-breakglass" className="text-sm">
              {t('security.cutover.skip_check', 'Skip break-glass check')}
            </Label>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCutoverDialog(false)}>
              {t('common.cancel', 'Cancel')}
            </Button>
            <Button onClick={() => cutoverMutation.mutate()} disabled={cutoverMutation.isPending}>
              {cutoverMutation.isPending
                ? t('common.processing', 'Processing...')
                : t('security.cutover.confirm', 'Switch to SSO')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Fallback Dialog */}
      <Dialog open={showFallbackDialog} onOpenChange={setShowFallbackDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('security.fallback.title', 'Enable Password Login')}</DialogTitle>
            <DialogDescription>
              {t('security.fallback.description', 'Switch back to hybrid authentication.')}
            </DialogDescription>
          </DialogHeader>
          <Alert>
            <Shield className="h-4 w-4" />
            <AlertTitle>{t('security.fallback.warning_title', 'Security Downgrade')}</AlertTitle>
            <AlertDescription>
              {t('security.fallback.warning_desc', 'Only do this if SSO is unavailable.')}
            </AlertDescription>
          </Alert>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowFallbackDialog(false)}>
              {t('common.cancel', 'Cancel')}
            </Button>
            <Button
              variant="destructive"
              onClick={() => fallbackMutation.mutate()}
              disabled={fallbackMutation.isPending}
            >
              {fallbackMutation.isPending
                ? t('common.processing', 'Processing...')
                : t('security.fallback.confirm', 'Enable Password')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
