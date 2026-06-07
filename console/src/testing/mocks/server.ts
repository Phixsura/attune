import { setupServer } from 'msw/node'
import { handlers } from '@/testing/mocks/handlers'

// MSW v2 node setup. Lifecycle is in setup-tests.ts:
//   beforeAll(server.listen({ onUnhandledRequest: 'error' }))
//   afterEach(server.resetHandlers())
//   afterAll(server.close())
// Per-test override: server.use(http.get(...))
export const server = setupServer(...handlers)
