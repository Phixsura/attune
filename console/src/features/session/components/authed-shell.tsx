import { Link, useNavigate, useRouterState } from '@tanstack/react-router'
import type { LucideIcon } from 'lucide-react'
import { KeyRound, LogOut, Menu, User } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Logo } from '@/components/brand/logo'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import type { SessionMe } from '@/features/session/api/get-me'
import { useLogout } from '@/features/session/api/logout'
import { RoleBadge } from '@/features/session/components/auth/role-badge'
import { ThemeToggle } from '@/features/session/components/theme-toggle'
import { type Permission, usePermissions } from '@/features/session/hooks/use-permissions'
import { consoleNavGroupLabelKeys, consoleNavGroupOrder, consoleNavItems } from '@/lib/console-ia'
import { consolePath } from '@/lib/console-path'

type NavItem = {
  icon: LucideIcon
  label: string
  path: string
  exact?: boolean
  permission?: Permission
  requireAdmin?: boolean
}

type NavGroup = {
  key: string
  items: NavItem[]
}

interface AuthedShellProps {
  me: SessionMe
  children: React.ReactNode
}

export function AuthedShell({ me, children }: AuthedShellProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const logout = useLogout()
  const { can, isAdmin, role } = usePermissions()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const [mobileOpen, setMobileOpen] = useState(false)

  const groups = useMemo<NavGroup[]>(
    () =>
      consoleNavGroupOrder.map((group) => ({
        key: t(consoleNavGroupLabelKeys[group]),
        items: consoleNavItems
          .filter((item) => item.group === group)
          .map((item) => ({
            icon: item.icon,
            label: t(item.labelKey),
            path: item.path,
            exact: item.exact,
            permission: item.permission,
            requireAdmin: item.adminOnly,
          })),
      })),
    [t],
  )

  const visibleGroups = groups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => {
        if (item.requireAdmin) return isAdmin
        if (!item.permission) return true
        return can(item.permission)
      }),
    }))
    .filter((group) => group.items.length > 0)

  const handleLogout = () => {
    logout.mutate(undefined, {
      onSuccess: () => {
        window.location.href = consolePath('/login')
      },
    })
  }

  const focusMainContent = () => {
    const mainContent = document.getElementById('main-content')
    if (!mainContent) return
    if (window.location.hash !== '#main-content') {
      window.history.pushState(
        null,
        '',
        `${window.location.pathname}${window.location.search}#main-content`,
      )
    }
    mainContent.focus({ preventScroll: true })
    mainContent.scrollIntoView?.({ block: 'start' })
  }

  const handleSkipToContent = (event: React.MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    focusMainContent()
  }

  const handleSkipToContentKeyDown = (event: React.KeyboardEvent<HTMLAnchorElement>) => {
    if (event.key !== 'Enter') return
    event.preventDefault()
    focusMainContent()
  }

  const sidebar = (
    <nav aria-label={t('shell.primary_nav')} className="space-y-5">
      {visibleGroups.map((group) => (
        <div key={group.key} className="space-y-1.5">
          <div className="px-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
            {group.key}
          </div>
          <div className="space-y-1">
            {group.items.map((item) => (
              <Link
                key={item.path}
                to={item.path}
                activeOptions={{ exact: item.exact ?? false }}
                activeProps={{
                  className:
                    'bg-primary/10 text-primary shadow-[inset_0_0_0_1px_rgba(234,88,12,0.14)]',
                }}
                inactiveProps={{
                  className: 'text-muted-foreground hover:bg-muted/70 hover:text-foreground',
                }}
                onClick={() => setMobileOpen(false)}
                className="flex items-center gap-3 rounded-xl px-2.5 py-2 text-sm transition-colors"
              >
                <item.icon className="size-4 shrink-0" />
                <span className="truncate">{item.label}</span>
              </Link>
            ))}
          </div>
        </div>
      ))}
    </nav>
  )

  const activeGroup = visibleGroups.find((group) =>
    group.items.some((item) => pathname === item.path || pathname.startsWith(`${item.path}/`)),
  )

  return (
    <div className="min-h-screen bg-[linear-gradient(180deg,rgba(255,255,255,0.96),rgba(255,255,255,0.9)),radial-gradient(circle_at_top_left,rgba(251,191,36,0.08),transparent_22%),var(--background)]">
      {/* biome-ignore lint/a11y/useValidAnchor: Skip links are intra-page anchors; the handler preserves focus across SPA routing. */}
      <a
        href="#main-content"
        onClick={handleSkipToContent}
        onKeyDown={handleSkipToContentKeyDown}
        className="sr-only fixed top-3 left-3 z-50 rounded-md bg-background px-3 py-2 text-sm font-medium text-foreground shadow-md focus:not-sr-only focus:outline-none focus:ring-2 focus:ring-ring"
      >
        {t('shell.skip_to_content')}
      </a>
      <header className="sticky top-0 z-30 border-b border-border/60 bg-background/92 backdrop-blur">
        <div className="mx-auto flex h-16 w-full max-w-[1600px] items-center gap-3 px-4 sm:px-6">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="lg:hidden"
                aria-label={t('shell.open_nav')}
              >
                <Menu className="size-4" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="flex h-full w-[18rem] flex-col p-0">
              <SheetHeader className="border-b border-border/70 px-5 py-4 text-left">
                <SheetTitle>{me.tenant?.name}</SheetTitle>
                <SheetDescription className="sr-only">
                  {t('shell.mobile_nav_description')}
                </SheetDescription>
              </SheetHeader>
              <div
                data-testid="shell-mobile-nav-scroll"
                className="min-h-0 flex-1 overflow-y-auto px-4 py-5"
              >
                {sidebar}
              </div>
            </SheetContent>
          </Sheet>

          <Link to="/" className="shrink-0 hover:opacity-80">
            <Logo />
          </Link>

          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-medium">{me.tenant?.name}</div>
            <div className="truncate text-xs text-muted-foreground">
              {activeGroup?.key ?? t('shell.groups.feedback')}
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-1 sm:gap-2">
            <ThemeToggle label={t('shell.toggle_theme')} />
            <RoleBadge role={role} className="hidden sm:inline-flex" />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2 px-2 sm:px-3">
                  <User className="size-4" />
                  <span className="sr-only sm:not-sr-only sm:inline-block sm:max-w-40 sm:truncate">
                    {me.user?.name}
                  </span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                {isAdmin && (
                  <>
                    <DropdownMenuItem
                      onSelect={() => {
                        void navigate({ to: '/change-password' })
                      }}
                    >
                      <KeyRound className="size-4" />
                      {t('auth.change_password.menu')}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                  </>
                )}
                <DropdownMenuItem onSelect={handleLogout} disabled={logout.isPending}>
                  <LogOut className="size-4" />
                  {t('auth.logout')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-[1640px] gap-4 px-4 py-6 sm:px-6">
        <aside className="hidden w-52 shrink-0 self-start lg:block">
          <div
            data-testid="shell-sidebar-scroll"
            className="sticky top-24 max-h-[calc(100dvh-7rem)] overflow-y-auto overscroll-contain pr-2"
          >
            <div className="mb-3 border-b border-border/70 pb-3">
              <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
                Attune Console
              </div>
              <p className="mt-1.5 text-xs leading-5 text-muted-foreground">
                {t('shell.console_overview')}
              </p>
            </div>
            {sidebar}
          </div>
        </aside>

        <main id="main-content" tabIndex={-1} className="min-w-0 flex-1 bg-background pb-10">
          <div className="mx-auto max-w-[1440px]">{children}</div>
        </main>
      </div>
    </div>
  )
}
