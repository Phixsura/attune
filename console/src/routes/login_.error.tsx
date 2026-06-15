import { createFileRoute, Link } from '@tanstack/react-router'
import { AlertCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export const Route = createFileRoute('/login_/error')({
  component: LoginErrorPage,
  validateSearch: (search): { code?: string; request_id?: string; trace_id?: string } => ({
    code: typeof search.code === 'string' ? search.code : undefined,
    request_id: typeof search.request_id === 'string' ? search.request_id : undefined,
    trace_id: typeof search.trace_id === 'string' ? search.trace_id : undefined,
  }),
})

function LoginErrorPage() {
  const { t } = useTranslation()
  const { code, request_id, trace_id } = Route.useSearch()

  const errorKey = code && isKnownErrorCode(code) ? code : 'auth_failed'
  const errorMessage = t(`auth.sso_error.${errorKey}`)

  return (
    <main className="grid min-h-screen place-items-center bg-muted/30">
      <div className="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm text-center">
        <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-destructive/10">
          <AlertCircle className="size-6 text-destructive" />
        </div>

        <h1 className="mt-4 text-xl font-semibold tracking-tight">{t('auth.login_failed')}</h1>

        <p className="mt-2 text-sm text-muted-foreground">{errorMessage}</p>

        {(request_id || trace_id) && (
          <div className="mt-2 space-y-1 font-mono text-xs text-muted-foreground">
            {request_id && (
              <p>
                {t('auth.request_id')}: {request_id}
              </p>
            )}
            {trace_id && (
              <p>
                {t('auth.trace_id')}: {trace_id}
              </p>
            )}
          </div>
        )}

        <Button asChild size="lg" className="mt-6 w-full">
          <Link to="/login">{t('auth.try_again')}</Link>
        </Button>
      </div>
    </main>
  )
}

const knownErrorCodes = [
  'state_expired',
  'state_mismatch',
  'token_failed',
  'not_in_group',
  'user_cancelled',
  'auth_failed',
  'internal_error',
] as const

function isKnownErrorCode(code: string): code is (typeof knownErrorCodes)[number] {
  return knownErrorCodes.includes(code as (typeof knownErrorCodes)[number])
}
