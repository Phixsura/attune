import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'
import i18n from '@/i18n'
import { resolveI18n, useDisplayName } from '@/lib/i18n-resolve'

describe('resolveI18n', () => {
  const m = { 'zh-CN': '中文', zh: '中', en: 'English', default: 'fallback' }

  it('picks the first non-empty preferred locale in order', () => {
    expect(resolveI18n(m, ['zh-CN', 'zh', 'en'])).toBe('中文')
    expect(resolveI18n(m, ['zh', 'en'])).toBe('中')
    expect(resolveI18n(m, ['en', 'zh'])).toBe('English')
  })

  it('skips empty values during preferred walk and falls through', () => {
    const sparse = { 'zh-CN': '', zh: '', en: 'fallback-en', default: 'D' }
    expect(resolveI18n(sparse, ['zh-CN', 'zh', 'en'])).toBe('fallback-en')
  })

  it('falls back to the "default" entry when no preferred locale matches', () => {
    expect(resolveI18n({ default: 'D' }, ['zh-CN', 'zh', 'en'])).toBe('D')
  })

  it('falls back to ANY non-empty entry when neither preferred nor default match', () => {
    // resolver's "last resort" walk over Object.values
    expect(resolveI18n({ fr: 'F' }, ['zh-CN', 'zh', 'en'])).toBe('F')
  })

  it('returns "" for an empty / null / undefined map', () => {
    expect(resolveI18n({}, ['en'])).toBe('')
    expect(resolveI18n(null, ['en'])).toBe('')
    expect(resolveI18n(undefined, ['en'])).toBe('')
  })

  it('unwraps the proto-style { entries: ... } wrapper transparently', () => {
    const wrapped = { entries: { 'zh-CN': 'wrapped', default: 'D' } }
    expect(resolveI18n(wrapped, ['zh-CN'])).toBe('wrapped')
    expect(resolveI18n(wrapped, ['en'])).toBe('D') // falls to default
  })

  it('returns "" for a wrapper with an empty entries map', () => {
    expect(resolveI18n({ entries: {} }, ['en'])).toBe('')
  })
})

describe('useDisplayName', () => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nextProvider i18n={i18n}>{children}</I18nextProvider>
  )

  it('builds chain "zh-CN → zh → en" from i18n.language=zh-CN and resolves accordingly', async () => {
    // i18n is initialized at module load with lng='zh-CN' (see src/i18n/index.ts).
    await i18n.changeLanguage('zh-CN')
    const { result } = renderHook(() => useDisplayName(), { wrapper })
    expect(result.current({ 'zh-CN': '中文', en: 'E' })).toBe('中文')
    // No zh-CN/zh hit; en in chain.
    expect(result.current({ en: 'E' })).toBe('E')
    // No locale in chain matches → falls through to default.
    expect(result.current({ default: 'D' })).toBe('D')
    // Empty map → ''.
    expect(result.current({})).toBe('')
  })

  it('handles a bare language tag (no dash) by chaining "fr → en"', async () => {
    await i18n.changeLanguage('fr')
    const { result } = renderHook(() => useDisplayName(), { wrapper })
    expect(result.current({ fr: 'Bonjour', en: 'Hi' })).toBe('Bonjour')
    expect(result.current({ en: 'Hi' })).toBe('Hi')
    // restore default for the rest of the suite
    await i18n.changeLanguage('zh-CN')
  })
})
