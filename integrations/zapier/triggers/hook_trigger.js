'use strict'

// Shared REST-hook trigger factory. All four attune triggers are identical
// in mechanics and differ only in event type, labels, and how the envelope
// maps to a flat Zapier item:
//   subscribe   POST /v1/hooks   {target_url, event_types:[event], consumer: "zapier"}
//   unsubscribe DELETE /v1/hooks/{bundle.subscribeData.id}
//   perform     reshape the delivered envelope (bundle.cleanedRequest)
//   performList GET /v1/hooks/samples/{event} — schema-identical fallback

const subscribeHook = (eventType) => (z, bundle) =>
  z
    .request({
      url: `${bundle.authData.base_url}/v1/hooks`,
      method: 'POST',
      body: {
        target_url: bundle.targetUrl,
        event_types: [eventType],
        consumer: 'zapier',
      },
    })
    .then((response) => response.data)

const unsubscribeHook = (z, bundle) =>
  z
    .request({
      url: `${bundle.authData.base_url}/v1/hooks/${bundle.subscribeData.id}`,
      method: 'DELETE',
    })
    .then((response) => response.data)

// Zapier dedups on `id`. The delivery id header is not part of the body, so
// the item id is derived from the entity id + event type — stable across
// at-least-once redeliveries of the same event, distinct across event types.
const itemFromEnvelope = (envelope) => {
  const entity = envelope.feedback || envelope.request || {}
  return {
    id: `${entity.id}-${envelope.event_type}`,
    ...envelope,
  }
}

const perform = (z, bundle) => [itemFromEnvelope(bundle.cleanedRequest)]

const performList = (eventType) => (z, bundle) =>
  z
    .request({
      url: `${bundle.authData.base_url}/v1/hooks/samples/${eventType}`,
      method: 'GET',
    })
    .then((response) => (response.data.samples || []).map(itemFromEnvelope))

const makeHookTrigger = ({ key, noun, label, description, eventType, sample }) => ({
  key,
  noun,
  display: { label, description },
  operation: {
    type: 'hook',
    performSubscribe: subscribeHook(eventType),
    performUnsubscribe: unsubscribeHook,
    perform,
    performList: performList(eventType),
    sample,
  },
})

module.exports = { makeHookTrigger, itemFromEnvelope }
