import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearStoredDraft, readDraft, useDraftGuard } from './use-draft-guard'

vi.mock('@tanstack/react-router', () => ({
  useBlocker: () => ({ status: 'idle', proceed: undefined, reset: undefined }),
}))

afterEach(() => sessionStorage.clear())

describe('readDraft', () => {
  it('returns null when nothing stored', () => {
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
  })

  it('returns parsed value when stored', () => {
    sessionStorage.setItem('attune:draft:test-key', JSON.stringify({ x: 42 }))
    expect(readDraft<{ x: number }>('test-key')).toEqual({ x: 42 })
  })

  it('returns null on malformed JSON', () => {
    sessionStorage.setItem('attune:draft:test-key', '{bad json')
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
  })
})

describe('clearStoredDraft', () => {
  it('removes the stored key', () => {
    sessionStorage.setItem('attune:draft:test-key', '{}')
    clearStoredDraft('test-key')
    expect(sessionStorage.getItem('attune:draft:test-key')).toBeNull()
  })
})

describe('useDraftGuard', () => {
  it('writes draft to sessionStorage after debounce when dirty', async () => {
    vi.useFakeTimers()
    const draft = { prompt: 'hello' }
    renderHook(() => useDraftGuard({ storageKey: 'test', draft, dirty: true }))
    expect(sessionStorage.getItem('attune:draft:test')).toBeNull()
    await act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(sessionStorage.getItem('attune:draft:test')).toBe(JSON.stringify(draft))
    vi.useRealTimers()
  })

  it('does not write when dirty is false', async () => {
    vi.useFakeTimers()
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { x: 1 }, dirty: false }))
    await act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(sessionStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('does not write when disabled', async () => {
    vi.useFakeTimers()
    renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { x: 1 }, dirty: true, disabled: true }),
    )
    await act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(sessionStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('clearDraft removes stored value', () => {
    sessionStorage.setItem('attune:draft:test', '{"a":1}')
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { a: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    expect(sessionStorage.getItem('attune:draft:test')).toBeNull()
  })
})
