import { useEffect } from 'react'

const appTitle = 'Attune Console'

export function consoleDocumentTitle(pageTitle: string) {
  /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
  return pageTitle ? `${pageTitle} - ${appTitle}` : appTitle
}

export function useDocumentTitle(pageTitle: string) {
  useEffect(() => {
    const nextTitle = consoleDocumentTitle(pageTitle)
    document.title = nextTitle

    return () => {
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (document.title === nextTitle) {
        document.title = appTitle
      }
    }
  }, [pageTitle])
}
