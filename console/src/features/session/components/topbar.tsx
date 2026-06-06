import { Link } from '@tanstack/react-router'
import { LogOut, User } from 'lucide-react'
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

interface TopBarProps {
  me: SessionMe
}

// TopBar is the shell every authed page sits under. Logo, tenant name,
// nav links, then a user menu on the right. Sticky so it stays put on
// long feedback lists.
export function TopBar({ me }: TopBarProps) {
  const { t } = useTranslation()
  const logout = useLogout()

  return (
    <header className="sticky top-0 z-10 border-b border-border bg-background/80 backdrop-blur">
      <div className="mx-auto flex h-14 w-full max-w-6xl items-center gap-6 px-6">
        <Link to="/" className="hover:opacity-80">
          <Logo />
        </Link>
        <span className="text-sm text-muted-foreground">{me.tenant?.name}</span>
        <nav className="ml-6 flex items-center gap-4 text-sm">
          <NavLink to="/feedback">{t('nav.feedback')}</NavLink>
          <NavLink to="/notify-targets">{t('nav.notify_targets')}</NavLink>
          <NavLink to="/api-keys">{t('nav.api_keys')}</NavLink>
          <NavLink to="/usage">{t('nav.usage')}</NavLink>
        </nav>
        <div className="ml-auto">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="gap-2">
                <User className="size-4" />
                {me.user?.name}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                {me.user?.role === 'admin' ? '管理员' : '成员'}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onSelect={() => {
                  logout.mutate(undefined, {
                    onSuccess: () => {
                      window.location.href = '/console/login'
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

interface NavLinkProps {
  to: string
  children: React.ReactNode
}

function NavLink({ to, children }: NavLinkProps) {
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
