import { describe, expect, it } from 'vitest'
import {
  buildDeveloperApiAdoptionKit,
  type DeveloperApiAdoptionArtifacts,
  defaultDeveloperApiAdoptionArtifacts,
} from './developer-api-adoption-kit'

const activeKey = {
  id: 'key-1',
  keyPrefix: 'ak_live_dev',
  label: 'developer sandbox',
  isActive: true,
  createdAt: '2026-07-01T00:00:00Z',
  lastUsedAt: '2026-07-02T00:00:00Z',
  scopes: ['ingest:write'],
  allowedCidrs: ['203.0.113.0/24'],
  usageCount: '42',
  environment: 'production',
  serviceAccountId: 'sa-1',
}

const serviceAccount = {
  id: 'sa-1',
  name: 'developer-ci',
  description: 'SDK smoke runner',
  isActive: true,
  createdAt: '2026-07-01T00:00:00Z',
  updatedAt: '2026-07-02T00:00:00Z',
}

const scopes = [
  {
    scope: 'feedback:read',
    resource: 'feedback',
    action: 'read',
    description: 'Read feedback',
    implies: [],
  },
  {
    scope: 'ingest:write',
    resource: 'ingest',
    action: 'write',
    description: 'Submit feedback',
    implies: [],
  },
]

const scopePresets = [
  {
    id: 'full_access',
    name: 'Full access',
    description: 'All scopes',
    scopes: ['feedback:read', 'ingest:write'],
  },
  {
    id: 'ingest_only',
    name: 'Ingest only',
    description: 'Submit feedback',
    scopes: ['ingest:write'],
  },
]

describe('buildDeveloperApiAdoptionKit', () => {
  it('joins OpenAPI, Node SDK, Go SDK, examples, sandbox, and webhook replay evidence', () => {
    const kit = buildDeveloperApiAdoptionKit({
      apiKeys: [activeKey],
      scopePresets,
      scopes,
      serviceAccounts: [serviceAccount],
    })

    expect(kit.fingerprint).toBe(
      '2 scopes / 2 presets / 1 active keys / 1 service accounts / 14/14 artifacts verified',
    )
    expect(kit.summary).toBe('developer API adoption kit evidence is ready')
    expect(kit.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      ready: 5,
      total: 5,
      watch: 0,
    })
    expect(kit.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      ['openapi_contract', 'ready', '2 scopes / 2 presets / 1 active keys / 1 used'],
      [
        'node_sdk',
        'ready',
        '1 Node examples / e2e on / browser smoke on / 1 automation identities',
      ],
      ['go_sdk', 'ready', '1 Go examples / e2e on / 1 active keys'],
      [
        'example_sandbox',
        'ready',
        '1 active keys / 1 service accounts / 2 presets / demo bootstrap on',
      ],
      ['webhook_replay', 'ready', '4 replay fixtures / replay smoke on / browser ingest on'],
    ])
  })

  it('blocks adoption lanes when required developer assets are absent', () => {
    const missingArtifacts: DeveloperApiAdoptionArtifacts = {
      ...defaultDeveloperApiAdoptionArtifacts,
      demoBootstrap: false,
      goSdkModule: false,
      nodeSdkE2e: false,
      openApiContract: false,
      replayFixtureCount: 0,
    }

    const kit = buildDeveloperApiAdoptionKit({
      apiKeys: [activeKey],
      artifacts: missingArtifacts,
      scopePresets,
      scopes,
      serviceAccounts: [serviceAccount],
    })

    expect(kit.summary).toBe('5 developer adoption lanes are blocked')
    expect(kit.totals).toMatchObject({ blocked: 5, needs_data: 0, ready: 0, watch: 0 })
  })

  it('marks every lane as needing data before the page has loaded adoption inputs', () => {
    const kit = buildDeveloperApiAdoptionKit({})

    expect(kit.fingerprint).toBe(
      '0 scopes / 0 presets / 0 active keys / 0 service accounts / 14/14 artifacts verified',
    )
    expect(kit.summary).toBe('5 developer adoption lanes need evidence')
    expect(kit.totals).toMatchObject({ blocked: 0, needs_data: 5, ready: 0, watch: 0 })
  })

  it('keeps adoption lanes on watch when examples and active identities are not hardened', () => {
    const kit = buildDeveloperApiAdoptionKit({
      apiKeys: [],
      artifacts: {
        ...defaultDeveloperApiAdoptionArtifacts,
        browserIngestExample: false,
        goExample: false,
        nodeExample: false,
        openApiGuide: false,
      },
      scopePresets,
      scopes,
      serviceAccounts: [],
    })

    expect(kit.summary).toBe('5 developer adoption lanes need hardening')
    expect(kit.lanes.find((lane) => lane.key === 'openapi_contract')).toMatchObject({
      status: 'watch',
    })
    expect(kit.lanes.find((lane) => lane.key === 'node_sdk')).toMatchObject({
      signal: '0 Node examples / e2e on / browser smoke on / 0 automation identities',
      status: 'watch',
    })
    expect(kit.lanes.find((lane) => lane.key === 'go_sdk')).toMatchObject({
      signal: '0 Go examples / e2e on / 0 active keys',
      status: 'watch',
    })
  })
})
