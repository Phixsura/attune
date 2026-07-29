'use strict'

module.exports = {
  key: 'add_tag',
  noun: 'Tag',
  display: {
    label: 'Add Tag to Feedback',
    description: 'Assigns a tag (by name) to a feedback item.',
  },
  operation: {
    inputFields: [
      { key: 'feedback_id', label: 'Feedback ID', type: 'integer', required: true },
      {
        key: 'tag_name',
        label: 'Tag Name',
        type: 'string',
        required: true,
        helpText: 'The tag must already exist in attune (Console → Tags).',
      },
    ],
    perform: (z, bundle) =>
      z
        .request({
          url: `${bundle.authData.base_url}/v1/feedback/${encodeURIComponent(bundle.inputData.feedback_id)}/tags`,
          method: 'POST',
          body: { tag_name: bundle.inputData.tag_name },
        })
        .then((response) => response.data),
    sample: { tag: { id: 'aaaaaaaa-1111-2222-3333-444444444444', name: 'bug', color: '#ef4444' } },
  },
}
