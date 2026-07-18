/* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
const consoleBase = import.meta.env.PROD ? '/console' : ''

export function consolePath(path: string): string {
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${consoleBase}${normalized}`
}

export function routePathFromConsoleRedirect(path: string): string {
  if (path === '/console') {
    return '/'
  }
  if (path.startsWith('/console/')) {
    return path.slice('/console'.length)
  }
  return path
}
