import { afterEach, describe, expect, it, vi } from 'vitest'
import { restoreFocusWhenReady } from '@/lib/focus'

describe('restoreFocusWhenReady', () => {
  const originalRAF = window.requestAnimationFrame

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    Object.defineProperty(window, 'requestAnimationFrame', {
      configurable: true,
      value: originalRAF,
    })
    document.body.replaceChildren()
  })

  it('ignores missing or disconnected focus targets', () => {
    expect(() => restoreFocusWhenReady(null)).not.toThrow()
    expect(() => restoreFocusWhenReady(document.createElement('button'))).not.toThrow()
  })

  it('returns early when the target is already focused', async () => {
    vi.stubGlobal('queueMicrotask', undefined)
    Object.defineProperty(window, 'requestAnimationFrame', {
      configurable: true,
      value: undefined,
    })
    const button = document.createElement('button')
    document.body.append(button)
    button.focus()

    restoreFocusWhenReady(button)
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(document.activeElement).toBe(button)
  })

  it('uses the timer fallback when microtasks and animation frames are unavailable', async () => {
    vi.stubGlobal('queueMicrotask', undefined)
    Object.defineProperty(window, 'requestAnimationFrame', {
      configurable: true,
      value: undefined,
    })
    const target = document.createElement('button')
    const other = document.createElement('button')
    document.body.append(target, other)

    restoreFocusWhenReady(target)
    other.focus()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(document.activeElement).toBe(other)
  })

  it('reclaims focus from controls inside a closed dialog during the retry pass', async () => {
    vi.stubGlobal('queueMicrotask', undefined)
    Object.defineProperty(window, 'requestAnimationFrame', {
      configurable: true,
      value: undefined,
    })
    const target = document.createElement('button')
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    dialog.dataset.state = 'closed'
    const other = document.createElement('button')
    dialog.append(other)
    document.body.append(target, dialog)

    restoreFocusWhenReady(target)
    other.focus()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(document.activeElement).toBe(target)
  })
})
