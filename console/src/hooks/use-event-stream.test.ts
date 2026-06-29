import { renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useEventStream } from './use-event-stream'

class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  listeners: Record<string, EventListener[]> = {}
  withCredentials: boolean
  constructor(url: string, opts?: { withCredentials?: boolean }) {
    this.url = url
    this.withCredentials = opts?.withCredentials ?? false
    MockEventSource.instances.push(this)
  }
  addEventListener(type: string, listener: EventListener) {
    if (!this.listeners[type]) {
      this.listeners[type] = []
    }
    this.listeners[type].push(listener)
  }
  close = vi.fn()
}

beforeEach(() => {
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useEventStream', () => {
  it('opens EventSource when enabled', () => {
    const cb = vi.fn()
    renderHook(() => useEventStream(cb))
    expect(MockEventSource.instances).toHaveLength(1)
    expect(MockEventSource.instances[0].url).toBe('/fb/v1/console/events/stream')
  })

  it('does not open EventSource when disabled', () => {
    renderHook(() => useEventStream(vi.fn(), false))
    expect(MockEventSource.instances).toHaveLength(0)
  })

  it('closes EventSource on unmount', () => {
    const { unmount } = renderHook(() => useEventStream(vi.fn()))
    const source = MockEventSource.instances[0]
    unmount()
    expect(source.close).toHaveBeenCalled()
  })

  it('registers listeners for known event types', () => {
    renderHook(() => useEventStream(vi.fn()))
    const source = MockEventSource.instances[0]
    expect(Object.keys(source.listeners)).toContain('feedback.created')
    expect(Object.keys(source.listeners)).toContain('feedback.enriched')
  })
})
