import { useEffect, useRef } from 'react'

export interface SSEEvent {
  type: string
  data: unknown
}

export function useEventStream(onEvent: (evt: SSEEvent) => void, enabled = true) {
  const callbackRef = useRef(onEvent)
  callbackRef.current = onEvent

  useEffect(() => {
    if (!enabled) return

    const url = '/fb/v1/console/events/stream'
    const source = new EventSource(url, { withCredentials: true })

    const handler = (e: MessageEvent) => {
      try {
        const parsed = JSON.parse(e.data) as SSEEvent
        callbackRef.current(parsed)
      } catch {
        // ignore malformed events
      }
    }

    const eventTypes = [
      'feedback.created',
      'feedback.enriched',
      'feedback.updated',
      'feedback.deleted',
      'workflow.transitioned',
    ]
    for (const type of eventTypes) {
      source.addEventListener(type, handler)
    }

    return () => {
      source.close()
    }
  }, [enabled])
}
