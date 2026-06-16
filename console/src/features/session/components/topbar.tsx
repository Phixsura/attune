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
      <div className="mx-auto flex h-14 w-full max-w-6xl items-center gap-6 px-6">
        <Link to="/" className="hover:opacity-80">
          <Logo />
        </Link>
        <span className="text-sm text-muted-foreground">{me.tenant?.name}</span>
        <nav className="ml-6 flex items-center gap-4 text-sm">
          <NavLink to="/feedback">{t('nav.feedback')}</NavLink>
          <NavLink to="/usage">{t('nav.usage')}</NavLink>
          <NavLink to="/llm-usage">{t('nav.llm_usage')}</NavLink>
          {can('nav:llm_config') && <NavLink to="/llm-config">{t('nav.llm_config')}</NavLink>}
          {can('nav:settings') && <NavLink to="/settings">{t('nav.settings')}</NavLink>}
        </nav>
        <div className="ml-auto flex items-center gap-3">
          <RoleBadge role={role} />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="gap-2">
                <User className="size-4" />
                {me.user?.name}
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
