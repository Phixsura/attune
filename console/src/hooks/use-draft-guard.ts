import { useBlocker } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'

const PREFIX = 'attune:draft:'
const DEBOUNCE_MS = 500

export function readDraft<T>(storageKey: string): T | null {
  try {
    const raw = sessionStorage.getItem(PREFIX + storageKey)
    if (!raw) return null
    return JSON.parse(raw) as T
  } catch {
    return null
  }
}

export function clearStoredDraft(storageKey: string): void {
  sessionStorage.removeItem(PREFIX + storageKey)
}

interface UseDraftGuardOpts<T> {
  storageKey: string
  draft: T
  dirty: boolean
  disabled?: boolean
}

interface UseDraftGuardReturn {
  dialogOpen: boolean
  confirmLeave: () => void
  cancelLeave: () => void
  clearDraft: () => void
}

export function useDraftGuard<T>(opts: UseDraftGuardOpts<T>): UseDraftGuardReturn {
  const { storageKey, draft, dirty, disabled = false } = opts
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (disabled || !dirty) return
    timerRef.current = setTimeout(() => {
      sessionStorage.setItem(PREFIX + storageKey, JSON.stringify(draft))
    }, DEBOUNCE_MS)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [storageKey, draft, dirty, disabled])

  const isBlocked = !disabled && dirty

  const blocker = useBlocker({
    shouldBlockFn: () => isBlocked,
    withResolver: true,
    enableBeforeUnload: isBlocked,
    disabled: !isBlocked,
  })

  const clearDraft = () => clearStoredDraft(storageKey)

  const confirmLeave = () => {
    clearDraft()
    blocker.proceed?.()
  }

  const cancelLeave = () => {
    blocker.reset?.()
  }

  return {
    dialogOpen: blocker.status === 'blocked',
    confirmLeave,
    cancelLeave,
    clearDraft,
  }
}
