'use strict'

// Static sample data (Zapier publishing check D012). MUST stay
// schema-identical to the live WIRE envelopes (outbound.Envelope as the
// raw-webhook adapter serializes it) — these mirror the server's
// samples_static.go fixtures, plus the flat `id` the trigger perform adds.

const feedbackSample = (eventType, urgent) => ({
  id: `12345-${eventType}-2026-07-01T08:09:15Z`,
  version: '2',
  event_type: eventType,
  timestamp: '2026-07-01T08:09:15Z',
  tenant_id: 'sample-tenant',
  feedback: {
    id: 12345,
    tenant_id: 'sample-tenant',
    content: 'The export button does nothing when I click it.',
    source: 'api',
    source_display: 'API client',
    user_id: 'user-789',
    language: 'en',
    submitted_at: '2026-07-01T08:09:10Z',
    enriched: {
      title: 'Export button unresponsive',
      attrs: { type: 'bug', severity: 'P1' },
      is_urgent: urgent,
      rationale: 'User reports a broken core workflow.',
      enriched_at: '2026-07-01T08:09:15Z',
    },
  },
})

const requestSample = (eventType, extra) => ({
  id: `11111111-2222-3333-4444-555555555555-${eventType}-2026-07-02T09:30:00Z`,
  version: '2',
  event_type: eventType,
  timestamp: '2026-07-02T09:30:00Z',
  tenant_id: 'sample-tenant',
  request: {
    id: '11111111-2222-3333-4444-555555555555',
    display_id: 'REQ-42',
    title: 'Add dark mode',
    description: 'Several customers asked for a dark theme.',
    status: 'in_progress',
    priority: 'high',
    created_at: '2026-07-01T10:00:00Z',
    updated_at: '2026-07-02T09:30:00Z',
    ...extra,
  },
})

module.exports = {
  feedbackCreated: feedbackSample('feedback.created', false),
  feedbackUrgent: feedbackSample('feedback.urgent', true),
  requestCreated: requestSample('request.created', { status: 'open' }),
  requestStatusChanged: requestSample('request.status_changed', {
    previous_status: 'planned',
  }),
}
