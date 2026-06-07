import '@testing-library/jest-dom/vitest'
import { afterAll, afterEach, beforeAll, beforeEach, vi } from 'vitest'
import { server } from '@/testing/mocks/server'

// Network-boundary mock lifecycle (TkDodo's Testing React Query + Kent
// C. Dodds's Stop mocking fetch). onUnhandledRequest: 'error' makes
// "added an endpoint and forgot to mock it" fail loudly instead of
// returning undefined.
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

beforeEach(() => {
  // jsdom doesn't implement these APIs but Radix Dialog/Select rely on
  // them. Shim once per test so a global mutated by a prior test
  // doesn't leak across files.
  if (!('ResizeObserver' in window)) {
    vi.stubGlobal(
      'ResizeObserver',
      vi.fn(() => ({ observe: vi.fn(), unobserve: vi.fn(), disconnect: vi.fn() })),
    )
  }
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
  if (!Element.prototype.hasPointerCapture) Element.prototype.hasPointerCapture = vi.fn(() => false)
  if (!Element.prototype.releasePointerCapture) Element.prototype.releasePointerCapture = vi.fn()
  if (!Element.prototype.setPointerCapture) Element.prototype.setPointerCapture = vi.fn()

  // SecretKeyDialog uses navigator.clipboard; jsdom has none.
  Object.assign(navigator, {
    clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
  })
})
