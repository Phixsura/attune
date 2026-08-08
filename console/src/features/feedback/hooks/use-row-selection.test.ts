import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useRowSelection } from './use-row-selection'

describe('useRowSelection', () => {
  const ids = ['a', 'b', 'c']

  it('starts with empty selection', () => {
    const { result } = renderHook(() => useRowSelection(ids))
    expect(result.current.selected.size).toBe(0)
    expect(result.current.isAllSelected).toBe(false)
  })

  it('toggle adds and removes an item', () => {
    const { result } = renderHook(() => useRowSelection(ids))
    act(() => result.current.toggle('b'))
    expect(result.current.selected.has('b')).toBe(true)
    act(() => result.current.toggle('b'))
    expect(result.current.selected.has('b')).toBe(false)
  })

  it('toggleAll selects all, then deselects all', () => {
    const { result } = renderHook(() => useRowSelection(ids))
    act(() => result.current.toggleAll())
    expect(result.current.selected.size).toBe(3)
    expect(result.current.isAllSelected).toBe(true)
    act(() => result.current.toggleAll())
    expect(result.current.selected.size).toBe(0)
  })

  it('clear resets selection', () => {
    const { result } = renderHook(() => useRowSelection(ids))
    act(() => result.current.toggle('a'))
    act(() => result.current.toggle('c'))
    act(() => result.current.clear())
    expect(result.current.selected.size).toBe(0)
  })

  it('selectOnly replaces selection and ignores stale IDs', () => {
    const { result } = renderHook(() => useRowSelection(ids))
    act(() => result.current.toggle('a'))
    act(() => result.current.selectOnly(['b', 'missing', 'c']))
    expect(Array.from(result.current.selected).sort()).toEqual(['b', 'c'])
  })

  it('removes stale IDs when itemIds changes', () => {
    const { result, rerender } = renderHook(({ items }) => useRowSelection(items), {
      initialProps: { items: ['a', 'b', 'c'] },
    })
    act(() => result.current.toggle('b'))
    act(() => result.current.toggle('c'))
    expect(result.current.selected.size).toBe(2)

    rerender({ items: ['a', 'b'] })
    expect(result.current.selected.has('c')).toBe(false)
    expect(result.current.selected.has('b')).toBe(true)
    expect(result.current.selected.size).toBe(1)
  })

  it('isAllSelected is false for empty itemIds', () => {
    const { result } = renderHook(() => useRowSelection([]))
    expect(result.current.isAllSelected).toBe(false)
  })
})
