import { describe, expect, it } from 'vitest'
import { Route as LoginRoute } from '@/routes/login'

describe('Login route validateSearch', () => {
  const validateSearch = LoginRoute.options.validateSearch as (s: Record<string, unknown>) => {
    redirect?: string
  }

  it('extracts valid redirect from search params', () => {
    expect(validateSearch({ redirect: '/feedback' })).toEqual({ redirect: '/feedback' })
  })

  it('returns undefined for non-string redirect', () => {
    expect(validateSearch({ redirect: 123 })).toEqual({ redirect: undefined })
    expect(validateSearch({})).toEqual({ redirect: undefined })
  })
})
