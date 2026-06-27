import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearStoredDraft, readDraft, readDraftAge, useDraftGuard } from './use-draft-guard'

vi.mock('@tanstack/react-router', () => ({
  useBlocker: () => ({ status: 'idle', proceed: undefined, reset: undefined }),
}))

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
})

describe('readDraft', () => {
  it('returns null when nothing stored', () => {
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
  })

  it('returns parsed data from localStorage envelope', () => {
    localStorage.setItem(
      'attune:draft:test-key',
      JSON.stringify({ _v: 1, _ts: Date.now(), data: { x: 42 } }),
    )
    expect(readDraft<{ x: number }>('test-key')).toEqual({ x: 42 })
  })

  it('migrates from sessionStorage to localStorage', () => {
    const envelope = JSON.stringify({ _v: 1, _ts: Date.now(), data: { x: 99 } })
    sessionStorage.setItem('attune:draft:test-key', envelope)
    expect(readDraft<{ x: number }>('test-key')).toEqual({ x: 99 })
    expect(localStorage.getItem('attune:draft:test-key')).not.toBeNull()
    expect(sessionStorage.getItem('attune:draft:test-key')).toBeNull()
  })

  it('keeps sessionStorage copy when localStorage migration fails', () => {
    const data = JSON.stringify({ _v: 1, _ts: Date.now(), data: { x: 42 } })
    sessionStorage.setItem('attune:draft:test-key', data)
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota exceeded', 'QuotaExceededError')
    })
    const result = readDraft<{ x: number }>('test-key')
    expect(result).toEqual({ x: 42 })
    expect(sessionStorage.getItem('attune:draft:test-key')).not.toBeNull()
    setItemSpy.mockRestore()
  })

  it('returns null on malformed JSON', () => {
    localStorage.setItem('attune:draft:test-key', '{bad json')
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
  })

  it('returns null and removes expired drafts', () => {
    localStorage.setItem(
      'attune:draft:test-key',
      JSON.stringify({ _v: 1, _ts: Date.now() - 25 * 60 * 60 * 1000, data: { x: 1 } }),
    )
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
    expect(localStorage.getItem('attune:draft:test-key')).toBeNull()
  })

  it('discards legacy non-envelope data and cleans up storage', () => {
    localStorage.setItem('attune:draft:test-key', JSON.stringify({ x: 7 }))
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
    expect(localStorage.getItem('attune:draft:test-key')).toBeNull()
  })

  it('discards drafts from a future schema version', () => {
    localStorage.setItem(
      'attune:draft:test-key',
      JSON.stringify({ _v: 999, _ts: Date.now(), data: { x: 1 } }),
    )
    expect(readDraft<{ x: number }>('test-key')).toBeNull()
    expect(localStorage.getItem('attune:draft:test-key')).toBeNull()
  })
})

describe('readDraftAge', () => {
  it('returns null when no draft', () => {
    expect(readDraftAge('nope')).toBeNull()
  })

  it('returns age in ms', () => {
    const ts = Date.now() - 5000
    localStorage.setItem('attune:draft:test-key', JSON.stringify({ _v: 1, _ts: ts, data: {} }))
    const age = readDraftAge('test-key')
    expect(age).not.toBeNull()
    // biome-ignore lint/style/noNonNullAssertion: guarded by toBeNull above
    expect(age!).toBeGreaterThanOrEqual(5000)
  })

  it('returns null for legacy non-envelope data', () => {
    localStorage.setItem('attune:draft:test-key', JSON.stringify({ x: 1 }))
    expect(readDraftAge('test-key')).toBeNull()
  })

  it('returns null for expired drafts', () => {
    localStorage.setItem(
      'attune:draft:test-key',
      JSON.stringify({ _v: 1, _ts: Date.now() - 25 * 60 * 60 * 1000, data: {} }),
    )
    expect(readDraftAge('test-key')).toBeNull()
  })
})

describe('clearStoredDraft', () => {
  it('removes from both storages', () => {
    localStorage.setItem('attune:draft:test-key', '{}')
    sessionStorage.setItem('attune:draft:test-key', '{}')
    clearStoredDraft('test-key')
    expect(localStorage.getItem('attune:draft:test-key')).toBeNull()
    expect(sessionStorage.getItem('attune:draft:test-key')).toBeNull()
  })
})

