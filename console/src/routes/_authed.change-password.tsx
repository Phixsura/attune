import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { meQuery } from '@/features/session/api/get-me'
import { type ApiError, api } from '@/lib/api-client'
import type { ChangePasswordResponse } from '@/proto/attune/v1/session'

// Minimum new-password length matches the server-side
// minNewPasswordLen in internal/handlers/console/auth/change_password.go.
const NEW_PASSWORD_MIN_LEN = 12

// ChangePasswordPage — admin-only self-service password rotation. On
// success we invalidate /me (the server may want to re-issue session
// metadata in future) and bounce to /feedback per the spec.
export const Route = createFileRoute('/_authed/change-password')({
  component: ChangePasswordPage,
})

function ChangePasswordPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [requestId, setRequestId] = useState<string | undefined>(undefined)
  const [submitting, setSubmitting] = useState(false)

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setRequestId(undefined)
    if (next.length < NEW_PASSWORD_MIN_LEN) {
      setError(t('auth.change_password.error.too_short', { min: NEW_PASSWORD_MIN_LEN }))
      return
    }
    if (next !== confirm) {
      setError(t('auth.change_password.error.mismatch'))
      return
    }
    if (next === current) {
      setError(t('auth.change_password.error.same'))
      return
    }
    setSubmitting(true)
    try {
      await api<ChangePasswordResponse>('/fb/v1/console/me/change-password', {
        method: 'POST',
        body: {
          currentPassword: current,
          newPassword: next,
        },
      })
      toast.success(t('auth.change_password.success'))
      await qc.invalidateQueries({ queryKey: ['console', 'me'] })
      await qc.ensureQueryData(meQuery())
      await navigate({ to: '/feedback' })
    } catch (err) {
      const apiErr = err as ApiError
      setRequestId(apiErr.requestId)
      if (apiErr.status === 401) {
        setError(t('auth.change_password.error.wrong_current'))
      } else if (apiErr.code === 'weak_password') {
        setError(t('auth.change_password.error.too_short', { min: NEW_PASSWORD_MIN_LEN }))
      } else if (apiErr.code === 'same_password') {
        setError(t('auth.change_password.error.same'))
      } else if (apiErr.status === 403) {
        setError(t('auth.change_password.error.forbidden'))
      } else {
        setError(apiErr.message || t('auth.change_password.error.generic'))
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto max-w-lg">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('auth.change_password.title')}</h1>
        <p className="mt-1 text-sm text-muted-foreground">{t('auth.change_password.subtitle')}</p>
      </div>

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>{t('auth.change_password.form_title')}</CardTitle>
          <CardDescription>{t('auth.change_password.form_help')}</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={onSubmit}>
            <div className="space-y-2">
              <Label htmlFor="cp-current">{t('auth.change_password.current_label')}</Label>
              <Input
                id="cp-current"
                type="password"
                autoComplete="current-password"
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
                disabled={submitting}
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="cp-new">{t('auth.change_password.new_label')}</Label>
              <Input
                id="cp-new"
                type="password"
                autoComplete="new-password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                disabled={submitting}
                required
                minLength={NEW_PASSWORD_MIN_LEN}
              />
              <p className="text-xs text-muted-foreground">
                {t('auth.change_password.new_help', { min: NEW_PASSWORD_MIN_LEN })}
              </p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="cp-confirm">{t('auth.change_password.confirm_label')}</Label>
              <Input
                id="cp-confirm"
                type="password"
                autoComplete="new-password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                disabled={submitting}
                required
                minLength={NEW_PASSWORD_MIN_LEN}
              />
            </div>

            {error && (
              <p className="text-sm text-destructive">
                {error}
                {requestId && (
                  <span className="ml-2 font-mono text-xs text-muted-foreground">
                    ({requestId})
                  </span>
                )}
              </p>
            )}

            <Button type="submit" disabled={submitting} className="w-full">
              {submitting && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('auth.change_password.submit')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
