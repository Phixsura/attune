import { renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useHotkeys } from './use-hotkeys'

function fireKey(key: string, opts: Partial<KeyboardEventInit> = {}) {
  document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, ...opts }))
}

describe('useHotkeys', () => {
  it('fires handler on matching key', () => {
    const handler = vi.fn()
    renderHook(() => useHotkeys([{ key: '?', handler, description: 'help' }]))
    fireKey('?')
    expect(handler).toHaveBeenCalledOnce()
  })

  it('does not fire when disabled', () => {
    const handler = vi.fn()
    renderHook(() => useHotkeys([{ key: '?', handler, description: 'help' }], false))
    fireKey('?')
    expect(handler).not.toHaveBeenCalled()
  })

  it('respects mod flag', () => {
    const handler = vi.fn()
    renderHook(() => useHotkeys([{ key: 's', mod: true, handler, description: 'save' }]))
    fireKey('s')
    expect(handler).not.toHaveBeenCalled()
    fireKey('s', { metaKey: true })
    expect(handler).toHaveBeenCalledOnce()
  })

  it('skips when target is an input element', () => {
    const handler = vi.fn()
    renderHook(() => useHotkeys([{ key: 'j', handler, description: 'next' }]))
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }))
    expect(handler).not.toHaveBeenCalled()
    document.body.removeChild(input)
  })
})
