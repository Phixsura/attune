import { createHmac, timingSafeEqual } from 'node:crypto'

export const requiredConnectorHooks = [
  'install',
  'verifyWebhookSignature',
  'normalizeWebhook',
  'mapFields',
  'classifyError',
  'recover',
]

export function verifyWebhookSignature({ algorithm, header, headers, rawBody, secret }) {
  const received = String(headers[header] ?? '')
  const expected = `${algorithm}=${createHmac(algorithm, secret).update(rawBody).digest('hex')}`
  return constantTimeEqual(received, expected)
}

export function normalizeGitHubIssueWebhook(fixture) {
  const payload = parseRawBody(fixture.rawBody)
  const delivery = stringHeader(fixture.headers, 'x-github-delivery')
  const eventName = stringHeader(fixture.headers, 'x-github-event')
  const issue = payload.issue
  const repository = payload.repository

  assertRecord(issue, 'payload.issue')
  assertRecord(repository, 'payload.repository')

  const issueNumber = requiredNumber(issue.number, 'payload.issue.number')
  const repositoryName = requiredString(repository.full_name, 'payload.repository.full_name')
  const action = requiredString(payload.action, 'payload.action')

  return {
    actor: requiredString(payload.sender?.login ?? 'unknown', 'payload.sender.login'),
    body: stringValue(issue.body),
    dedupeKey: `github:${eventName}:${delivery}`,
    eventType: `${eventName}.${action}`,
    externalKey: `github:${repositoryName}#${issueNumber}`,
    labels: githubLabels(issue.labels),
    localObjectType: 'customer_request',
    provider: 'github',
    providerEventId: delivery,
    providerObjectType: 'issue',
    repository: repositoryName,
    sourceUpdatedAt: requiredString(issue.updated_at, 'payload.issue.updated_at'),
    state: requiredString(issue.state, 'payload.issue.state'),
    status: requiredString(issue.state, 'payload.issue.state'),
    title: requiredString(issue.title, 'payload.issue.title'),
    url: requiredString(issue.html_url, 'payload.issue.html_url'),
  }
}

export function mapFields(record, sampleMapping) {
  const mapped = {}
  for (const [target, sourcePath] of Object.entries(sampleMapping)) {
    mapped[target] = valueAtPath(record, sourcePath)
  }
  return mapped
}

export function classifyProviderError(error) {
  if (error.httpStatus === 429) return 'retry_after'
  if (error.httpStatus === 401 || error.httpStatus === 403) return 'reauthorize'
  if (error.httpStatus === 409) return 'manual_review'
  if (error.httpStatus === 422) return 'dead_letter'
  if (error.httpStatus >= 500) return 'retry'
  return 'manual_review'
}

function parseRawBody(rawBody) {
  try {
    const parsed = JSON.parse(rawBody)
    assertRecord(parsed, 'rawBody')
    return parsed
  } catch (error) {
    throw new Error(`fixture rawBody must be valid JSON: ${error.message}`)
  }
}

function githubLabels(labels) {
  if (!Array.isArray(labels)) return []
  return labels
    .map((label) => (typeof label?.name === 'string' ? label.name : ''))
    .filter((label) => label.length > 0)
}

function valueAtPath(record, path) {
  if (path === 'labels') return Array.isArray(record.labels) ? [...record.labels] : []
  return path.split('.').reduce((current, segment) => {
    if (current === undefined || current === null) return undefined
    if (typeof current !== 'object') return undefined
    return current[segment]
  }, record)
}

function constantTimeEqual(received, expected) {
  const receivedBytes = Buffer.from(received)
  const expectedBytes = Buffer.from(expected)
  if (receivedBytes.length !== expectedBytes.length) return false
  return timingSafeEqual(receivedBytes, expectedBytes)
}

function stringHeader(headers, key) {
  return requiredString(headers[key], `headers.${key}`)
}

function stringValue(value) {
  return typeof value === 'string' ? value : ''
}

function requiredString(value, label) {
  if (typeof value !== 'string' || value.length === 0) {
    throw new Error(`${label} must be a non-empty string`)
  }
  return value
}

function requiredNumber(value, label) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${label} must be a finite number`)
  }
  return value
}

function assertRecord(value, label) {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${label} must be an object`)
  }
}