describe('useDraftGuard', () => {
  it('writes draft to localStorage after debounce when dirty', async () => {
    vi.useFakeTimers()
    const draft = { prompt: 'hello' }
    renderHook(() => useDraftGuard({ storageKey: 'test', draft, dirty: true }))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    await act(() => {
      vi.advanceTimersByTime(500)
    })
    const raw = localStorage.getItem('attune:draft:test')
    expect(raw).not.toBeNull()
    // biome-ignore lint/style/noNonNullAssertion: guarded by toBeNull above
    const envelope = JSON.parse(raw!)
    expect(envelope._v).toBe(1)
    expect(envelope.data).toEqual(draft)
    vi.useRealTimers()
  })

  it('does not write when dirty is false', async () => {
    vi.useFakeTimers()
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { x: 1 }, dirty: false }))
    await act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
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
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('clearDraft removes stored value from localStorage', () => {
    localStorage.setItem('attune:draft:test', '{"a":1}')
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { a: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
  })

  it('cancels pending debounce timer on unmount', async () => {
    vi.useFakeTimers()
    const { unmount } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    unmount()
    await act(() => {
      vi.advanceTimersByTime(600)
    })
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('debounces rapid draft changes — only the latest value is persisted', async () => {
    vi.useFakeTimers()
    const { rerender } = renderHook(
      ({ draft }: { draft: { v: number } }) =>
        useDraftGuard({ storageKey: 'test', draft, dirty: true }),
      { initialProps: { draft: { v: 1 } } },
    )
    await act(() => vi.advanceTimersByTime(200))
    rerender({ draft: { v: 2 } })
    await act(() => vi.advanceTimersByTime(200))
    rerender({ draft: { v: 3 } })
    await act(() => vi.advanceTimersByTime(600))
    const raw = localStorage.getItem('attune:draft:test')
    expect(raw).not.toBeNull()
    // biome-ignore lint/style/noNonNullAssertion: guarded by toBeNull above
    const envelope = JSON.parse(raw!)
    expect(envelope.data).toEqual({ v: 3 })
    vi.useRealTimers()
  })

  it('re-activates guard after clearDraft + new dirty edit', async () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook(
      ({ draft, dirty }: { draft: { v: number }; dirty: boolean }) =>
        useDraftGuard({ storageKey: 'test', draft, dirty }),
      { initialProps: { draft: { v: 1 }, dirty: true } },
    )
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).not.toBeNull()
    act(() => result.current.clearDraft())
    rerender({ draft: { v: 1 }, dirty: false })
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    rerender({ draft: { v: 2 }, dirty: true })
    await act(() => vi.advanceTimersByTime(600))
    const raw = localStorage.getItem('attune:draft:test')
    expect(raw).not.toBeNull()
    // biome-ignore lint/style/noNonNullAssertion: guarded by toBeNull above
    const envelope = JSON.parse(raw!)
    expect(envelope.data).toEqual({ v: 2 })
    vi.useRealTimers()
  })

  it('transitions from disabled to enabled preserves guard behavior', async () => {
    vi.useFakeTimers()
    const { rerender } = renderHook(
      ({ disabled }: { disabled: boolean }) =>
        useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true, disabled }),
      { initialProps: { disabled: true } },
    )
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    rerender({ disabled: false })
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).not.toBeNull()
    vi.useRealTimers()
  })

  it('survives localStorage quota exceeded without throwing', async () => {
    vi.useFakeTimers()
    const setItemSpy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('quota exceeded', 'QuotaExceededError')
    })
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }))
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    setItemSpy.mockRestore()
    vi.useRealTimers()
  })

  it('clearDraft is idempotent — calling twice does not throw', () => {
    localStorage.setItem('attune:draft:test', '{"v":1}')
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    act(() => result.current.clearDraft())
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
  })

  it('clearDraft cancels pending debounce timer so stale draft is not re-written', async () => {
    vi.useFakeTimers()
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    await act(() => vi.advanceTimersByTime(600))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('debounce timer does not resurrect draft after clearDraft even if dirty prop is still true', async () => {
    vi.useFakeTimers()
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    // Timer scheduled but not yet fired
    await act(() => vi.advanceTimersByTime(200))
    act(() => result.current.clearDraft())
    // Timer fires — should NOT write because dirtyRef is false
    await act(() => vi.advanceTimersByTime(400))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
    vi.useRealTimers()
  })

  it('returns proceed function', () => {
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: false }),
    )
    expect(typeof result.current.proceed).toBe('function')
  })

  it('returns draftAge', () => {
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: false }),
    )
    expect(result.current.draftAge).toBeNull()
  })

  it('flushes draft to localStorage on beforeunload when dirty', () => {
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { v: 99 }, dirty: true }))
    window.dispatchEvent(new Event('beforeunload'))
    const raw = localStorage.getItem('attune:draft:test')
    expect(raw).not.toBeNull()
    // biome-ignore lint/style/noNonNullAssertion: guarded by toBeNull above
    const envelope = JSON.parse(raw!)
    expect(envelope.data).toEqual({ v: 99 })
  })

  it('does not flush on beforeunload when not dirty', () => {
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: false }))
    window.dispatchEvent(new Event('beforeunload'))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
  })

  it('clearDraft prevents beforeunload from re-writing', () => {
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    window.dispatchEvent(new Event('beforeunload'))
    expect(localStorage.getItem('attune:draft:test')).toBeNull()
  })

  it('broadcasts draft-cleared on BroadcastChannel when clearDraft is called', async () => {
    const messages: unknown[] = []
    const ch = new BroadcastChannel('attune-draft')
    ch.onmessage = (e) => messages.push(e.data)

    localStorage.setItem('attune:draft:test', '{"v":1}')
    const { result } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    act(() => result.current.clearDraft())
    await vi.waitFor(() => {
      expect(messages).toContainEqual({ type: 'draft-cleared', key: 'test' })
    })
    ch.close()
  })

  it('calls onExternalSave when receiving draft-cleared from another tab', async () => {
    const onExternalSave = vi.fn()
    renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: false, onExternalSave }),
    )
    const sender = new BroadcastChannel('attune-draft')
    sender.postMessage({ type: 'draft-cleared', key: 'test' })
    sender.close()
    await vi.waitFor(() => expect(onExternalSave).toHaveBeenCalledOnce())
  })

  it('ignores BroadcastChannel messages for other keys', async () => {
    const onExternalSave = vi.fn()
    renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: false, onExternalSave }),
    )
    const sender = new BroadcastChannel('attune-draft')
    sender.postMessage({ type: 'draft-cleared', key: 'other-key' })
    sender.close()
    await new Promise((r) => setTimeout(r, 50))
    expect(onExternalSave).not.toHaveBeenCalled()
  })

  it('does not create BroadcastChannel when disabled', async () => {
    const onExternalSave = vi.fn()
    renderHook(() =>
      useDraftGuard({
        storageKey: 'test',
        draft: { v: 1 },
        dirty: false,
        disabled: true,
        onExternalSave,
      }),
    )
    const sender = new BroadcastChannel('attune-draft')
    sender.postMessage({ type: 'draft-cleared', key: 'test' })
    sender.close()
    await new Promise((r) => setTimeout(r, 50))
    expect(onExternalSave).not.toHaveBeenCalled()
  })

  it('adds dirty indicator to document.title when dirty', () => {
    document.title = 'Settings'
    renderHook(() => useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }))
    expect(document.title).toBe('● Settings')
  })

  it('removes dirty indicator from document.title when dirty becomes false', () => {
    document.title = 'Settings'
    const { rerender } = renderHook(
      ({ dirty }: { dirty: boolean }) =>
        useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty }),
      { initialProps: { dirty: true } },
    )
    expect(document.title).toBe('● Settings')
    rerender({ dirty: false })
    expect(document.title).toBe('Settings')
  })

  it('cleans up document.title on unmount while dirty', () => {
    document.title = 'Settings'
    const { unmount } = renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true }),
    )
    expect(document.title).toBe('● Settings')
    unmount()
    expect(document.title).toBe('Settings')
  })

  it('does not add title indicator when disabled', () => {
    document.title = 'Settings'
    renderHook(() =>
      useDraftGuard({ storageKey: 'test', draft: { v: 1 }, dirty: true, disabled: true }),
    )
    expect(document.title).toBe('Settings')
  })
})
