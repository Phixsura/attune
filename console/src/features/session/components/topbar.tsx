import { Link, useNavigate } from '@tanstack/react-router'
import { KeyRound, LogOut, User } from 'lucide-react'
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
import type { SessionMe } from '@/features/session/api/get-me'
import { useLogout } from '@/features/session/api/logout'
import { RoleBadge } from '@/features/session/components/auth/role-badge'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { consolePath } from '@/lib/console-path'

function NavLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className="text-muted-foreground transition-colors hover:text-foreground"
      activeProps={{ className: 'text-foreground' }}
    >
      {children}
    </Link>
  )
}

interface TopBarProps {
  me: SessionMe
}

// TopBar is the shell every authed page sits under. Logo, tenant name,
// nav links, then a user menu on the right. Sticky so it stays put on
// long feedback lists.
export function TopBar({ me }: TopBarProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const logout = useLogout()
  const { can, isAdmin, role } = usePermissions()

  return (
    <header className="sticky top-0 z-10 border-b border-border bg-background/80 backdrop-blur">
      <div className="mx-auto flex h-14 w-full max-w-6xl min-w-0 items-center gap-3 px-4 sm:gap-6 sm:px-6">
        <Link to="/" className="shrink-0 hover:opacity-80">
          <Logo />
        </Link>
        <span className="hidden text-sm text-muted-foreground md:inline">{me.tenant?.name}</span>
        <nav className="flex min-w-0 flex-1 items-center gap-3 overflow-x-auto text-sm sm:gap-4">
          <NavLink to="/feedback">{t('nav.feedback')}</NavLink>
          <NavLink to="/usage">{t('nav.usage')}</NavLink>
          <NavLink to="/llm-usage">{t('nav.llm_usage')}</NavLink>
          {can('nav:llm_config') && <NavLink to="/llm-config">{t('nav.llm_config')}</NavLink>}
          {can('nav:settings') && <NavLink to="/settings">{t('nav.settings')}</NavLink>}
          {isAdmin && <NavLink to="/outbox-dead">{t('nav.outbox_dead')}</NavLink>}
        </nav>
        <div className="flex shrink-0 items-center gap-1 sm:gap-3">
          <div className="hidden sm:block">
            <RoleBadge role={role} />
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="gap-2 px-2 sm:px-3">
                <User className="size-4" />
                <span className="hidden max-w-32 truncate sm:inline">{me.user?.name}</span>
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
              <DropdownMenuItem
                onSelect={() => {
                  logout.mutate(undefined, {
                    onSuccess: () => {
                      window.location.href = consolePath('/login')
                    },
                  })
                }}
                disabled={logout.isPending}
              >
                <LogOut className="size-4" />
                {t('auth.logout')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </header>
  )
}
