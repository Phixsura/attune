'use strict'

module.exports = {
  key: 'update_request',
  noun: 'Customer Request',
  display: {
    label: 'Update Request Workflow',
    description: "Updates a customer request's status, title, description, or priority.",
  },
  operation: {
    inputFields: [
      { key: 'request_id', label: 'Request ID', type: 'string', required: true },
      {
        key: 'status',
        label: 'Status',
        type: 'string',
        required: false,
        choices: {
          CUSTOMER_REQUEST_STATUS_OPEN: 'Open',
          CUSTOMER_REQUEST_STATUS_PLANNED: 'Planned',
          CUSTOMER_REQUEST_STATUS_IN_PROGRESS: 'In progress',
          CUSTOMER_REQUEST_STATUS_SHIPPED: 'Shipped',
          CUSTOMER_REQUEST_STATUS_CANCELLED: 'Cancelled',
        },
      },
      { key: 'title', label: 'Title', type: 'string', required: false },
      { key: 'description', label: 'Description', type: 'text', required: false },
    ],
    perform: (z, bundle) => {
      const body = {}
      if (bundle.inputData.status) body.status = bundle.inputData.status
      if (bundle.inputData.title) body.title = bundle.inputData.title
      if (bundle.inputData.description) body.description = bundle.inputData.description
      return z
        .request({
          url: `${bundle.authData.base_url}/v1/requests/${encodeURIComponent(bundle.inputData.request_id)}`,
          method: 'PATCH',
          body,
        })
        .then((response) => response.data)
    },
    sample: {
      request: {
        id: '11111111-2222-3333-4444-555555555555',
        displayId: 'REQ-42',
        title: 'Add dark mode',
        status: 'CUSTOMER_REQUEST_STATUS_IN_PROGRESS',
      },
    },
  },
}
