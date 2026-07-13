import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  DiscoverSlackChannelsRequest,
  DiscoverSlackChannelsResponse,
} from '@/proto/attune/v1/inbound_source'

export type DiscoverSlackChannelsInput = DiscoverSlackChannelsRequest
export type DiscoverSlackChannelsResult = DiscoverSlackChannelsResponse

// useDiscoverSlackChannels resolves the Slack channel list the configured token
// can actually read. The Console create dialog uses it to let the operator pick
// a channel without hand-typing a raw ID.
export function useDiscoverSlackChannels() {
  return useMutation({
    mutationFn: (body: DiscoverSlackChannelsInput) =>
      api<DiscoverSlackChannelsResult>('/fb/v1/console/inbound/sources/slack/discover', {
        method: 'POST',
        body,
      }),
  })
}
