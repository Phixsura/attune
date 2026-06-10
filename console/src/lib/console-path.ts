const consoleBase = import.meta.env.PROD ? '/console' : ''

export function consolePath(path: string): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${consoleBase}${normalized}`
}

export function routePathFromConsoleRedirect(path: string): string {
  if (import.meta.env.PROD) {
    return path
  }
  if (path === '/console') {
    return '/'
  }
  if (path.startsWith('/console/')) {
    return path.slice('/console'.length)
  }
  return path
}
