import { createRouter, RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { queryClient } from './routes/__root'
import { routeTree } from './routeTree.gen'
import './i18n'
import './styles/globals.css'

// Vite's `base` rewrites asset URLs but TanStack Router has its own
// basepath — without this, the router treats the FULL pathname (e.g.
// `/console/feedback`) as the route to match and returns "Not Found".
// Match vite.config.ts's NODE_ENV switch.
const basepath = import.meta.env.PROD ? '/console' : undefined

const router = createRouter({
  routeTree,
  basepath,
  context: { queryClient },
  // For Stage B the SPA owns the URL; nginx serves index.html for
  // anything under /console/*.
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const root = document.getElementById('root')
if (!root) throw new Error('root element missing')

createRoot(root).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>,
)
