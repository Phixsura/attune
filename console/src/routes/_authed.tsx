import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { meQuery } from '@/features/session/api/get-me'
import { AuthedShell } from '@/features/session/components/authed-shell'

// _authed.* routes share this layout. The loader runs /me once at
// router-context boot; if it 401s we redirect to /login with the
// current pathname encoded so post-login bounces back here.

export const Route = createFileRoute('/_authed')({
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery())
    } catch {
      throw redirect({
        to: '/login',
        search: { redirect: location.pathname },
      })
    }
  },
  component: AuthedLayout,
})

function AuthedLayout() {
  const { t } = useTranslation()
  const me = useQuery(meQuery())
  if (me.isPending) {
    return (
      <main className="grid min-h-screen place-items-center text-sm text-muted-foreground">
        {t('app.loading')}
      </main>
    )
  }
  if (!me.data) {
    // beforeLoad already redirected; this is just a safety net.
    return null
  }
  return (
    <AuthedShell me={me.data}>
      <Outlet />
    </AuthedShell>
  )
}
