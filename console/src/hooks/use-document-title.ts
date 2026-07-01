import { useEffect } from 'react'

const appTitle = 'Attune Console'

export function consoleDocumentTitle(pageTitle: string) {
  return pageTitle ? `${pageTitle} - ${appTitle}` : appTitle
}

export function useDocumentTitle(pageTitle: string) {
  useEffect(() => {
    const nextTitle = consoleDocumentTitle(pageTitle)
    document.title = nextTitle

    return () => {
      if (document.title === nextTitle) {
        document.title = appTitle
      }
    }
  }, [pageTitle])
}
