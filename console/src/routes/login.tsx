import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

// Login is intentionally the simplest possible flow: one button that
// hands off to /fb/v1/console/install/start (server 302s to Lark).
//
// The post-login redirect_uri is the current path so a user landing
// on `/console/api-keys` while logged out comes back to the same
// place after auth.

export const Route = createFileRoute('/login')({
  component: LoginPage,
  validateSearch: (search): { redirect?: string } => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
})

function LoginPage() {
  const { t } = useTranslation()
  const { redirect } = Route.useSearch()

  const startURL = `/fb/v1/console/install/start${
    redirect ? `?redirect_uri=${encodeURIComponent(redirect)}` : ''
  }`

  return (
    <main className="grid min-h-screen place-items-center bg-muted/30">
      <div className="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm">
        <h1 className="text-xl font-semibold tracking-tight">{t('app.title')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {t('auth.login_hint_tenant', { action: t('auth.login_with_lark') })}
        </p>
        <Button asChild className="mt-6 w-full" size="lg">
          <a href={startURL}>{t('auth.login_with_lark')}</a>
        </Button>
        <p className="mt-6 text-center text-xs text-muted-foreground">
          {t('auth.login_hint_first_user')}
        </p>
      </div>
    </main>
  )
}
