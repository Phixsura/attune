import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { type SavedViewFilters, useSavedViews } from '@/features/feedback/hooks/use-saved-views'

const emptyFilters: SavedViewFilters = {
  attrFilters: {},
  tagFilter: '',
  workflowFilter: '',
  enrichmentFilter: '',
  urgentOnly: false,
  queueMode: 'all',
  sortMode: 'newest',
  q: '',
}

afterEach(() => {
  localStorage.clear()
})

describe('useSavedViews', () => {
  it('starts with empty views', () => {
    const { result } = renderHook(() => useSavedViews())
    expect(result.current.views).toEqual([])
  })

  it('saves a view and returns it', () => {
    const { result } = renderHook(() => useSavedViews())
    act(() => {
      result.current.save('My View', { ...emptyFilters, urgentOnly: true })
    })
    expect(result.current.views).toHaveLength(1)
    expect(result.current.views[0].name).toBe('My View')
    expect(result.current.views[0].filters.urgentOnly).toBe(true)
  })

  it('removes a view by id', () => {
    const { result } = renderHook(() => useSavedViews())
    let id: string
    act(() => {
      const view = result.current.save('To Remove', emptyFilters)
      id = view.id
    })
    expect(result.current.views).toHaveLength(1)
    act(() => {
      result.current.remove(id)
    })
    expect(result.current.views).toEqual([])
  })

  it('persists views in localStorage', () => {
    const { result, unmount } = renderHook(() => useSavedViews())
    act(() => {
      result.current.save('Persistent', emptyFilters)
    })
    unmount()
    const raw = localStorage.getItem('attune:feedback:savedViews')
    expect(raw).toBeTruthy()
    const parsed = JSON.parse(raw!)
    expect(parsed).toHaveLength(1)
    expect(parsed[0].name).toBe('Persistent')
  })
})
