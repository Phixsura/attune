import { describe, expect, it } from 'vitest'
import type { ExternalConnection } from '@/features/external-sync/api/external-sync'
import {
  buildIntegrationCatalog,
  defaultIntegrationCatalogArtifacts,
  type IntegrationCatalogArtifacts,
} from '@/features/external-sync/integration-catalog'

describe('buildIntegrationCatalog', () => {
  it('verifies marketplace catalog cards, install states, permissions, replay, and upgrades', () => {
    const catalog = buildIntegrationCatalog({ connections: [externalConnection()] })

    expect(catalog.fingerprint).toBe(
      '8/8 connectors / 8 install states / 8 permission maps / 8 sample replays / 8 upgrade paths / verifier on',
    )
    expect(catalog.summary).toBe('integration catalog is verified')
    expect(catalog.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 6,
      verified: 6,
      watch: 0,
    })
    expect(catalog.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      [
        'catalog_cards',
        'verified',
        '8 catalog cards / Jira, GitHub, Intercom, Zendesk, Salesforce, HubSpot, Custom webhook, CSV',
      ],
      ['install_status', 'verified', '1 live installs / 8 catalog states / 0 setup blockers'],
      ['permission_scope', 'verified', '8 permission maps / 23 scopes / least privilege on'],
      ['health_badge', 'verified', '8 health badges / 0 unhealthy tenant connectors'],
      ['sample_replay', 'verified', '8 replay fixtures / 8 normalized samples'],
      ['upgrade_path', 'verified', '8 upgrade paths / rollback 8/8'],
    ])
    expect(catalog.connectors.find((connector) => connector.id === 'github')).toMatchObject({
      liveConnectionName: 'GitHub Prod',
      runtimeHealthBadge: 'healthy',
      runtimeInstallStatus: 'installed',
    })
  })

  it('blocks when a required catalog card is missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.filter(
        (connector) => connector.id !== 'zendesk',
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'catalog_cards')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks permission scope clarity when a connector has no scopes', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github' ? { ...connector, scopes: [] } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'permission_scope')).toMatchObject({
      signal: '7 permission maps / 19 scopes / least privilege incomplete',
      status: 'blocked',
    })
  })

  it('keeps health badges on watch when tenant connectors are unhealthy', () => {
    const catalog = buildIntegrationCatalog({
      connections: [externalConnection(), externalConnection({ status: 'quarantined' })],
    })

    expect(catalog.summary).toBe('1 integration catalog lanes need hardening')
    expect(catalog.lanes.find((lane) => lane.key === 'health_badge')).toMatchObject({
      signal: '8 health badges / 1 unhealthy tenant connector',
      status: 'watch',
    })
  })

  it('decorates live unhealthy connectors with degraded runtime badges', () => {
    const catalog = buildIntegrationCatalog({
      connections: [externalConnection({ lastTestStatus: 'failed', status: 'active' })],
    })

    expect(catalog.summary).toBe('1 integration catalog lanes need hardening')
    expect(catalog.connectors.find((connector) => connector.id === 'github')).toMatchObject({
      runtimeHealthBadge: 'degraded',
      runtimeInstallStatus: 'installed',
    })
  })

  it('blocks sample replay evidence when fixture coverage is missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'intercom' ? { ...connector, replayFixture: '' } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'sample_replay')).toMatchObject({
      evidence: '7/8 fixture paths / 8/8 normalized samples',
      status: 'blocked',
    })
  })

  it('blocks upgrade readiness when rollback evidence is missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'salesforce' ? { ...connector, upgradeRollback: false } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'upgrade_path')).toMatchObject({
      signal: '8 upgrade paths / rollback 7/8',
      status: 'blocked',
    })
  })

  it('blocks catalog cards when verifier or required display names are missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      catalogVerifier: false,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github' ? { ...connector, displayName: '' } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.fingerprint).toContain('verifier off')
    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'catalog_cards')).toMatchObject({
      evidence: 'manifest verifier missing / 8/8 required connectors / 5 categories',
      status: 'blocked',
    })
  })

  it('blocks catalog cards when a required display name is empty', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github' ? { ...connector, displayName: '' } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'catalog_cards')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks install states when cards lack setup checks or install metadata', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github'
          ? { ...connector, installStatus: '' as never, setupChecks: ['credential test'] }
          : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'install_status')).toMatchObject({
      signal: '0 live installs / 7 catalog states / 0 setup blockers',
      status: 'blocked',
    })
  })

  it('blocks install states when setup checks are too thin', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github' ? { ...connector, setupChecks: ['credential test'] } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'install_status')).toMatchObject({
      signal: '0 live installs / 8 catalog states / 0 setup blockers',
      status: 'blocked',
    })
  })

  it('keeps install states on watch when live connections still require setup', () => {
    const catalog = buildIntegrationCatalog({
      connections: [externalConnection({ status: 'requires_setup' })],
    })

    expect(catalog.summary).toBe('1 integration catalog lanes need hardening')
    expect(catalog.lanes.find((lane) => lane.key === 'install_status')).toMatchObject({
      signal: '1 live installs / 8 catalog states / 1 setup blockers',
      status: 'watch',
    })
  })

  it('blocks health badges when catalog cards omit health signals', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github'
          ? { ...connector, healthSignals: ['credential test'] }
          : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'health_badge')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks health badges when a catalog card has no badge metadata', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'github' ? { ...connector, healthBadge: '' as never } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'health_badge')).toMatchObject({
      evidence: '7/8 health badges / 0 degraded / 0 quarantined',
      status: 'blocked',
    })
  })

  it('shows live connector watch badges before the latest credential test is healthy', () => {
    const catalog = buildIntegrationCatalog({
      connections: [externalConnection({ lastTestStatus: 'pending', status: 'active' })],
    })

    expect(catalog.summary).toBe('integration catalog is verified')
    expect(catalog.connectors.find((connector) => connector.id === 'github')).toMatchObject({
      runtimeHealthBadge: 'watch',
      runtimeInstallStatus: 'installed',
    })
  })

  it('blocks sample replay evidence when normalized samples are missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'csv' ? { ...connector, replayNormalizedType: '' } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'sample_replay')).toMatchObject({
      evidence: '8/8 fixture paths / 7/8 normalized samples',
      status: 'blocked',
    })
  })

  it('keeps upgrade paths on watch when a connector requires migration', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'jira'
          ? { ...connector, upgradeCompatibility: 'requires_migration' as const }
          : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes need hardening')
    expect(catalog.lanes.find((lane) => lane.key === 'upgrade_path')).toMatchObject({
      evidence: '8/8 upgrade paths / 8/8 rollback plans / 1 migrations',
      status: 'watch',
    })
  })

  it('blocks upgrade readiness when the migration path is missing', () => {
    const artifacts: IntegrationCatalogArtifacts = {
      ...defaultIntegrationCatalogArtifacts,
      connectors: defaultIntegrationCatalogArtifacts.connectors.map((connector) =>
        connector.id === 'hubspot' ? { ...connector, upgradePath: '' } : connector,
      ),
    }

    const catalog = buildIntegrationCatalog({ artifacts })

    expect(catalog.summary).toBe('1 integration catalog lanes are blocked')
    expect(catalog.lanes.find((lane) => lane.key === 'upgrade_path')).toMatchObject({
      signal: '7 upgrade paths / rollback 8/8',
      status: 'blocked',
    })
  })
})

function externalConnection(patch: Partial<ExternalConnection> = {}): ExternalConnection {
  return {
    authType: 'token',
    baseUrl: '',
    createdAt: '2026-07-08T01:00:00Z',
    createdBy: 'user-a11y',
    enabled: true,
    id: 'conn-github-prod',
    lastError: '',
    lastTestStatus: 'ok',
    lastTestedAt: '2026-07-08T02:00:00Z',
    name: 'GitHub Prod',
    provider: 'github',
    providerConfigJson: '{"owner":"acme","repo":"console"}',
    scopes: ['issues'],
    status: 'active',
    tenantId: 'tenant-1',
    updatedAt: '2026-07-08T02:00:00Z',
    updatedBy: 'user-a11y',
    ...patch,
    providerInstallationId: patch.providerInstallationId ?? 'provider-installation-github',
    webhookSecretConfigured: patch.webhookSecretConfigured ?? true,
  }
}
