import { type RefObject, useEffect, useRef } from 'react'
import { restoreFocusWhenReady } from '@/lib/focus'

export function useRestoreFocusOnClose(
  open: boolean,
  restoreFocusRef: RefObject<HTMLElement | null> | undefined,
  enabled = true,
) {
  const wasOpenRef = useRef(open)

  useEffect(() => {
    if (wasOpenRef.current && !open && enabled) {
      restoreFocusWhenReady(restoreFocusRef?.current)
    }
    wasOpenRef.current = open
  }, [enabled, open, restoreFocusRef])
}
