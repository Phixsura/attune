import { useEffect, useRef } from 'react'

export interface HotkeyBinding {
  key: string
  mod?: boolean
  shift?: boolean
  handler: () => void
  description: string
  group?: string
}

export function useHotkeys(bindings: HotkeyBinding[], enabled = true): void {
  const bindingsRef = useRef(bindings)
  bindingsRef.current = bindings

  useEffect(() => {
    if (!enabled) return
    const handler = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      const isInput =
        target.tagName === 'INPUT' ||
        target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' ||
        target.isContentEditable
      if (isInput && !e.metaKey && !e.ctrlKey) return

      for (const binding of bindingsRef.current) {
        const wantMod = binding.mod ?? false
        const wantShift = binding.shift ?? false
        const hasMod = e.metaKey || e.ctrlKey
        if (e.key === binding.key && hasMod === wantMod && e.shiftKey === wantShift) {
          e.preventDefault()
          binding.handler()
          return
        }
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [enabled])
}
