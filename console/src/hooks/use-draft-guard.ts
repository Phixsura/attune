import { useBlocker } from '@tanstack/react-router'
import { useCallback, useEffect, useRef, useState } from 'react'

const PREFIX = 'attune:draft:'
const DEBOUNCE_MS = 500
const DRAFT_VERSION = 1
const DEFAULT_TTL_MS = 24 * 60 * 60 * 1000

interface StoredEnvelope<T> {
  _v: number
  _ts: number
  data: T
}

function isEnvelope(val: unknown): val is StoredEnvelope<unknown> {
  return (
    typeof val === 'object' &&
    val !== null &&
    typeof (val as Record<string, unknown>)._v === 'number' &&
    typeof (val as Record<string, unknown>)._ts === 'number' &&
    'data' in val
  )
}

export function readDraft<T>(storageKey: string, ttlMs = DEFAULT_TTL_MS): T | null {
  const key = PREFIX + storageKey
  try {
    let raw = localStorage.getItem(key)
    if (!raw) {
      raw = sessionStorage.getItem(key)
      if (raw) {
        try {
          localStorage.setItem(key, raw)
          sessionStorage.removeItem(key)
        } catch {
          /* migration best-effort — keep sessionStorage copy if localStorage is full */
        }
      }
    }
    if (!raw) return null
    const parsed = JSON.parse(raw)
    if (!isEnvelope(parsed)) {
      localStorage.removeItem(key)
      return null
    }
    if (parsed._v > DRAFT_VERSION) {
      localStorage.removeItem(key)
      return null
    }
    if (Date.now() - parsed._ts > ttlMs) {
      localStorage.removeItem(key)
      return null
    }
    return parsed.data as T
  } catch {
    return null
  }
}

export function readDraftAge(storageKey: string, ttlMs = DEFAULT_TTL_MS): number | null {
  const key = PREFIX + storageKey
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    const parsed = JSON.parse(raw)
    if (!isEnvelope(parsed)) return null
    const age = Date.now() - parsed._ts
    if (age > ttlMs) return null
    return age
  } catch {
    return null
  }
}

export function clearStoredDraft(storageKey: string): void {
  const key = PREFIX + storageKey
  localStorage.removeItem(key)
  sessionStorage.removeItem(key)
}

function writeDraft<T>(storageKey: string, data: T): boolean {
  const key = PREFIX + storageKey
  try {
    const envelope: StoredEnvelope<T> = { _v: DRAFT_VERSION, _ts: Date.now(), data }
    localStorage.setItem(key, JSON.stringify(envelope))
    return true
  } catch {
    return false
  }
}

interface UseDraftGuardOpts<T> {
  storageKey: string
  draft: T
  dirty: boolean
  disabled?: boolean
  onExternalSave?: () => void
}

interface UseDraftGuardReturn {
  dialogOpen: boolean
  confirmLeave: () => void
  cancelLeave: () => void
  proceed: () => void
  clearDraft: () => void
  draftAge: number | null
}

export function useDraftGuard<T>(opts: UseDraftGuardOpts<T>): UseDraftGuardReturn {
  const { storageKey, draft, dirty, disabled = false } = opts
  const [initialAge] = useState(() => readDraftAge(storageKey))
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const channelRef = useRef<BroadcastChannel | null>(null)
  const draftRef = useRef(draft)
  draftRef.current = draft
  const dirtyRef = useRef(dirty)
  dirtyRef.current = dirty
  const onExternalSaveRef = useRef(opts.onExternalSave)
  onExternalSaveRef.current = opts.onExternalSave

  // biome-ignore lint/correctness/useExhaustiveDependencies: draft triggers timer reset; callback reads latest via ref
  useEffect(() => {
    if (disabled || !dirty) return
    timerRef.current = setTimeout(() => {
      if (dirtyRef.current) writeDraft(storageKey, draftRef.current)
    }, DEBOUNCE_MS)
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current)
    }
  }, [storageKey, draft, dirty, disabled])

  useEffect(() => {
    if (disabled) return
    const flush = () => {
      if (dirtyRef.current) writeDraft(storageKey, draftRef.current)
    }
    window.addEventListener('beforeunload', flush)
    return () => window.removeEventListener('beforeunload', flush)
  }, [storageKey, disabled])

  useEffect(() => {
    if (disabled || !dirty) return
    document.title = `● ${document.title.replace(/^● /, '')}`
    return () => {
      document.title = document.title.replace(/^● /, '')
    }
  }, [dirty, disabled])

  useEffect(() => {
    if (disabled || typeof BroadcastChannel === 'undefined') return
    const ch = new BroadcastChannel('attune-draft')
    channelRef.current = ch
    ch.onmessage = (e) => {
      if (e.data?.type === 'draft-cleared' && e.data.key === storageKey) {
        onExternalSaveRef.current?.()
      }
    }
    return () => {
      channelRef.current = null
      ch.close()
    }
  }, [storageKey, disabled])

  const isBlocked = !disabled && dirty
  const isBlockedRef = useRef(isBlocked)
  isBlockedRef.current = isBlocked

  const blocker = useBlocker({
    shouldBlockFn: () => isBlockedRef.current,
    withResolver: true,
    disabled: !isBlocked,
  })

  const clearDraft = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
    dirtyRef.current = false
    clearStoredDraft(storageKey)
    channelRef.current?.postMessage({ type: 'draft-cleared', key: storageKey })
  }, [storageKey])

  return {
    dialogOpen: blocker.status === 'blocked',
    confirmLeave: () => {
      clearDraft()
      blocker.proceed?.()
    },
    cancelLeave: () => {
      blocker.reset?.()
    },
    proceed: () => {
      blocker.proceed?.()
    },
    clearDraft,
    draftAge: initialAge,
  }
}
