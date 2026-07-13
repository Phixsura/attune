import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  TestInboundConnectionRequest,
  TestInboundConnectionResponse,
} from '@/proto/attune/v1/inbound_source'

// Feature-stable aliases.
export type TestInboundConnectionInput = TestInboundConnectionRequest
export type TestInboundConnectionResult = TestInboundConnectionResponse

// useTestInboundSourceConnection validates the selected inbound source
// connection without persisting it. Email probes IMAP creds; Slack probes
// auth.test and optionally the selected channel. The server returns 200 with
// ok=true|false for probe outcomes; malformed requests still use the shared
// {code,message,requestId} error envelope.
export function useTestInboundSourceConnection() {
  return useMutation({
    mutationFn: (body: TestInboundConnectionInput) =>
      api<TestInboundConnectionResult>('/fb/v1/console/inbound/sources/test-connection', {
        method: 'POST',
        body,
      }),
  })
}
