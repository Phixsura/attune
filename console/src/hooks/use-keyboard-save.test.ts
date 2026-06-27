import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useKeyboardSave } from './use-keyboard-save'

function fireKey(key: string, modifiers: { metaKey?: boolean; ctrlKey?: boolean } = {}) {
  document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...modifiers }))
}

describe('useKeyboardSave', () => {
  it('calls onSave on Cmd+S', () => {
    const onSave = vi.fn()
    renderHook(() => useKeyboardSave({ onSave }))
    fireKey('s', { metaKey: true })
    expect(onSave).toHaveBeenCalledOnce()
  })

  it('calls onSave on Ctrl+S', () => {
    const onSave = vi.fn()
    renderHook(() => useKeyboardSave({ onSave }))
    fireKey('s', { ctrlKey: true })
    expect(onSave).toHaveBeenCalledOnce()
  })

  it('calls onSubmit on Cmd+Enter', () => {
    const onSubmit = vi.fn()
    renderHook(() => useKeyboardSave({ onSubmit }))
    fireKey('Enter', { metaKey: true })
    expect(onSubmit).toHaveBeenCalledOnce()
  })

  it('does not fire when enabled=false', () => {
    const onSave = vi.fn()
    renderHook(() => useKeyboardSave({ onSave, enabled: false }))
    fireKey('s', { metaKey: true })
    expect(onSave).not.toHaveBeenCalled()
  })

  it('does not fire without modifier key', () => {
    const onSave = vi.fn()
    renderHook(() => useKeyboardSave({ onSave }))
    fireKey('s')
    expect(onSave).not.toHaveBeenCalled()
  })

  it('cleans up listener on unmount', () => {
    const onSave = vi.fn()
    const { unmount } = renderHook(() => useKeyboardSave({ onSave }))
    unmount()
    fireKey('s', { metaKey: true })
    expect(onSave).not.toHaveBeenCalled()
  })

  it('uses ref to avoid stale closure', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = renderHook(
      ({ onSave }: { onSave: () => void }) => useKeyboardSave({ onSave }),
      { initialProps: { onSave: first } },
    )
    rerender({ onSave: second })
    fireKey('s', { metaKey: true })
    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledOnce()
  })

  it('re-attaches listener when enabled toggles', () => {
    const onSave = vi.fn()
    const { rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useKeyboardSave({ onSave, enabled }),
      { initialProps: { enabled: false } },
    )
    fireKey('s', { metaKey: true })
    expect(onSave).not.toHaveBeenCalled()
    rerender({ enabled: true })
    fireKey('s', { metaKey: true })
    expect(onSave).toHaveBeenCalledOnce()
  })
})
