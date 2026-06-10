import { describe, expect, it } from 'vitest'
import { consolePath, routePathFromConsoleRedirect } from './console-path'

describe('console paths', () => {
  it('uses dev-local routes when Vite serves without the /console basepath', () => {
    expect(consolePath('/')).toBe('/')
    expect(consolePath('/login')).toBe('/login')
    expect(routePathFromConsoleRedirect('/console')).toBe('/')
    expect(routePathFromConsoleRedirect('/console/feedback')).toBe('/feedback')
    expect(routePathFromConsoleRedirect('/feedback')).toBe('/feedback')
  })
})
