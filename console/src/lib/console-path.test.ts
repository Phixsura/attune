import { describe, expect, it } from 'vitest'
import { consolePath, routePathFromConsoleRedirect } from './console-path'

describe('console paths', () => {
  it('normalizes server redirects into router-local paths', () => {
    expect(consolePath('/')).toBe('/')
    expect(consolePath('/login')).toBe('/login')
    expect(routePathFromConsoleRedirect('/console')).toBe('/')
    expect(routePathFromConsoleRedirect('/console/feedback')).toBe('/feedback')
    expect(routePathFromConsoleRedirect('/console/settings')).toBe('/settings')
    expect(routePathFromConsoleRedirect('/feedback')).toBe('/feedback')
  })
})
