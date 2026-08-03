import { describe, expect, it } from 'vitest'
import {
  buildDeveloperApiConsistencyContract,
  type DeveloperApiConsistencyArtifacts,
  defaultDeveloperApiConsistencyArtifacts,
} from '@/features/api-keys/developer-api-consistency-contract'

describe('buildDeveloperApiConsistencyContract', () => {
  it('verifies the full developer API consistency contract', () => {
    const contract = buildDeveloperApiConsistencyContract()

    expect(contract.fingerprint).toBe(
      '3/3 public pagination surfaces / 3/3 console mirrors / 3/3 filters / 3/3 sort enums / verifier on',
    )
    expect(contract.summary).toBe('developer API consistency contract is verified')
    expect(contract.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 6,
      verified: 6,
      watch: 0,
    })
    expect(contract.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      [
        'pagination_contract',
        'verified',
        '2 cursor surfaces / 1 before_id surface / nextCursor + nextBeforeId',
      ],
      [
        'filter_contract',
        'verified',
        'audit actions + actor/target/time / request_type / status[]',
      ],
      [
        'sort_contract',
        'verified',
        'CustomerRequestSort + SortDirection / decision score / delivery health',
      ],
      [
        'error_envelope',
        'verified',
        'ErrorResponse code/message/requestId across OpenAPI, Node, and Go',
      ],
      ['idempotency_contract', 'verified', 'Idempotency-Key / management POST coverage 2/2'],
      [
        'sdk_wire_semantics',
        'verified',
        'actions[]=repeat / request_type / before_id / positive limit validators',
      ],
    ])
  })

  it('blocks when paginated surfaces drift across OpenAPI or SDK pagers', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      apiConsistencyVerifier: false,
      goSdkPagers: 2,
      publicPaginationSurfaces: 2,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('2 API consistency lanes are blocked')
    expect(contract.lanes.find((lane) => lane.key === 'pagination_contract')).toMatchObject({
      status: 'blocked',
    })
    expect(contract.lanes.find((lane) => lane.key === 'sdk_wire_semantics')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks pagination when console mirrors or SDK pagers fall short even with verifier enabled', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      consolePaginationSurfaces: 2,
      goSdkPagers: 2,
      nodeSdkPagers: 2,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes are blocked')
    expect(contract.lanes.find((lane) => lane.key === 'pagination_contract')).toMatchObject({
      evidence: 'public 3/3 / console 2/3 / Node pagers 2/3 / Go pagers 2/3',
      status: 'blocked',
    })
  })

  it('blocks missing filter surfaces and watches SDK validation gaps', () => {
    const blocked = buildDeveloperApiConsistencyContract({
      artifacts: {
        ...defaultDeveloperApiConsistencyArtifacts,
        filterSurfaces: 2,
      },
    })
    expect(blocked.lanes.find((lane) => lane.key === 'filter_contract')).toMatchObject({
      status: 'blocked',
    })

    const watched = buildDeveloperApiConsistencyContract({
      artifacts: {
        ...defaultDeveloperApiConsistencyArtifacts,
        goQueryValidation: false,
        nodeQueryValidation: false,
      },
    })
    expect(watched.summary).toBe('1 API consistency lanes need hardening')
    expect(watched.lanes.find((lane) => lane.key === 'filter_contract')).toMatchObject({
      evidence: 'filters 3/3 / Node validation missing / Go validation missing',
      status: 'watch',
    })
  })

  it('watches generated SDK sort enum drift after OpenAPI remains covered', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      sdkSortEnum: false,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes need hardening')
    expect(contract.lanes.find((lane) => lane.key === 'sort_contract')).toMatchObject({
      evidence: 'sort surfaces 3/3 / OpenAPI enum available / generated SDK enum missing',
      status: 'watch',
    })
  })

  it('blocks missing OpenAPI error envelope and watches SDK envelope gaps', () => {
    const blocked = buildDeveloperApiConsistencyContract({
      artifacts: {
        ...defaultDeveloperApiConsistencyArtifacts,
        openApiErrorEnvelope: false,
      },
    })
    expect(blocked.lanes.find((lane) => lane.key === 'error_envelope')).toMatchObject({
      status: 'blocked',
    })

    const watched = buildDeveloperApiConsistencyContract({
      artifacts: {
        ...defaultDeveloperApiConsistencyArtifacts,
        goErrorEnvelope: false,
        nodeErrorEnvelope: false,
      },
    })
    expect(watched.summary).toBe('1 API consistency lanes need hardening')
    expect(watched.lanes.find((lane) => lane.key === 'error_envelope')).toMatchObject({
      evidence: 'OpenAPI available / Node missing / Go missing',
      status: 'watch',
    })
  })

  it('keeps idempotency on watch until both SDK coverage paths are present', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      goIdempotencyCoverage: false,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes need hardening')
    expect(contract.lanes.find((lane) => lane.key === 'idempotency_contract')).toMatchObject({
      signal: 'Idempotency-Key / management POST coverage 1/2',
      status: 'watch',
    })
  })

  it('blocks idempotency when the OpenAPI header is missing', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      idempotencyHeaderOpenApi: false,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes are blocked')
    expect(contract.lanes.find((lane) => lane.key === 'idempotency_contract')).toMatchObject({
      evidence: 'OpenAPI header missing / Node coverage available / Go coverage available',
      status: 'blocked',
    })
  })

  it('blocks when sort enums fall out of the generated contract', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      openApiSortEnum: false,
      sortSurfaces: 2,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes are blocked')
    expect(contract.lanes.find((lane) => lane.key === 'sort_contract')).toMatchObject({
      evidence: 'sort surfaces 2/3 / OpenAPI enum missing / generated SDK enum available',
      status: 'blocked',
    })
  })

  it('blocks SDK wire semantics when either generated SDK misses query fixtures', () => {
    const artifacts: DeveloperApiConsistencyArtifacts = {
      ...defaultDeveloperApiConsistencyArtifacts,
      goWireQueryCoverage: false,
      nodeWireQueryCoverage: false,
    }

    const contract = buildDeveloperApiConsistencyContract({ artifacts })

    expect(contract.summary).toBe('1 API consistency lanes are blocked')
    expect(contract.lanes.find((lane) => lane.key === 'sdk_wire_semantics')).toMatchObject({
      evidence: 'Node wire tests missing / Go wire tests missing / verifier available',
      status: 'blocked',
    })
  })
})
