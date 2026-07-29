'use strict'

module.exports = {
  key: 'create_feedback',
  noun: 'Feedback',
  display: {
    label: 'Create Feedback',
    description: 'Ingests a feedback item into attune (it will be enriched asynchronously).',
  },
  operation: {
    inputFields: [
      { key: 'content', label: 'Content', type: 'text', required: true },
      {
        key: 'source',
        label: 'Source',
        type: 'string',
        required: false,
        helpText: 'attune source token, e.g. `api` (default) or a configured inbound channel.',
      },
      { key: 'user_id', label: 'User ID', type: 'string', required: false },
      { key: 'language', label: 'Language', type: 'string', required: false },
    ],
    perform: (z, bundle) =>
      z
        .request({
          url: `${bundle.authData.base_url}/v1/feedback/ingest`,
          method: 'POST',
          body: {
            content: bundle.inputData.content,
            source: bundle.inputData.source || 'api',
            user_id: bundle.inputData.user_id || '',
            language: bundle.inputData.language || '',
          },
        })
        .then((response) => response.data),
    sample: { id: 12345, status: 'accepted' },
  },
}
