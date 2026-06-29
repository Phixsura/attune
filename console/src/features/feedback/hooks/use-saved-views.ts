import { useCallback, useSyncExternalStore } from 'react'

const STORAGE_KEY = 'attune:feedback:savedViews'

export interface SavedViewFilters {
  attrFilters: Record<string, string>
  tagFilter: string
  workflowFilter: string
  enrichmentFilter: string
  urgentOnly: boolean
  queueMode: string
  sortMode: string
  q: string
}

export interface SavedView {
  id: string
  name: string
  filters: SavedViewFilters
  createdAt: string
}

function readViews(): SavedView[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    return JSON.parse(raw) as SavedView[]
  } catch {
    return []
  }
}

function writeViews(views: SavedView[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(views))
  window.dispatchEvent(new StorageEvent('storage', { key: STORAGE_KEY }))
}

let viewsCache: SavedView[] | null = null

function subscribe(cb: () => void): () => void {
  const handler = (e: StorageEvent) => {
    if (e.key === STORAGE_KEY || e.key === null) {
      viewsCache = null
      cb()
    }
  }
  window.addEventListener('storage', handler)
  return () => window.removeEventListener('storage', handler)
}

function getSnapshot(): SavedView[] {
  if (viewsCache === null) {
    viewsCache = readViews()
  }
  return viewsCache
}

export function useSavedViews() {
  const views = useSyncExternalStore(subscribe, getSnapshot, () => [])

  const save = useCallback((name: string, filters: SavedViewFilters) => {
    const existing = readViews()
    const view: SavedView = {
      id: crypto.randomUUID(),
      name,
      filters,
      createdAt: new Date().toISOString(),
    }
    viewsCache = null
    writeViews([view, ...existing])
    return view
  }, [])

  const remove = useCallback((id: string) => {
    const existing = readViews()
    viewsCache = null
    writeViews(existing.filter((v) => v.id !== id))
  }, [])

  return { views, save, remove }
}
