import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { useResponseTemplates } from './use-response-templates'

beforeEach(() => {
  localStorage.clear()
})

describe('useResponseTemplates', () => {
  it('starts with empty templates', () => {
    const { result } = renderHook(() => useResponseTemplates())
    expect(result.current.templates).toEqual([])
  })

  it('saves a template', () => {
    const { result } = renderHook(() => useResponseTemplates())
    act(() => {
      result.current.save('Thanks', 'Thank you for your feedback!')
    })
    expect(result.current.templates).toHaveLength(1)
    expect(result.current.templates[0].name).toBe('Thanks')
    expect(result.current.templates[0].content).toBe('Thank you for your feedback!')
  })

  it('updates a template', () => {
    const { result } = renderHook(() => useResponseTemplates())
    let id: string
    act(() => {
      const tpl = result.current.save('Original', 'content')
      id = tpl.id
    })
    act(() => {
      result.current.update(id, 'Updated', 'new content')
    })
    expect(result.current.templates[0].name).toBe('Updated')
    expect(result.current.templates[0].content).toBe('new content')
  })

  it('removes a template', () => {
    const { result } = renderHook(() => useResponseTemplates())
    let id: string
    act(() => {
      const tpl = result.current.save('ToDelete', 'bye')
      id = tpl.id
    })
    act(() => {
      result.current.remove(id)
    })
    expect(result.current.templates).toHaveLength(0)
  })

  it('persists across hook instances', () => {
    const { result } = renderHook(() => useResponseTemplates())
    act(() => {
      result.current.save('Persist', 'value')
    })
    const { result: result2 } = renderHook(() => useResponseTemplates())
    expect(result2.current.templates).toHaveLength(1)
    expect(result2.current.templates[0].name).toBe('Persist')
  })
})
