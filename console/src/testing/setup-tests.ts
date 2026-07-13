import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterAll, afterEach, beforeAll, beforeEach, vi } from 'vitest'
import { server } from '@/testing/mocks/server'

const nativeGetComputedStyle = window.getComputedStyle.bind(window)

// Network-boundary mock lifecycle (TkDodo's Testing React Query + Kent
// C. Dodds's Stop mocking fetch). onUnhandledRequest: 'error' makes
// "added an endpoint and forgot to mock it" fail loudly instead of
// returning undefined.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
beforeEach(() => {
  // Reset call history on every vi.fn (including module-level mocks
  // hoisted by vi.mock at file top — e.g. the sonner mock in
  // dialogs.test.tsx). Without this, a vi.fn defined at module scope
  // accumulates calls across tests, and any future
  // `expect(fn).not.toHaveBeenCalled()` reads as "called" because of
  // a prior test's invocation. Mock IMPLEMENTATIONS are preserved.
  vi.clearAllMocks()
})
afterEach(() => {
  // Explicit RTL cleanup — auto-cleanup relies on vitest's afterEach
  // hook being installed, which can fail to register under certain
  // pool/isolate combinations. Always unmount before resetting MSW
  // so React state from the previous test doesn't survive into the
  // next test's beforeEach DOM patches.
  cleanup()
  // Reverse any vi.stubGlobal('fetch', ...) / vi.stubGlobal('navigator', ...)
  // from the previous test. Without this, an early-throwing test that
  // stubbed a global leaks the stub into the next test silently.
  vi.unstubAllGlobals()
  server.resetHandlers()
})
afterAll(() => server.close())

beforeEach(() => {
  // jsdom doesn't implement these APIs but Radix Dialog/Select rely on
  // them. Shim once per test so a global mutated by a prior test
  // doesn't leak across files.
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = vi.fn()
    },
  )
  if (!window.matchMedia) {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    )
  }
  Element.prototype.scrollIntoView = vi.fn()
  window.scrollTo = vi.fn()
  if (!Element.prototype.hasPointerCapture) Element.prototype.hasPointerCapture = vi.fn(() => false)
  if (!Element.prototype.releasePointerCapture) Element.prototype.releasePointerCapture = vi.fn()
  if (!Element.prototype.setPointerCapture) Element.prototype.setPointerCapture = vi.fn()
  window.getComputedStyle = ((element: Element) =>
    nativeGetComputedStyle(element)) as typeof window.getComputedStyle
  HTMLCanvasElement.prototype.getContext = vi.fn(() => ({
    arc: vi.fn(),
    beginPath: vi.fn(),
    clearRect: vi.fn(),
    clip: vi.fn(),
    closePath: vi.fn(),
    createImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) })),
    drawImage: vi.fn(),
    fill: vi.fn(),
    fillRect: vi.fn(),
    fillText: vi.fn(),
    getImageData: vi.fn(() => ({ data: new Uint8ClampedArray(4) })),
    lineTo: vi.fn(),
    measureText: vi.fn(() => ({ width: 0 })),
    moveTo: vi.fn(),
    putImageData: vi.fn(),
    rect: vi.fn(),
    restore: vi.fn(),
    rotate: vi.fn(),
    save: vi.fn(),
    scale: vi.fn(),
    setTransform: vi.fn(),
    stroke: vi.fn(),
    transform: vi.fn(),
    translate: vi.fn(),
  })) as unknown as typeof HTMLCanvasElement.prototype.getContext

  // Note: navigator.clipboard is NOT shimmed here. user-event v14's
  // setup() installs its own Clipboard mock that survives the test
  // suite. Component tests that need to assert on clipboard.writeText
  // should `vi.spyOn(navigator.clipboard, 'writeText')` AFTER
  // renderWithProviders. See dialogs.test.tsx for the canonical
  // pattern.
})
