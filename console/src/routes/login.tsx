import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { type ApiError, api } from '@/lib/api-client'
import type { LoginRequest, LoginResponse } from '@/proto/attune/v1/session'

// Login is local-admin email + password (#66 replaces the external
// OAuth button). The form POSTs JSON to /fb/v1/console/install/login; on
// success the server returns { redirect: "/console/..." } which we
// honour (or fall back to /console/).
//
// Body + response shapes come from the generated proto types
// (proto/attune/v1/session.proto → console/src/proto/attune/v1/session.ts):
// canonical protoJSON uses camelCase keys (redirectUri, not redirect_uri).
// The first admin is bootstrapped by the backend at startup from
// ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE] env vars when the
// admins table is empty — there is no first-time signup UI here.
//
// We route through the shared `api()` helper (review M8, #66) so login
// errors surface the same {code, message, requestId} envelope as every
// other mutation — and the SPA can show `requestId` in tooltips so
// support can find the matching server log line.

export const Route = createFileRoute('/login')({
  component: LoginPage,
  validateSearch: (search): { redirect?: string } => ({
    redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
  }),
})

function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { redirect } = Route.useSearch()

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [requestId, setRequestId] = useState<string | undefined>(undefined)
  const [submitting, setSubmitting] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setRequestId(undefined)
    setSubmitting(true)
    try {
      const body: LoginRequest = {
        email,
        password,
        redirectUri: redirect ?? '/console/',
      }
      const data = await api<LoginResponse>('/fb/v1/console/install/login', {
        method: 'POST',
        body,
      })
      await navigate({ to: data.redirect || '/console/' })
    } catch (err) {
      const apiErr = err as ApiError
      setRequestId(apiErr.requestId)
      if (apiErr.status === 423) {
        setError(t('auth.locked_out'))
      } else if (apiErr.status === 401 || apiErr.status === 400) {
        setError(t('auth.invalid_credentials'))
      } else if (apiErr.status === 403) {
        // Login-CSRF defence rejected the request — almost certainly a
        // misconfigured reverse-proxy stripping Origin. Surface the
        // requestId so the operator can grep the server log.
        setError(t('auth.cross_site_rejected'))
      } else {
        setError(apiErr.message || t('auth.login_failed'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="grid min-h-screen place-items-center bg-muted/30">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm rounded-xl border border-border bg-card p-8 shadow-sm"
      >
        <h1 className="text-xl font-semibold tracking-tight">{t('app.title')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{t('auth.login_hint_admin')}</p>

        <div className="mt-6 space-y-4">
          <div className="space-y-2">
            <Label htmlFor="email">{t('auth.email_label')}</Label>
            <Input
              id="email"
              type="email"
              required
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={submitting}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">{t('auth.password_label')}</Label>
            <Input
              id="password"
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={submitting}
            />
          </div>
        </div>

        {error && (
          <p className="mt-4 text-sm text-destructive">
            {error}
            {requestId && (
              <span className="ml-2 font-mono text-xs text-muted-foreground">({requestId})</span>
            )}
          </p>
        )}

        <Button type="submit" size="lg" className="mt-6 w-full" disabled={submitting}>
          {submitting ? t('auth.logging_in') : t('auth.login_submit')}
        </Button>
      </form>
    </main>
  )
}
