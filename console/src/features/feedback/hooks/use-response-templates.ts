import { useCallback, useSyncExternalStore } from 'react'

export interface ResponseTemplate {
  id: string
  name: string
  content: string
  createdAt: string
}

const STORAGE_KEY = 'attune:response-templates'
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

function readTemplates(): ResponseTemplate[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as ResponseTemplate[]) : []
  } catch {
    return []
  }
}

function writeTemplates(templates: ResponseTemplate[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(templates))
  emit()
}

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

let cached: ResponseTemplate[] | null = null

function getSnapshot(): ResponseTemplate[] {
  const fresh = readTemplates()
  if (cached && JSON.stringify(cached) === JSON.stringify(fresh)) return cached
  cached = fresh
  return fresh
}

export function useResponseTemplates() {
  const templates = useSyncExternalStore(subscribe, getSnapshot, () => [])

  const save = useCallback((name: string, content: string): ResponseTemplate => {
    const all = readTemplates()
    const t: ResponseTemplate = {
      id: crypto.randomUUID(),
      name,
      content,
      createdAt: new Date().toISOString(),
    }
    writeTemplates([...all, t])
    return t
  }, [])

  const update = useCallback((id: string, name: string, content: string) => {
    const all = readTemplates()
    writeTemplates(all.map((t) => (t.id === id ? { ...t, name, content } : t)))
  }, [])

  const remove = useCallback((id: string) => {
    writeTemplates(readTemplates().filter((t) => t.id !== id))
  }, [])

  return { templates, save, update, remove }
}
