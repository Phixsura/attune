'use strict'

const { makeHookTrigger } = require('./hook_trigger')
const samples = require('../samples')

module.exports = makeHookTrigger({
  key: 'new_feedback',
  noun: 'Feedback',
  label: 'New Feedback',
  description: 'Triggers when a feedback item is ingested and enriched.',
  eventType: 'feedback.created',
  sample: samples.feedbackCreated,
})
