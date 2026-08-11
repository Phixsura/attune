'use strict'

const { makeHookTrigger } = require('./hook_trigger')
const samples = require('../samples')

module.exports = makeHookTrigger({
  key: 'new_request',
  noun: 'Customer Request',
  label: 'New Customer Request',
  description: 'Triggers when a customer request is created.',
  eventType: 'request.created',
  sample: samples.requestCreated,
})
