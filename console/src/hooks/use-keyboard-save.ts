import { useEffect, useRef } from 'react'

interface UseKeyboardSaveOpts {
  onSave?: () => void
  onSubmit?: () => void
  enabled?: boolean
}

export function useKeyboardSave(opts: UseKeyboardSaveOpts): void {
  const onSaveRef = useRef(opts.onSave)
  onSaveRef.current = opts.onSave
  const onSubmitRef = useRef(opts.onSubmit)
  onSubmitRef.current = opts.onSubmit

  useEffect(() => {
    if (opts.enabled === false) return
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key === 's' && onSaveRef.current) {
        e.preventDefault()
        onSaveRef.current()
      }
      if (mod && e.key === 'Enter' && onSubmitRef.current) {
        e.preventDefault()
        onSubmitRef.current()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [opts.enabled])
}
