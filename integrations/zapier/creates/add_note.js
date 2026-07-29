'use strict'

module.exports = {
  key: 'add_note',
  noun: 'Note',
  display: {
    label: 'Add Note to Request',
    description:
      'Adds a note to a customer request — internal (team-only) or public (portal comment, moderated).',
  },
  operation: {
    inputFields: [
      { key: 'request_id', label: 'Request ID', type: 'string', required: true },
      { key: 'body', label: 'Note', type: 'text', required: true },
      {
        key: 'visibility',
        label: 'Visibility',
        type: 'string',
        required: false,
        default: 'internal',
        choices: { internal: 'Internal (team only)', public: 'Public (moderated portal comment)' },
      },
    ],
    perform: (z, bundle) =>
      z
        .request({
          url: `${bundle.authData.base_url}/v1/requests/${encodeURIComponent(bundle.inputData.request_id)}/notes`,
          method: 'POST',
          body: {
            body: bundle.inputData.body,
            visibility: bundle.inputData.visibility || 'internal',
          },
        })
        .then((response) => response.data),
    sample: {
      request: {
        id: '11111111-2222-3333-4444-555555555555',
        displayId: 'REQ-42',
        title: 'Add dark mode',
      },
    },
  },
}
