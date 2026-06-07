import { describe, expect, it } from 'vitest'
import { resolveI18n } from '@/lib/i18n-resolve'

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
