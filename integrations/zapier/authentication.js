'use strict'

// Custom (API key) auth. attune is self-hostable OSS, so the base URL is a
// connection field alongside the key. The test call is GET /v1/auth/verify;
// the connection label uses the workspace name + key label it returns.

const test = (z, bundle) =>
  z
    .request({
      url: `${bundle.authData.base_url}/v1/auth/verify`,
      method: 'GET',
    })
    .then((response) => response.data)

const authentication = {
  type: 'custom',
  fields: [
    {
      key: 'base_url',
      label: 'attune Base URL',
      required: true,
      type: 'string',
      helpText:
        'Your attune server URL, e.g. `https://attune.example.com`. No trailing slash.',
    },
    {
      key: 'api_key',
      label: 'API Key',
      required: true,
      type: 'password',
      helpText:
        'Create one in attune Console → Settings → API Keys with the `hooks:manage`, ' +
        '`ingest:write`, `requests:read`, `requests:write`, and `tags:write` scopes.',
    },
  ],
  test,
  connectionLabel: (z, bundle) => {
    // bundle.inputData carries the auth-test response. The API serializes
    // protojson camelCase (tenantDisplayName), not proto field names.
    const data = bundle.inputData || {}
    const workspace = data.tenantDisplayName || ''
    const label = data.label || ''
    return [workspace, label].filter(Boolean).join(' — ') || 'attune'
  },
}

// Inject the API key header on every request; strip trailing slash from the
// configured base URL so path joins stay canonical.
const beforeRequest = (request, z, bundle) => {
  if (bundle.authData && bundle.authData.api_key) {
    request.headers = request.headers || {}
    request.headers['X-API-Key'] = bundle.authData.api_key
  }
  if (bundle.authData && bundle.authData.base_url) {
    bundle.authData.base_url = bundle.authData.base_url.replace(/\/+$/, '')
  }
  return request
}

module.exports = { authentication, beforeRequest }
