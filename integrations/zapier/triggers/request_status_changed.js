'use strict'

const { makeHookTrigger } = require('./hook_trigger')
const samples = require('../samples')

module.exports = makeHookTrigger({
  key: 'request_status_changed',
  noun: 'Customer Request',
  label: 'Request Status Changed',
  description: "Triggers when a customer request's status changes (e.g. planned to shipped).",
  eventType: 'request.status_changed',
  sample: samples.requestStatusChanged,
})
