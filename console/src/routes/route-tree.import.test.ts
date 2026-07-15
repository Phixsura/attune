import { describe, expect, it } from 'vitest'
import { routeTree } from '@/routeTree.gen'

const routeModules = import.meta.glob(['./*.tsx', '!./*.test.tsx'], { eager: true })

describe('routeTree', () => {
  it('imports the production route table without throwing', () => {
    expect(routeTree).toBeDefined()
    expect(Object.keys(routeModules)).not.toHaveLength(0)
  })
})
