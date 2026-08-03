import { describe, expect, it } from 'vitest'
import {
  buildDeveloperSdkParityGate,
  type DeveloperSdkParityArtifacts,
  defaultDeveloperSdkParityArtifacts,
} from '@/features/api-keys/developer-sdk-parity-gate'

const ingestOnlyKey = {
  allowedCidrs: [],
  createdAt: '2026-07-01T00:00:00Z',
  environment: 'production',
  id: 'key-ingest',
  isActive: true,
  keyPrefix: 'att_live_ingest',
  label: 'browser widget',
  scopes: ['ingest:write'],
  usageCount: '3',
}

const managementKey = {
  ...ingestOnlyKey,
  id: 'key-management',
  keyPrefix: 'att_live_management',
  label: 'ci management',
  scopes: ['ingest:write', 'feedback:read'],
}

describe('buildDeveloperSdkParityGate', () => {
  it('verifies the full Node/Go SDK parity gate', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey, managementKey],
    })

    expect(gate.fingerprint).toBe(
      '35/35 shared methods / verifier on / 1 browser-safe keys / 6/6 release gates',
    )
    expect(gate.summary).toBe('Node/Go SDK parity gate is verified')
    expect(gate.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 5,
      verified: 5,
      watch: 0,
    })
    expect(gate.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      ['management_surface', 'verified', '35 shared methods / Node 35 / Go 35 / drift 0'],
      [
        'error_contract',
        'verified',
        'ErrorResponse + ErrorCode / AttuneError + TransportErrorCode / envelope available',
      ],
      [
        'retry_idempotency',
        'verified',
        '408/429/5xx / Retry-After / idempotency available / API version pinned on',
      ],
      [
        'browser_boundary',
        'verified',
        '1 browser-safe keys / 1 management scoped keys / browser smoke on',
      ],
      [
        'release_artifacts',
        'verified',
        'npm ESM+CJS+types / Go module / live e2e 2/2 / packed smoke on',
      ],
    ])
  })

  it('blocks when shared public management methods drift', () => {
    const artifacts: DeveloperSdkParityArtifacts = {
      ...defaultDeveloperSdkParityArtifacts,
      goClientMethods: 34,
      sharedClientMethods: 34,
    }

    const gate = buildDeveloperSdkParityGate({ apiKeys: [ingestOnlyKey], artifacts })

    expect(gate.summary).toBe('1 SDK parity lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'management_surface')).toMatchObject({
      signal: '34 shared methods / Node 35 / Go 34 / drift 2',
      status: 'blocked',
    })
  })

  it('asks for browser-key evidence when runtime API key evidence has not loaded', () => {
    const gate = buildDeveloperSdkParityGate({})

    expect(gate.summary).toBe('1 SDK parity lanes need browser key evidence')
    expect(gate.lanes.find((lane) => lane.key === 'browser_boundary')).toMatchObject({
      signal: 'browser key evidence missing',
      status: 'needs_data',
    })
  })

  it('keeps the browser boundary on watch until an ingest-only active key exists', () => {
    const gate = buildDeveloperSdkParityGate({ apiKeys: [managementKey] })

    expect(gate.summary).toBe('1 SDK parity lanes need hardening')
    expect(gate.lanes.find((lane) => lane.key === 'browser_boundary')).toMatchObject({
      signal: '0 browser-safe keys / 1 management scoped keys / browser smoke on',
      status: 'watch',
    })
  })

  it('blocks management parity when the verifier is missing', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        sdkParityVerifier: false,
      },
    })

    expect(gate.summary).toBe('1 SDK parity lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'management_surface')).toMatchObject({
      evidence: '35 expected shared methods / Node 35 / Go 35 / verifier missing',
      status: 'blocked',
    })
  })

  it('blocks missing error envelopes and watches missing transport categories', () => {
    const blocked = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        errorContractExports: false,
      },
    })
    const watched = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        transportErrors: false,
      },
    })

    expect(blocked.summary).toBe('1 SDK parity lanes are blocked')
    expect(blocked.lanes.find((lane) => lane.key === 'error_contract')).toMatchObject({
      status: 'blocked',
    })
    expect(watched.summary).toBe('1 SDK parity lanes need hardening')
    expect(watched.lanes.find((lane) => lane.key === 'error_contract')).toMatchObject({
      status: 'watch',
    })
  })

  it('blocks missing retry exports and watches unpinned API versions', () => {
    const blocked = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        idempotencyCoverage: false,
      },
    })
    const watched = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        apiVersionPinned: false,
      },
    })

    expect(blocked.summary).toBe('1 SDK parity lanes are blocked')
    expect(blocked.lanes.find((lane) => lane.key === 'retry_idempotency')).toMatchObject({
      status: 'blocked',
    })
    expect(watched.summary).toBe('1 SDK parity lanes need hardening')
    expect(watched.lanes.find((lane) => lane.key === 'retry_idempotency')).toMatchObject({
      signal: '408/429/5xx / Retry-After / idempotency available / API version pinned off',
      status: 'watch',
    })
  })

  it('blocks browser examples when the browser smoke is absent', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        nodeBrowserSmoke: false,
      },
    })

    expect(gate.summary).toBe('1 SDK parity lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'browser_boundary')).toMatchObject({
      signal: '1 browser-safe keys / 0 management scoped keys / browser smoke off',
      status: 'blocked',
    })
  })

  it('watches browser examples until server-only management warnings ship', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        serverOnlyManagementWarning: false,
      },
    })

    expect(gate.summary).toBe('1 SDK parity lanes need hardening')
    expect(gate.lanes.find((lane) => lane.key === 'browser_boundary')).toMatchObject({
      evidence:
        'browser smoke available / example available / warning missing / 1 ingest-only active keys',
      status: 'watch',
    })
  })

  it('blocks release artifacts when package metadata or Go module support is absent', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        nodePackedTypes: false,
      },
    })

    expect(gate.summary).toBe('1 SDK parity lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'release_artifacts')).toMatchObject({
      evidence:
        'npm exports available / d.ts missing / Go module available / packed smoke available',
      status: 'blocked',
    })
  })

  it('watches release artifacts until live e2e and packed install smoke pass', () => {
    const gate = buildDeveloperSdkParityGate({
      apiKeys: [ingestOnlyKey],
      artifacts: {
        ...defaultDeveloperSdkParityArtifacts,
        goLiveE2e: false,
        packedArtifactInstallSmoke: false,
      },
    })

    expect(gate.fingerprint).toBe(
      '35/35 shared methods / verifier on / 1 browser-safe keys / 4/6 release gates',
    )
    expect(gate.summary).toBe('1 SDK parity lanes need hardening')
    expect(gate.lanes.find((lane) => lane.key === 'release_artifacts')).toMatchObject({
      signal: 'npm ESM+CJS+types / Go module / live e2e 1/2 / packed smoke off',
      status: 'watch',
    })
  })
})
