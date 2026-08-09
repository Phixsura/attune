'use strict'

const { version: platformVersion } = require('zapier-platform-core')
const { version } = require('./package.json')

const { authentication, beforeRequest } = require('./authentication')
const newFeedback = require('./triggers/new_feedback')
const urgentFeedback = require('./triggers/urgent_feedback')
const newRequest = require('./triggers/new_request')
const requestStatusChanged = require('./triggers/request_status_changed')
const createFeedback = require('./creates/create_feedback')
const updateRequest = require('./creates/update_request')
const addTag = require('./creates/add_tag')
const addNote = require('./creates/add_note')

module.exports = {
  version,
  platformVersion,
  authentication,
  beforeRequest: [beforeRequest],
  triggers: {
    [newFeedback.key]: newFeedback,
    [urgentFeedback.key]: urgentFeedback,
    [newRequest.key]: newRequest,
    [requestStatusChanged.key]: requestStatusChanged,
  },
  creates: {
    [createFeedback.key]: createFeedback,
    [updateRequest.key]: updateRequest,
    [addTag.key]: addTag,
    [addNote.key]: addNote,
  },
}
