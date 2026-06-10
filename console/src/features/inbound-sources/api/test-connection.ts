import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  TestInboundConnectionRequest,
  TestInboundConnectionResponse,
} from '@/proto/attune/v1/inbound_source'

// Feature-stable aliases.
export type TestInboundConnectionInput = TestInboundConnectionRequest
export type TestInboundConnectionResult = TestInboundConnectionResponse

// useTestInboundSourceConnection — currently email-only. Validates the
// IMAP creds (dial + login + select + logout) without persisting. The
// server returns 200 with ok=true|false for connection probe outcomes;
// malformed requests still use the shared {code,message,requestId}
// error envelope.
export function useTestInboundSourceConnection() {
  return useMutation({
    mutationFn: (body: TestInboundConnectionInput) =>
      api<TestInboundConnectionResult>('/fb/v1/console/inbound/sources/test-connection', {
        method: 'POST',
        body,
      }),
  })
}
