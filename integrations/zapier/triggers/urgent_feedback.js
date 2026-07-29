'use strict'

const { makeHookTrigger } = require('./hook_trigger')
const samples = require('../samples')

module.exports = makeHookTrigger({
  key: 'urgent_feedback',
  noun: 'Feedback',
  label: 'New Urgent Feedback',
  description: 'Triggers when an enriched feedback item is classified urgent.',
  eventType: 'feedback.urgent',
  sample: samples.feedbackUrgent,
})
