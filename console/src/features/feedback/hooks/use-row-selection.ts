import { useCallback, useEffect, useMemo, useState } from 'react'

export function useRowSelection(itemIds: string[]) {
  const [selected, setSelected] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    setSelected((prev) => {
      const valid = new Set(itemIds)
      const next = new Set<string>()
      for (const id of prev) {
        if (valid.has(id)) next.add(id)
      }
      return next.size === prev.size ? prev : next
    })
  }, [itemIds])

  const toggle = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const toggleAll = useCallback(() => {
    setSelected((prev) => (prev.size === itemIds.length ? new Set() : new Set(itemIds)))
  }, [itemIds])

  const clear = useCallback(() => setSelected(new Set()), [])
  const selectOnly = useCallback(
    (ids: string[]) => {
      const valid = new Set(itemIds)
      setSelected(new Set(ids.filter((id) => valid.has(id))))
    },
    [itemIds],
  )

  const isAllSelected = useMemo(
    () => itemIds.length > 0 && selected.size === itemIds.length,
    [itemIds, selected],
  )

  return { selected, toggle, toggleAll, clear, selectOnly, isAllSelected }
}
