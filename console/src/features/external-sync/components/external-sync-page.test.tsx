import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { describe, expect, it, vi } from 'vitest'
import {
  type CreateExternalConnectionRequest,
  ExternalSyncConflictResolution,
  ExternalSyncDirection,
} from '@/features/external-sync/api/external-sync'
import {
  activeConnectionActionIDsFromMutations,
  activeMappingActionIDsFromMutations,
  activeRunActionIDsFromMutations,
  BatchConflictResolutionControls,
  ConflictResolutionControls,
  ConnectionsCard,
  CreateConnectionDialog,
  CreateProviderInstallationDialog,
  canShowRecordTimeline,
  capabilityGrade,
  DiagnosticRows,
  directionLabel,
  EditConnectionDialog,
  EventDetailCard,
  EventsCard,
  ExternalSyncPage,
  errorMessage,
  eventSignatureLabel,
  eventStatusLabel,
  formatDate,
  isActiveRun,
  isReplayableEvent,
  isRetryableRunStatus,
  MappingEditor,
  mappingAllowsPull,
  normalizeJSONInput,
  ProviderInstallationsCard,
  prettyJSON,
  providerConfigFromInstallationResources,
  qualificationToastDescription,
  RecordTimelinePanel,
  RunDetailCard,
  RunsCard,
  recordTimelineTargetFromConflict,
  recordTimelineTargetFromFailure,
  StatusPill,
  shortID,
  statusLabel,
  unknownSchemaFields,
} from '@/features/external-sync/components/external-sync-page'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() },
}))

function runDetailResponse(id: string, overrides: Record<string, unknown> = {}) {
  return {
    run: {
      id,
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      mappingId: 'mapping-1',
      direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
      trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
      status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
      attempts: 0,
      nextRetryAt: '',
      startedAt: '',
      finishedAt: '',
      cursorBeforeJson: '{}',
      cursorAfterJson: '{}',
      recordsSeen: 0,
      recordsChanged: 0,
      recordsFailed: 0,
      conflictsCreated: 0,
      errorKind: '',
      errorMessage: '',
      actorId: 'admin',
      createdAt: '2026-07-08T02:10:00Z',
      updatedAt: '2026-07-08T02:10:00Z',
      inFlight: false,
      ...overrides,
    },
    attempts: [],
    failures: [],
    conflicts: [],
  }
}

describe('ExternalSyncPage', () => {
  it('renders provider installations and saves exact resource selection', async () => {
    const onSelect = vi.fn()
    const onQualify = vi.fn()
    const onDelete = vi.fn()
    const onSaveResources = vi.fn()
    const { user } = renderWithProviders(
      <ProviderInstallationsCard
        installations={[
          {
            id: 'pi-1',
            tenantId: 'tenant-1',
            provider: 'github',
            displayName: 'GitHub App',
            installationKind: 'github_app',
            status: 'active',
            externalInstallationId: '123',
            accountLogin: 'acme',
            accountId: '',
            accountUrl: '',
            baseUrl: '',
            permissionsJson: '{"metadata":"read","issues":"write"}',
            capabilityProfileJson: '{"grade":"full_app"}',
            resourceSelection: 'selected',
            qualificationStatus: 'ok',
            lastQualifiedAt: '',
            lastError: '',
            createdBy: 'admin',
            updatedBy: 'admin',
            createdAt: '2026-07-08T01:00:00Z',
            updatedAt: '2026-07-08T01:00:00Z',
          },
        ]}
        resources={[
          {
            id: 'res-1',
            tenantId: 'tenant-1',
            installationId: 'pi-1',
            provider: 'github',
            resourceType: 'repository',
            externalResourceId: '',
            resourceKey: 'acme/app',
            displayName: 'acme/app',
            htmlUrl: 'https://github.com/acme/app',
            selected: true,
            status: 'active',
            permissionsJson: '{}',
            lastSeenAt: '',
            createdAt: '2026-07-08T01:00:00Z',
            updatedAt: '2026-07-08T01:00:00Z',
          },
          {
            id: 'res-2',
            tenantId: 'tenant-1',
            installationId: 'pi-1',
            provider: 'github',
            resourceType: 'repository',
            externalResourceId: '',
            resourceKey: 'acme/other',
            displayName: 'acme/other',
            htmlUrl: 'https://github.com/acme/other',
            selected: false,
            status: 'active',
            permissionsJson: '{}',
            lastSeenAt: '',
            createdAt: '2026-07-08T01:00:00Z',
            updatedAt: '2026-07-08T01:00:00Z',
          },
        ]}
        loading={false}
        resourcesLoading={false}
        selectedID="pi-1"
        selecting={false}
        onSelect={onSelect}
        onQualify={onQualify}
        onDelete={onDelete}
        onSaveResources={onSaveResources}
      />,
    )

    expect(screen.getByText('GitHub App')).toBeInTheDocument()
    expect(screen.getByText('full_app')).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText(/acme\/app/)).toBeChecked())
    await user.click(screen.getByLabelText(/acme\/app/))
    await user.click(screen.getByLabelText(/acme\/other/))
    await user.click(screen.getByRole('button', { name: '保存' }))

    expect(onSaveResources).toHaveBeenCalledWith('pi-1', ['res-2'])
  })

  it('creates provider installation requests from the dialog', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <CreateProviderInstallationDialog
        open
        pending={false}
        providers={[{ provider: 'github', display: 'GitHub' }]}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    await user.type(screen.getByLabelText('显示名称'), 'GitHub App')
    await user.type(screen.getByLabelText('外部安装 ID'), '12345')
    await user.type(screen.getByLabelText('账号或组织'), 'acme')
    await user.type(screen.getByLabelText('资源键'), 'acme/app')
    await user.click(screen.getByRole('button', { name: '新建' }))

    expect(onSubmit).toHaveBeenCalledWith({
      provider: 'github',
      displayName: 'GitHub App',
      installationKind: 'github_app',
      externalInstallationId: '12345',
      accountLogin: 'acme',
      accountId: '',
      accountUrl: '',
      baseUrl: '',
      permissionsJson: '{"metadata":"read","issues":"write"}',
      capabilityProfileJson: '{}',
      resourceSelection: 'selected',
      resources: [
        {
          resourceType: 'repository',
          externalResourceId: '',
          resourceKey: 'acme/app',
          displayName: 'acme/app',
          htmlUrl: '',
          selected: true,
          status: 'active',
          permissionsJson: '{}',
        },
      ],
    })
  })

  it('binds create connection requests to the selected provider installation', async () => {
    const selectedInstallation = {
      id: 'pi-1',
      tenantId: 'tenant-1',
      provider: 'github',
      displayName: 'GitHub App',
      installationKind: 'github_app',
      status: 'active',
      externalInstallationId: '123',
      accountLogin: 'acme',
      accountId: '',
      accountUrl: 'https://github.com/acme',
      baseUrl: 'https://api.github.com',
      permissionsJson: '{"metadata":"read","issues":"write"}',
      capabilityProfileJson: '{"grade":"full_app"}',
      resourceSelection: 'selected',
      qualificationStatus: 'ok',
      lastQualifiedAt: '2026-07-08T02:00:00Z',
      lastError: '',
      createdBy: 'admin',
      updatedBy: 'admin',
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T02:00:00Z',
    }
    const selectedInstallationResources = [
      {
        id: 'res-1',
        tenantId: 'tenant-1',
        installationId: 'pi-1',
        provider: 'github',
        resourceType: 'repository',
        externalResourceId: '100',
        resourceKey: 'acme/app',
        displayName: 'acme/app',
        htmlUrl: 'https://github.com/acme/app',
        selected: true,
        status: 'active',
        permissionsJson: '{}',
        lastSeenAt: '2026-07-08T02:00:00Z',
        createdAt: '2026-07-08T01:00:00Z',
        updatedAt: '2026-07-08T02:00:00Z',
      },
    ]
    const renderDialog = (onSubmit: (body: CreateExternalConnectionRequest) => void) =>
      renderWithProviders(
        <CreateConnectionDialog
          open
          pending={false}
          providers={[
            { provider: 'github', display: 'GitHub' },
            { provider: 'jira', display: 'Jira' },
          ]}
          selectedInstallation={selectedInstallation}
          selectedInstallationResources={selectedInstallationResources}
          onOpenChange={vi.fn()}
          onSubmit={onSubmit}
        />,
      )

    const onSubmit = vi.fn()
    let view = renderDialog(onSubmit)
    await waitFor(() => expect(screen.getByLabelText('名称')).toHaveValue('GitHub App'))
    await view.user.type(screen.getByLabelText('凭据'), 'gh-token')
    await view.user.click(screen.getByRole('button', { name: '新建' }))

    expect(onSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        provider: 'github',
        name: 'GitHub App',
        baseUrl: 'https://api.github.com',
        providerConfigJson: '{"owner":"acme","repo":"app"}',
        providerInstallationId: 'pi-1',
      }),
    )
    view.unmount()

    const onProviderChangedSubmit = vi.fn()
    view = renderDialog(onProviderChangedSubmit)
    await waitFor(() => expect(screen.getByLabelText('名称')).toHaveValue('GitHub App'))
    await view.user.click(screen.getByRole('combobox', { name: 'Provider' }))
    await view.user.click(screen.getByRole('option', { name: 'Jira' }))
    await view.user.type(screen.getByLabelText('凭据'), 'jira-token')
    await view.user.click(screen.getByRole('button', { name: '新建' }))

    expect(onProviderChangedSubmit).toHaveBeenCalledWith(
      expect.objectContaining({
        provider: 'jira',
        baseUrl: '',
        providerConfigJson: '{}',
        providerInstallationId: '',
      }),
    )
  })

  it('renders health, connection, mapping, runs, and selected run detail', async () => {
    let requestedRun: unknown
    let updatedConnection: unknown
    let resolvedConflict: unknown
    let batchResolvedConflict: unknown
    let qualifiedConnectionID = ''
    let resumedConnectionID = ''
    let retriedRunID = ''
    let retriedFailureID = ''
    let replayedEventID = ''
    let resetMappingID = ''
    let savedMapping: unknown
    let previewBody: unknown
    let backfillBody: unknown
    let timelineBody: unknown
    const runQueries: string[] = []
    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 1,
          deadRuns: 0,
          openConflicts: 1,
          newestSuccessfulRunAt: '2026-07-08T02:03:04Z',
          disabledConnections: 1,
          throttledRuns: 1,
          unauthorizedRuns: 1,
          providerUnavailableRuns: 1,
          delayedRetryRuns: 1,
          newestRetryAfter: '2026-07-08T03:04:05Z',
          degradedConnections: 1,
          quarantinedConnections: 1,
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: [
            {
              id: 'conn-1',
              tenantId: 'tenant-1',
              provider: 'github',
              name: 'GitHub Prod',
              enabled: true,
              status: 'active',
              authType: 'token',
              baseUrl: '',
              providerConfigJson: '{}',
              scopes: ['issues'],
              lastTestedAt: '',
              lastTestStatus: 'ok',
              lastError: '',
              createdBy: 'admin',
              updatedBy: 'admin',
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:00:00Z',
              webhookSecretConfigured: true,
            },
            {
              id: 'conn-2',
              tenantId: 'tenant-1',
              provider: 'github',
              name: 'GitHub Quarantined',
              enabled: false,
              status: 'quarantined',
              authType: 'token',
              baseUrl: '',
              providerConfigJson: '{}',
              scopes: ['issues'],
              lastTestedAt: '',
              lastTestStatus: 'failed',
              lastError: 'provider_unavailable: repeated failures',
              createdBy: 'admin',
              updatedBy: 'admin',
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:30:00Z',
              webhookSecretConfigured: false,
            },
          ],
        }),
      ),
      http.patch('/fb/v1/console/external-sync/connections/conn-1', async ({ request }) => {
        updatedConnection = await request.json()
        return HttpResponse.json({
          id: 'conn-1',
          tenantId: 'tenant-1',
          provider: 'github',
          name: 'GitHub Enterprise',
          enabled: false,
          status: 'active',
          authType: 'token',
          baseUrl: 'https://github.example.com/api/v3',
          providerConfigJson: '{"repo_url":"https://github.example.com/acme/app"}',
          scopes: ['issues', 'pull'],
          lastTestedAt: '',
          lastTestStatus: 'ok',
          lastError: '',
          createdBy: 'admin',
          updatedBy: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:20:00Z',
          webhookSecretConfigured: true,
        })
      }),
      http.post('/fb/v1/console/external-sync/connections/conn-1:qualify', () => {
        qualifiedConnectionID = 'conn-1'
        return HttpResponse.json({
          connectionId: 'conn-1',
          ready: true,
          checks: [
            {
              name: 'provider_check',
              status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK',
              summary: 'Provider check succeeded',
              detailJson: '{"latency_ms":12}',
            },
          ],
        })
      }),
      http.post('/fb/v1/console/external-sync/connections/conn-2:resume', () => {
        resumedConnectionID = 'conn-2'
        return HttpResponse.json({
          id: 'conn-2',
          tenantId: 'tenant-1',
          provider: 'github',
          name: 'GitHub Quarantined',
          enabled: true,
          status: 'active',
          authType: 'token',
          baseUrl: '',
          providerConfigJson: '{}',
          scopes: ['issues'],
          lastTestedAt: '',
          lastTestStatus: 'failed',
          lastError: '',
          createdBy: 'admin',
          updatedBy: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:45:00Z',
          webhookSecretConfigured: false,
        })
      }),
      http.get('/fb/v1/console/external-sync/connections/conn-1/schema', () =>
        HttpResponse.json({
          schemas: [
            {
              type: 'issue',
              fields: ['number', 'title', 'state', 'labels', 'updated_at'],
              requiredFields: ['title'],
              writableFields: ['title', 'state', 'labels'],
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections/conn-2/schema', () =>
        HttpResponse.json({
          schemas: [
            {
              type: 'issue',
              fields: ['number', 'title', 'state', 'labels', 'updated_at'],
              requiredFields: ['title'],
              writableFields: ['title', 'state', 'labels'],
            },
          ],
        }),
      ),
      http.put('/fb/v1/console/external-sync/mappings/mapping-1', async ({ request }) => {
        savedMapping = await request.json()
        return HttpResponse.json({
          id: 'mapping-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          localObjectType: 'customer_request',
          externalObjectType: 'issue',
          direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
          fieldMappingJson: '{}',
          statusMappingJson: '{}',
          conflictPolicy: 'manual',
          tombstonePolicy: 'mark_stale',
          enabled: true,
          mappingVersion: 2,
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:25:00Z',
        })
      }),
      http.get('/fb/v1/console/external-sync/mappings', () =>
        HttpResponse.json({
          mappings: [
            {
              id: 'mapping-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              localObjectType: 'customer_request',
              externalObjectType: 'issue',
              direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
              fieldMappingJson: '{}',
              statusMappingJson: '{}',
              conflictPolicy: 'manual',
              tombstonePolicy: 'mark_stale',
              enabled: true,
              mappingVersion: 1,
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/runs', ({ request }) => {
        const url = new URL(request.url)
        runQueries.push(url.search)
        if (url.searchParams.get('before_id') === 'run-1') {
          return HttpResponse.json({
            runs: [
              {
                id: 'run-0',
                tenantId: 'tenant-1',
                connectionId: 'conn-1',
                mappingId: 'mapping-1',
                direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
                trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
                status: 'failed',
                attempts: 2,
                nextRetryAt: '',
                startedAt: '2026-07-08T01:00:00Z',
                finishedAt: '2026-07-08T01:01:00Z',
                cursorBeforeJson: '{}',
                cursorAfterJson: '{}',
                recordsSeen: 5,
                recordsChanged: 0,
                recordsFailed: 5,
                conflictsCreated: 0,
                errorKind: 'provider_unavailable',
                errorMessage: 'temporary outage',
                actorId: 'admin',
                createdAt: '2026-07-08T01:00:00Z',
                updatedAt: '2026-07-08T01:01:00Z',
                inFlight: false,
              },
            ],
            nextBeforeId: '',
          })
        }
        return HttpResponse.json({
          runs: [
            {
              id: 'run-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              mappingId: 'mapping-1',
              direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
              trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
              status: 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED',
              attempts: 1,
              nextRetryAt: '',
              startedAt: '2026-07-08T02:00:00Z',
              finishedAt: '2026-07-08T02:01:00Z',
              cursorBeforeJson: '{"page":1}',
              cursorAfterJson: '{"page":2}',
              recordsSeen: 3,
              recordsChanged: 2,
              recordsFailed: 1,
              conflictsCreated: 1,
              errorKind: '',
              errorMessage: '',
              actorId: 'admin',
              createdAt: '2026-07-08T02:00:00Z',
              updatedAt: '2026-07-08T02:01:00Z',
              inFlight: false,
            },
          ],
          nextBeforeId: 'run-1',
        })
      }),
      http.get('/fb/v1/console/external-sync/events', () =>
        HttpResponse.json({
          events: [
            {
              id: 'event-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              mappingId: 'mapping-1',
              provider: 'github',
              eventType: 'issues',
              externalEventId: 'delivery-1',
              dedupeKey: 'github:issues:delivery-1',
              signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED',
              status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
              payloadDigest: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
              normalizedPayloadJson: '{}',
              receivedAt: '2026-07-08T02:05:00Z',
              replayedAt: '',
              replayedBy: '',
              runId: '',
              failureReason: '',
              createdAt: '2026-07-08T02:05:00Z',
              updatedAt: '2026-07-08T02:05:00Z',
            },
          ],
          nextBeforeId: '',
        }),
      ),
      http.get('/fb/v1/console/external-sync/events/event-1', () =>
        HttpResponse.json({
          id: 'event-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          provider: 'github',
          eventType: 'issues',
          externalEventId: 'delivery-1',
          dedupeKey: 'github:issues:delivery-1',
          signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED',
          status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
          payloadDigest: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
          normalizedPayloadJson: '{"action":"opened","delivery":"delivery-1"}',
          receivedAt: '2026-07-08T02:05:00Z',
          replayedAt: '',
          replayedBy: '',
          runId: '',
          failureReason: '',
          createdAt: '2026-07-08T02:05:00Z',
          updatedAt: '2026-07-08T02:05:00Z',
        }),
      ),
      http.post('/fb/v1/console/external-sync/runs', async ({ request }) => {
        requestedRun = await request.json()
        return HttpResponse.json({
          id: 'run-push-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
          trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
          status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
          attempts: 0,
          nextRetryAt: '',
          startedAt: '',
          finishedAt: '',
          cursorBeforeJson: '{}',
          cursorAfterJson: '{}',
          recordsSeen: 0,
          recordsChanged: 0,
          recordsFailed: 0,
          conflictsCreated: 0,
          errorKind: '',
          errorMessage: '',
          actorId: 'admin',
          createdAt: '2026-07-08T02:10:00Z',
          updatedAt: '2026-07-08T02:10:00Z',
          inFlight: false,
        })
      }),
      http.post('/fb/v1/console/external-sync/mappings/mapping-1:reset-cursor', () => {
        resetMappingID = 'mapping-1'
        return HttpResponse.json(
          {
            mapping: {
              id: 'mapping-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              localObjectType: 'customer_request',
              externalObjectType: 'issue',
              direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
              fieldMappingJson: '{}',
              statusMappingJson: '{}',
              conflictPolicy: 'manual',
              tombstonePolicy: 'mark_stale',
              enabled: true,
              mappingVersion: 1,
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:00:00Z',
            },
            run: {
              id: 'run-reset-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              mappingId: 'mapping-1',
              direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
              trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
              status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
              attempts: 0,
              nextRetryAt: '',
              startedAt: '',
              finishedAt: '',
              cursorBeforeJson: '{}',
              cursorAfterJson: '{}',
              recordsSeen: 0,
              recordsChanged: 0,
              recordsFailed: 0,
              conflictsCreated: 0,
              errorKind: '',
              errorMessage: '',
              actorId: 'admin-1',
              createdAt: '2026-07-08T02:30:00Z',
              updatedAt: '2026-07-08T02:30:00Z',
              inFlight: false,
            },
          },
          { status: 202 },
        )
      }),
      http.post(
        '/fb/v1/console/external-sync/mappings/:mappingAction',
        async ({ params, request }) => {
          const action = String(params.mappingAction)
          if (action.includes('preview')) {
            previewBody = await request.json()
            return HttpResponse.json({
              schema: {
                type: 'issue',
                fields: ['number', 'title', 'state'],
                requiredFields: ['title'],
                writableFields: ['title', 'state'],
              },
              errors: [],
              warnings: [],
            })
          }
          const payload = await request.json()
          backfillBody = { action: params.mappingAction, payload }
          if (!action.includes('backfill')) {
            return HttpResponse.json({}, { status: 404 })
          }
          return HttpResponse.json(
            {
              mapping: {
                id: 'mapping-1',
                tenantId: 'tenant-1',
                connectionId: 'conn-1',
                localObjectType: 'customer_request',
                externalObjectType: 'issue',
                direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
                fieldMappingJson: '{}',
                statusMappingJson: '{}',
                conflictPolicy: 'manual',
                tombstonePolicy: 'mark_stale',
                enabled: true,
                mappingVersion: 1,
                createdAt: '2026-07-08T01:00:00Z',
                updatedAt: '2026-07-08T01:00:00Z',
              },
              run: {
                id: 'run-backfill-1',
                tenantId: 'tenant-1',
                connectionId: 'conn-1',
                mappingId: 'mapping-1',
                direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
                trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_BACKFILL',
                status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
                attempts: 0,
                nextRetryAt: '',
                startedAt: '',
                finishedAt: '',
                cursorBeforeJson: '{}',
                cursorAfterJson: '{}',
                recordsSeen: 0,
                recordsChanged: 0,
                recordsFailed: 0,
                conflictsCreated: 0,
                errorKind: '',
                errorMessage: '',
                actorId: 'admin-1',
                createdAt: '2026-07-08T02:35:00Z',
                updatedAt: '2026-07-08T02:35:00Z',
                inFlight: false,
              },
            },
            { status: 202 },
          )
        },
      ),
      http.post('/fb/v1/console/external-sync/runs/run-0:retry', () => {
        retriedRunID = 'run-0'
        return HttpResponse.json({
          id: 'run-0',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
          trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_RETRY',
          status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
          attempts: 2,
          nextRetryAt: '',
          startedAt: '',
          finishedAt: '',
          cursorBeforeJson: '{}',
          cursorAfterJson: '{}',
          recordsSeen: 5,
          recordsChanged: 0,
          recordsFailed: 5,
          conflictsCreated: 0,
          errorKind: '',
          errorMessage: '',
          actorId: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:15:00Z',
          inFlight: false,
        })
      }),
      http.get('/fb/v1/console/external-sync/runs/run-0', () =>
        HttpResponse.json(
          runDetailResponse('run-0', {
            direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_RETRY',
            attempts: 2,
            recordsSeen: 5,
            recordsFailed: 5,
          }),
        ),
      ),
      http.post('/fb/v1/console/external-sync/failures/failure-1:retry', () => {
        retriedFailureID = 'failure-1'
        return HttpResponse.json({
          id: 'failure-1',
          tenantId: 'tenant-1',
          runId: 'run-1',
          mappingId: 'mapping-1',
          operation: 'push',
          localObjectId: 'cr-42',
          externalKey: 'issue-42',
          failureKind: 'validation',
          message: 'missing required provider field',
          payloadDigest: 'sha256:abc123',
          retryMode: 'manual',
          normalizedPayloadJson: '{"number":42,"title":"Quota bug"}',
          retryable: false,
          resolvedAt: '2026-07-08T02:07:00Z',
          resolvedBy: 'admin',
          createdAt: '2026-07-08T02:01:00Z',
        })
      }),
      http.post('/fb/v1/console/external-sync/events/event-1:replay', () => {
        replayedEventID = 'event-1'
        return HttpResponse.json({
          event: {
            id: 'event-1',
            tenantId: 'tenant-1',
            connectionId: 'conn-1',
            mappingId: 'mapping-1',
            provider: 'github',
            eventType: 'issues',
            externalEventId: 'delivery-1',
            dedupeKey: 'github:issues:delivery-1',
            signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED',
            status: 'EXTERNAL_SYNC_EVENT_STATUS_REPLAYED',
            payloadDigest: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
            normalizedPayloadJson: '{}',
            receivedAt: '2026-07-08T02:05:00Z',
            replayedAt: '2026-07-08T02:06:00Z',
            replayedBy: 'admin',
            runId: 'run-event-1',
            failureReason: '',
            createdAt: '2026-07-08T02:05:00Z',
            updatedAt: '2026-07-08T02:06:00Z',
          },
          run: {
            id: 'run-event-1',
            tenantId: 'tenant-1',
            connectionId: 'conn-1',
            mappingId: 'mapping-1',
            direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_WEBHOOK',
            status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
            attempts: 0,
            nextRetryAt: '',
            startedAt: '',
            finishedAt: '',
            cursorBeforeJson: '{}',
            cursorAfterJson: '{}',
            recordsSeen: 0,
            recordsChanged: 0,
            recordsFailed: 0,
            conflictsCreated: 0,
            errorKind: '',
            errorMessage: '',
            actorId: 'admin',
            createdAt: '2026-07-08T02:06:00Z',
            updatedAt: '2026-07-08T02:06:00Z',
            inFlight: false,
          },
        })
      }),
      http.get('/fb/v1/console/external-sync/runs/run-event-1', () =>
        HttpResponse.json(
          runDetailResponse('run-event-1', {
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_WEBHOOK',
          }),
        ),
      ),
      http.get('/fb/v1/console/external-sync/runs/run-push-1', () =>
        HttpResponse.json(
          runDetailResponse('run-push-1', {
            direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
          }),
        ),
      ),
      http.get('/fb/v1/console/external-sync/runs/run-reset-1', () =>
        HttpResponse.json(runDetailResponse('run-reset-1')),
      ),
      http.get('/fb/v1/console/external-sync/runs/run-backfill-1', () =>
        HttpResponse.json(
          runDetailResponse('run-backfill-1', {
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_BACKFILL',
          }),
        ),
      ),
      http.post('/fb/v1/console/external-sync/records:timeline', async ({ request }) => {
        timelineBody = await request.json()
        return HttpResponse.json({
          entries: [
            {
              kind: 'link',
              occurredAt: '2026-07-08T02:01:00Z',
              runId: '',
              status: 'synced',
              operation: 'link',
              localObjectId: 'cr-42',
              externalKey: 'issue-42',
              summary: 'Object link updated',
              detailJson: '{"sync_state":"synced"}',
            },
            {
              kind: 'failure',
              occurredAt: '2026-07-08T02:01:30Z',
              runId: 'run-1',
              status: 'open',
              operation: 'push',
              localObjectId: 'cr-42',
              externalKey: 'issue-42',
              summary: 'validation: missing required provider field',
              detailJson: '{"retryable":true}',
            },
          ],
        })
      }),
      http.post(
        '/fb/v1/console/external-sync/conflicts/conflict-1:resolve',
        async ({ request }) => {
          resolvedConflict = await request.json()
          return HttpResponse.json({
            id: 'conflict-1',
            tenantId: 'tenant-1',
            mappingId: 'mapping-1',
            localObjectId: 'cr-43',
            externalKey: 'issue-43',
            conflictKind: 'version_mismatch',
            status: 'ignored',
            localSnapshotJson: '{"status":"planned"}',
            externalSnapshotJson: '{"state":"closed"}',
            resolution: 'ignored',
            resolvedAt: '2026-07-08T02:03:00Z',
            resolvedBy: 'admin',
            createdAt: '2026-07-08T02:01:00Z',
            updatedAt: '2026-07-08T02:03:00Z',
          })
        },
      ),
      http.post('/fb/v1/console/external-sync/conflicts:batch-resolve', async ({ request }) => {
        batchResolvedConflict = await request.json()
        return HttpResponse.json({
          conflicts: [
            {
              id: 'conflict-1',
              tenantId: 'tenant-1',
              mappingId: 'mapping-1',
              localObjectId: 'cr-43',
              externalKey: 'issue-43',
              conflictKind: 'version_mismatch',
              status: 'resolved',
              localSnapshotJson: '{"status":"planned"}',
              externalSnapshotJson: '{"state":"closed"}',
              resolution: 'external_wins',
              resolvedAt: '2026-07-08T02:04:00Z',
              resolvedBy: 'admin',
              createdAt: '2026-07-08T02:01:00Z',
              updatedAt: '2026-07-08T02:04:00Z',
            },
            {
              id: 'conflict-2',
              tenantId: 'tenant-1',
              mappingId: 'mapping-1',
              localObjectId: 'cr-44',
              externalKey: 'issue-44',
              conflictKind: 'version_mismatch',
              status: 'resolved',
              localSnapshotJson: '{"status":"planned"}',
              externalSnapshotJson: '{"state":"closed"}',
              resolution: 'external_wins',
              resolvedAt: '2026-07-08T02:04:00Z',
              resolvedBy: 'admin',
              createdAt: '2026-07-08T02:01:30Z',
              updatedAt: '2026-07-08T02:04:00Z',
            },
          ],
          resolvedCount: 2,
        })
      }),
      http.get('/fb/v1/console/external-sync/runs/run-1', () =>
        HttpResponse.json({
          run: {
            id: 'run-1',
            tenantId: 'tenant-1',
            connectionId: 'conn-1',
            mappingId: 'mapping-1',
            direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
            trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
            status: 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED',
            attempts: 2,
            nextRetryAt: '',
            startedAt: '2026-07-08T02:00:00Z',
            finishedAt: '2026-07-08T02:01:00Z',
            cursorBeforeJson: '{"page":1}',
            cursorAfterJson: '{"page":2}',
            recordsSeen: 3,
            recordsChanged: 2,
            recordsFailed: 1,
            conflictsCreated: 1,
            errorKind: '',
            errorMessage: '',
            actorId: 'admin',
            createdAt: '2026-07-08T02:00:00Z',
            updatedAt: '2026-07-08T02:01:00Z',
            inFlight: false,
          },
          attempts: [
            {
              id: '1',
              runId: 'run-1',
              attemptNumber: 1,
              startedAt: '2026-07-08T02:00:00Z',
              finishedAt: '2026-07-08T02:00:15Z',
              result: 'retryable_error',
              httpStatus: 429,
              providerRequestId: 'gh-req-1',
              retryAfter: '2026-07-08T02:02:00Z',
              errorKind: 'rate_limited',
              errorMessage: 'secondary rate limit',
            },
            {
              id: '2',
              runId: 'run-1',
              attemptNumber: 2,
              startedAt: '2026-07-08T02:02:00Z',
              finishedAt: '2026-07-08T02:02:20Z',
              result: 'succeeded',
              httpStatus: 200,
              providerRequestId: 'gh-req-2',
              retryAfter: '',
              errorKind: '',
              errorMessage: '',
            },
          ],
          failures: [
            {
              id: 'failure-1',
              tenantId: 'tenant-1',
              runId: 'run-1',
              mappingId: 'mapping-1',
              operation: 'push',
              localObjectId: 'cr-42',
              externalKey: 'issue-42',
              failureKind: 'validation',
              message: 'missing required provider field',
              payloadDigest: 'sha256:abc123',
              retryMode: 'manual',
              normalizedPayloadJson: '{"number":42,"title":"Quota bug"}',
              retryable: true,
              resolvedAt: '',
              resolvedBy: '',
              createdAt: '2026-07-08T02:01:00Z',
            },
          ],
          conflicts: [
            {
              id: 'conflict-1',
              tenantId: 'tenant-1',
              mappingId: 'mapping-1',
              localObjectId: 'cr-43',
              externalKey: 'issue-43',
              conflictKind: 'version_mismatch',
              status: 'open',
              localSnapshotJson: '{"status":"planned"}',
              externalSnapshotJson: '{"state":"closed"}',
              resolution: '',
              resolvedAt: '',
              resolvedBy: '',
              createdAt: '2026-07-08T02:01:00Z',
              updatedAt: '2026-07-08T02:01:00Z',
            },
            {
              id: 'conflict-2',
              tenantId: 'tenant-1',
              mappingId: 'mapping-1',
              localObjectId: 'cr-44',
              externalKey: 'issue-44',
              conflictKind: 'version_mismatch',
              status: 'open',
              localSnapshotJson: '{"status":"planned"}',
              externalSnapshotJson: '{"state":"open"}',
              resolution: '',
              resolvedAt: '',
              resolvedBy: '',
              createdAt: '2026-07-08T02:01:30Z',
              updatedAt: '2026-07-08T02:01:30Z',
            },
          ],
        }),
      ),
    )

    const { user } = renderWithProviders(<ExternalSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('GitHub Prod')).toBeInTheDocument()
    })
    expect(screen.getByText('Webhook secret 已配置')).toBeInTheDocument()
    expect(screen.getByText('启用连接')).toBeInTheDocument()
    expect(screen.getByText('被限流')).toBeInTheDocument()
    expect(screen.getByText('鉴权异常')).toBeInTheDocument()
    expect(screen.getByText('服务商异常')).toBeInTheDocument()
    expect(screen.getByText('延迟重试')).toBeInTheDocument()
    expect(screen.getByText('持续失败')).toBeInTheDocument()
    expect(screen.getByText('已隔离')).toBeInTheDocument()
    expect(screen.getByText('customer_request → issue')).toBeInTheDocument()
    expect(screen.getByText('Provider schema')).toBeInTheDocument()
    expect(screen.getByText('updated_at')).toBeInTheDocument()
    expect(screen.getByText('见 3')).toBeInTheDocument()
    expect(screen.getByText('issues')).toBeInTheDocument()
    const integrationCatalog = screen.getByTestId('external-sync-integration-catalog')
    expect(integrationCatalog).toHaveTextContent('集成目录')
    expect(integrationCatalog).toHaveTextContent(
      '8/8 connectors / 8 install states / 8 permission maps / 8 sample replays / 8 upgrade paths / verifier on',
    )
    expect(integrationCatalog).toHaveTextContent('1 integration catalog lanes need hardening')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-catalog_cards'),
    ).toHaveTextContent(
      '8 catalog cards / Jira, GitHub, Intercom, Zendesk, Salesforce, HubSpot, Custom webhook, CSV',
    )
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-install_status'),
    ).toHaveTextContent('1 live installs / 8 catalog states / 0 setup blockers')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-permission_scope'),
    ).toHaveTextContent('8 permission maps / 23 scopes / least privilege on')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-health_badge'),
    ).toHaveTextContent('8 health badges / 1 unhealthy tenant connector')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-sample_replay'),
    ).toHaveTextContent('8 replay fixtures / 8 normalized samples')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-upgrade_path'),
    ).toHaveTextContent('8 upgrade paths / rollback 8/8')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-connector-github'),
    ).toHaveTextContent('租户已安装')
    expect(
      within(integrationCatalog).getByTestId('external-sync-integration-catalog-connector-csv'),
    ).toHaveTextContent('template-v1-to-workbench-v2')
    const upgradeDiagnostics = screen.getByTestId('external-sync-upgrade-diagnostics')
    expect(upgradeDiagnostics).toHaveTextContent('升级诊断')
    expect(upgradeDiagnostics).toHaveTextContent(
      '6/6 checks / verifier on / playbook available / compatibility available / fixtures 3/3',
    )
    expect(upgradeDiagnostics).toHaveTextContent('1 upgrade diagnostics lanes need hardening')
    expect(
      within(upgradeDiagnostics).getByTestId('external-sync-upgrade-diagnostics-install_health'),
    ).toHaveTextContent('1 installed / 1 degraded / 1 quarantined / 1 retryable')
    expect(
      within(upgradeDiagnostics).getByTestId(
        'external-sync-upgrade-diagnostics-permission_boundary',
      ),
    ).toHaveTextContent('2 scoped connections / 8 permission maps / 0 blank scopes')
    expect(
      within(upgradeDiagnostics).getByTestId('external-sync-upgrade-diagnostics-schema_drift'),
    ).toHaveTextContent('5 provider fields / 0 drift risks / mapping v1')
    expect(
      within(upgradeDiagnostics).getByTestId('external-sync-upgrade-diagnostics-webhook_readiness'),
    ).toHaveTextContent('1 verified / 0 failed / 1 configured secrets')
    expect(
      within(upgradeDiagnostics).getByTestId('external-sync-upgrade-diagnostics-fixture_replay'),
    ).toHaveTextContent('8 catalog replays / fixture lane verified / 1 received events')
    expect(
      within(upgradeDiagnostics).getByTestId(
        'external-sync-upgrade-diagnostics-version_compatibility',
      ),
    ).toHaveTextContent('8 connectors / rollback 8/8 / fixtures 3/3')
    expect(
      within(upgradeDiagnostics).getByTestId(
        'external-sync-upgrade-diagnostics-row-install_health',
      ),
    ).toHaveTextContent('恢复隔离连接或重跑凭证测试')
    const connectorConformance = screen.getByTestId('external-sync-connector-conformance-gate')
    expect(connectorConformance).toHaveTextContent('Connector conformance gate')
    expect(connectorConformance).toHaveTextContent(
      '1/1 providers / 3/3 fixtures / 6/6 hooks / 1 live connectors / 1 verified signatures',
    )
    expect(connectorConformance).toHaveTextContent('1 connector conformance lanes need hardening')
    expect(
      within(connectorConformance).getByTestId(
        'external-sync-connector-conformance-gate-webhook_signature',
      ),
    ).toHaveTextContent('1 verified / 0 failed / 1 configured secrets')
    expect(
      within(connectorConformance).getByTestId(
        'external-sync-connector-conformance-gate-field_mapping',
      ),
    ).toHaveTextContent('0 mapped fields / 5 provider fields / 0 problems')
    const fieldMappingWorkbench = screen.getByTestId('external-sync-field-mapping-workbench')
    expect(fieldMappingWorkbench).toHaveTextContent('字段映射工作台')
    expect(fieldMappingWorkbench).toHaveTextContent(
      'GitHub Prod / mapping-1 / 0/2 required fields / 5 provider fields / 0 drift risks',
    )
    expect(fieldMappingWorkbench).toHaveTextContent('3 field mapping lanes are blocked')
    expect(
      within(fieldMappingWorkbench).getByTestId(
        'external-sync-field-mapping-workbench-required_mapping',
      ),
    ).toHaveTextContent('0/2 required mapped / 2 suggested / 0 drifted / JSON valid')
    expect(
      within(fieldMappingWorkbench).getByTestId('external-sync-field-mapping-row-title'),
    ).toHaveTextContent('建议 title')
    expect(
      within(fieldMappingWorkbench).getByTestId('external-sync-field-mapping-row-status'),
    ).toHaveTextContent('建议 state')
    expect(
      runQueries.some((query) => new URLSearchParams(query).get('connection_id') === 'conn-1'),
    ).toBe(true)
    await user.click(screen.getAllByLabelText('资格检查')[0])
    await waitFor(() => {
      expect(qualifiedConnectionID).toBe('conn-1')
    })
    await waitFor(() => {
      expect(screen.getAllByLabelText('资格检查')[0]).toBeEnabled()
    })
    await user.click(screen.getByLabelText('恢复连接'))
    await waitFor(() => {
      expect(resumedConnectionID).toBe('conn-2')
    })

    await user.click(screen.getByLabelText('查看事件 event-1'))
    await waitFor(() => {
      expect(screen.getByText('事件 event-1 的 delivery ledger。')).toBeInTheDocument()
    })
    expect(screen.getByText('github:issues:delivery-1')).toBeInTheDocument()
    expect(screen.getByText('标准化 payload')).toBeInTheDocument()
    expect(screen.getByText(/"action": "opened"/)).toBeInTheDocument()

    const fieldMappingInput = screen.getByLabelText('字段映射 JSON')
    const mappingSaveButton = screen.getByRole('button', { name: '保存' })
    fireEvent.change(fieldMappingInput, { target: { value: '{bad' } })
    expect(screen.getByText('字段映射 JSON 必须是 JSON 对象')).toBeInTheDocument()
    expect(mappingSaveButton).toBeDisabled()
    fireEvent.change(fieldMappingInput, { target: { value: '{"title":"headline"}' } })
    expect(screen.getByText('未匹配 provider schema：headline')).toBeInTheDocument()
    expect(mappingSaveButton).toBeEnabled()
    fireEvent.change(fieldMappingInput, { target: { value: '{}' } })
    await user.click(mappingSaveButton)
    await waitFor(() => {
      expect(savedMapping).toMatchObject({
        fieldMappingJson: '{}',
        statusMappingJson: '{}',
      })
    })
    expect(toast.success).toHaveBeenCalledWith('映射已保存')

    await user.click(screen.getByRole('button', { name: '预检 issue 映射' }))
    await waitFor(() => {
      expect(previewBody).toMatchObject({
        fieldMappingJson: '{}',
        statusMappingJson: '{}',
      })
    })

    await user.click(screen.getByRole('checkbox', { name: '回填前重置游标' }))
    const backfillButton = screen.getByRole('button', { name: '回填 issue 记录' })
    expect(backfillButton).toBeEnabled()
    fireEvent.click(screen.getByText('回填'))
    await waitFor(() => {
      expect(backfillBody).toMatchObject({ payload: { resetCursor: true } })
    })
    await user.click(screen.getByRole('button', { name: '重置 issue 同步游标' }))
    await waitFor(() => {
      expect(resetMappingID).toBe('mapping-1')
    })

    await user.click(screen.getAllByLabelText('编辑')[0])
    const editDialog = screen.getByRole('dialog', { name: '编辑外部连接' })
    await user.clear(within(editDialog).getByLabelText('名称'))
    await user.type(within(editDialog).getByLabelText('名称'), 'GitHub Enterprise')
    await user.type(
      within(editDialog).getByLabelText('Base URL'),
      'https://github.example.com/api/v3',
    )
    await user.clear(within(editDialog).getByLabelText('配置 JSON'))
    fireEvent.change(within(editDialog).getByLabelText('配置 JSON'), {
      target: { value: '{"repo_url":"https://github.example.com/acme/app"}' },
    })
    await user.clear(within(editDialog).getByLabelText('Scopes'))
    await user.type(within(editDialog).getByLabelText('Scopes'), 'issues, pull')
    await user.type(within(editDialog).getByLabelText('Webhook secret 轮换'), 'new-secret-123456')
    await user.click(within(editDialog).getByRole('checkbox', { name: '启用连接' }))
    await user.click(within(editDialog).getByRole('button', { name: '保存' }))
    await waitFor(() => {
      expect(updatedConnection).toMatchObject({
        name: 'GitHub Enterprise',
        enabled: false,
        baseUrl: 'https://github.example.com/api/v3',
        providerConfigJson: '{"repo_url":"https://github.example.com/acme/app"}',
        scopes: ['issues', 'pull'],
        webhookSecret: 'new-secret-123456',
      })
    })
    expect(updatedConnection).not.toHaveProperty('credential')
    await waitFor(() => {
      expect(screen.getAllByLabelText('编辑')[0]).toBeEnabled()
      expect(screen.getAllByLabelText('资格检查')[0]).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: '加载更多' }))
    await waitFor(() => {
      expect(screen.getByText('见 5')).toBeInTheDocument()
    })
    expect(
      runQueries.some((query) => new URLSearchParams(query).get('before_id') === 'run-1'),
    ).toBe(true)
    await user.click(screen.getByLabelText('重试'))
    await waitFor(() => {
      expect(retriedRunID).toBe('run-0')
    })
    await user.click(screen.getByRole('button', { name: '重放' }))
    await waitFor(() => {
      expect(replayedEventID).toBe('event-1')
    })

    const requestRunButton = screen.getAllByLabelText('请求同步')[0]
    await waitFor(() => {
      expect(requestRunButton).toBeEnabled()
    })
    await user.click(requestRunButton)
    await waitFor(() => {
      expect(requestedRun).toMatchObject({
        connectionId: 'conn-1',
        mappingId: 'mapping-1',
        direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
      })
    })

    const runButton = screen.getByText('见 3').closest('button')
    expect(runButton).not.toBeNull()
    await user.click(runButton as HTMLButtonElement)

    await waitFor(() => {
      expect(screen.getByText('运行 run-1 的 attempts、失败记录和冲突。')).toBeInTheDocument()
    })
    expect(screen.getByText('运行后游标')).toBeInTheDocument()
    expect(screen.getAllByText('Provider 请求 ID').length).toBeGreaterThan(0)
    expect(screen.getByText('gh-req-1')).toBeInTheDocument()
    expect(screen.getAllByText('HTTP 状态').length).toBeGreaterThan(0)
    expect(screen.getByText('sha256:abc123')).toBeInTheDocument()
    expect(screen.getAllByText('标准化 payload').length).toBeGreaterThan(0)
    expect(screen.getAllByText('本地快照').length).toBeGreaterThan(0)
    expect(screen.getAllByText('外部快照').length).toBeGreaterThan(0)
    expect(screen.getByText('2 个待处理冲突')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '重试' }).at(-1) as HTMLButtonElement)
    await waitFor(() => {
      expect(retriedFailureID).toBe('failure-1')
    })
    expect(toast.success).toHaveBeenCalledWith('失败记录已标记重试')
    await user.click(screen.getByRole('button', { name: '批量处理' }))
    await waitFor(() => {
      expect(batchResolvedConflict).toMatchObject({
        ids: ['conflict-1', 'conflict-2'],
        resolution: 'EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS',
      })
    })
    await user.click(screen.getAllByRole('button', { name: '时间线' })[0])
    await waitFor(() => {
      expect(timelineBody).toMatchObject({
        mappingId: 'mapping-1',
        localObjectId: 'cr-42',
        externalKey: 'issue-42',
      })
    })
    expect(screen.getByText('issue-42 时间线')).toBeInTheDocument()
    expect(screen.getByText('Object link updated')).toBeInTheDocument()
    await user.click(screen.getAllByRole('combobox', { name: '处理方式' })[0])
    await user.click(screen.getByRole('option', { name: '忽略' }))
    await user.click(screen.getAllByRole('button', { name: '处理冲突' })[0])
    await waitFor(() => {
      expect(resolvedConflict).toMatchObject({
        resolution: 'EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED',
      })
    })
  }, 90_000)

  it('lets the backend choose the mapping direction when mappings are unavailable', async () => {
    let requestedRun: unknown
    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: '',
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: [
            {
              id: 'conn-1',
              tenantId: 'tenant-1',
              provider: 'github',
              name: 'GitHub Prod',
              enabled: true,
              status: 'active',
              authType: 'token',
              baseUrl: '',
              providerConfigJson: '{}',
              scopes: ['issues'],
              lastTestedAt: '',
              lastTestStatus: 'ok',
              lastError: '',
              createdBy: 'admin',
              updatedBy: 'admin',
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections/conn-1/schema', () =>
        HttpResponse.json({ schemas: [] }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', () => HttpResponse.json({ mappings: [] })),
      http.get('/fb/v1/console/external-sync/runs', () => HttpResponse.json({ runs: [] })),
      http.get('/fb/v1/console/external-sync/events', () => HttpResponse.json({ events: [] })),
      http.post('/fb/v1/console/external-sync/runs', async ({ request }) => {
        requestedRun = await request.json()
        return HttpResponse.json({
          id: 'run-default-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
          trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
          status: 'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
          attempts: 0,
          nextRetryAt: '',
          startedAt: '',
          finishedAt: '',
          cursorBeforeJson: '{}',
          cursorAfterJson: '{}',
          recordsSeen: 0,
          recordsChanged: 0,
          recordsFailed: 0,
          conflictsCreated: 0,
          errorKind: '',
          errorMessage: '',
          actorId: 'admin',
          createdAt: '2026-07-08T02:10:00Z',
          updatedAt: '2026-07-08T02:10:00Z',
          inFlight: false,
        })
      }),
    )

    const { user } = renderWithProviders(<ExternalSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('GitHub Prod')).toBeInTheDocument()
    })
    const requestRunButton = screen.getByLabelText('请求同步')
    await waitFor(() => {
      expect(requestRunButton).toBeEnabled()
    })
    await user.click(requestRunButton)

    await waitFor(() => {
      expect(requestedRun).toMatchObject({
        connectionId: 'conn-1',
        mappingId: '',
        direction: 'EXTERNAL_SYNC_DIRECTION_UNSPECIFIED',
      })
    })
  })

  it('creates a connection from the empty state and refreshes the workspace', async () => {
    let createdConnection: unknown
    let connections: unknown[] = []
    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: connections.length,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: '',
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({ connections }),
      ),
      http.post('/fb/v1/console/external-sync/connections', async ({ request }) => {
        createdConnection = await request.json()
        const connection = {
          id: 'conn-new',
          tenantId: 'tenant-1',
          provider: 'github',
          name: 'GitHub OSS',
          enabled: true,
          status: 'active',
          authType: 'token',
          baseUrl: 'https://api.github.com',
          providerConfigJson: '{"repo_url":"https://github.com/acme/app"}',
          scopes: ['issues', 'pull', 'triage'],
          lastTestedAt: '',
          lastTestStatus: 'ok',
          lastError: '',
          createdBy: 'admin',
          updatedBy: 'admin',
          createdAt: '2026-07-08T03:00:00Z',
          updatedAt: '2026-07-08T03:00:00Z',
          webhookSecretConfigured: true,
        }
        connections = [connection]
        return HttpResponse.json(connection, { status: 201 })
      }),
      http.get('/fb/v1/console/external-sync/connections/conn-new/schema', () =>
        HttpResponse.json({ schemas: [] }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', () => HttpResponse.json({ mappings: [] })),
      http.get('/fb/v1/console/external-sync/runs', () => HttpResponse.json({ runs: [] })),
      http.get('/fb/v1/console/external-sync/events', () => HttpResponse.json({ events: [] })),
    )

    const { user } = renderWithProviders(<ExternalSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('暂无外部连接')).toBeInTheDocument()
    })
    expect(screen.getByText('选择一个连接后查看映射。')).toBeInTheDocument()
    expect(screen.getByText('暂无同步任务')).toBeInTheDocument()
    expect(screen.getByText('暂无外部事件')).toBeInTheDocument()
    expect(screen.getByText('未选择运行')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新建连接' }))
    const dialog = screen.getByRole('dialog', { name: '新建外部连接' })
    await user.click(within(dialog).getByRole('combobox', { name: 'Provider' }))
    await user.click(screen.getByRole('option', { name: 'GitHub' }))
    fireEvent.change(within(dialog).getByLabelText('名称'), {
      target: { value: ' GitHub OSS ' },
    })
    fireEvent.change(within(dialog).getByLabelText('Base URL'), {
      target: { value: ' https://api.github.com ' },
    })
    fireEvent.change(within(dialog).getByLabelText('凭据'), {
      target: { value: ' ghp_secret_token ' },
    })
    fireEvent.change(within(dialog).getByLabelText('Webhook secret'), {
      target: { value: ' hook-secret-123456 ' },
    })
    fireEvent.change(within(dialog).getByLabelText('配置 JSON'), {
      target: { value: ' {"repo_url":"https://github.com/acme/app"} ' },
    })
    fireEvent.change(within(dialog).getByLabelText('Scopes'), {
      target: { value: ' issues, pull, , triage ' },
    })

    await user.click(within(dialog).getByRole('button', { name: '新建' }))

    await waitFor(() => {
      expect(createdConnection).toMatchObject({
        provider: 'github',
        name: 'GitHub OSS',
        authType: 'token',
        credential: 'ghp_secret_token',
        webhookSecret: 'hook-secret-123456',
        baseUrl: 'https://api.github.com',
        providerConfigJson: '{"repo_url":"https://github.com/acme/app"}',
        scopes: ['issues', 'pull', 'triage'],
        enabled: true,
      })
    })
    await waitFor(() => {
      expect(screen.getByText('GitHub OSS')).toBeInTheDocument()
    })
    expect(toast.success).toHaveBeenCalledWith('外部连接已创建')
    expect(screen.getByText('暂无映射')).toBeInTheDocument()
  })

  it('surfaces connection qualification and mapping preview problems', async () => {
    let previewCalls = 0
    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 1,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: 'not-a-date',
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: [
            {
              id: 'conn-1',
              tenantId: 'tenant-1',
              provider: 'github',
              name: 'GitHub Needs Attention',
              enabled: true,
              status: 'degraded',
              authType: 'token',
              baseUrl: '',
              providerConfigJson: '{}',
              scopes: ['issues'],
              lastTestedAt: '',
              lastTestStatus: 'failed',
              lastError: 'previous credential check failed',
              createdBy: 'admin',
              updatedBy: 'admin',
              createdAt: 'bad-date',
              updatedAt: 'bad-date',
              webhookSecretConfigured: false,
            },
          ],
        }),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/connections\/conn-1:test$/, () =>
        HttpResponse.json({
          ok: false,
          status: 401,
          latencyMs: 9,
          error: 'bad credentials',
        }),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/connections\/conn-1:qualify$/, () =>
        HttpResponse.json({
          connectionId: 'conn-1',
          ready: false,
          checks: [
            {
              name: 'provider_check',
              status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK',
              summary: 'Provider reached',
              detailJson: '{}',
            },
            {
              name: 'webhook_secret',
              status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_WARNING',
              summary: 'Missing webhook secret',
              detailJson: '{}',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections/conn-1/schema', () =>
        HttpResponse.json({
          schemas: [
            {
              type: 'issue',
              fields: ['title', 'state'],
              requiredFields: ['title'],
              writableFields: ['title'],
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', () =>
        HttpResponse.json({
          mappings: [
            {
              id: 'mapping-1',
              tenantId: 'tenant-1',
              connectionId: 'conn-1',
              localObjectType: 'customer_request',
              externalObjectType: 'issue',
              direction: 'EXTERNAL_SYNC_DIRECTION_PUSH',
              fieldMappingJson: '{"title":"title"}',
              statusMappingJson: '{"planned":"state"}',
              conflictPolicy: 'manual',
              tombstonePolicy: 'mark_stale',
              enabled: true,
              mappingVersion: 2,
              createdAt: 'bad-date',
              updatedAt: 'bad-date',
            },
          ],
        }),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/mappings\/mapping-1:preview$/, () => {
        previewCalls += 1
        if (previewCalls === 1) {
          return HttpResponse.json({
            schema: {
              type: 'issue',
              fields: ['title', 'state'],
              requiredFields: ['title'],
              writableFields: ['title'],
            },
            errors: [],
            warnings: ['state mapping will be normalized'],
          })
        }
        return HttpResponse.json({
          schema: {
            type: 'issue',
            fields: ['title', 'state'],
            requiredFields: ['title'],
            writableFields: ['title'],
          },
          errors: ['title is required'],
          warnings: [],
        })
      }),
      http.get('/fb/v1/console/external-sync/runs', () => HttpResponse.json({ runs: [] })),
      http.get('/fb/v1/console/external-sync/events', () => HttpResponse.json({ events: [] })),
    )

    const { user } = renderWithProviders(<ExternalSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('GitHub Needs Attention')).toBeInTheDocument()
    })
    expect(screen.getByText('previous credential check failed')).toBeInTheDocument()
    expect(screen.getByTitle('not-a-date')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: '回填前重置游标' })).toBeDisabled()

    await user.click(screen.getByLabelText('测试连接'))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('bad credentials')
    })

    await waitFor(() => {
      expect(screen.getAllByLabelText('资格检查')[0]).toBeEnabled()
    })
    fireEvent.click(screen.getAllByLabelText('资格检查')[0])
    await waitFor(() => {
      expect(toast.warning).toHaveBeenCalledWith('连接资格检查需要关注', {
        description: 'Missing webhook secret',
      })
    })

    await user.click(screen.getByRole('button', { name: '预检 issue 映射' }))
    await waitFor(() => {
      expect(toast.warning).toHaveBeenCalledWith('映射预检发现 1 个警告', {
        description: 'state mapping will be normalized',
      })
    })
    await user.click(screen.getByRole('button', { name: '预检 issue 映射' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('映射预检发现 1 个错误', {
        description: 'title is required',
      })
    })
  })

  it('surfaces command failures and keeps destructive success state consistent', async () => {
    const apiFailure = (message: string) =>
      HttpResponse.json({ code: 'external_sync_test', message }, { status: 500 })
    let deleted = false
    let deleteAttempts = 0
    let testAttempts = 0
    let runDetailRequests = 0
    const connection = {
      id: 'conn-1',
      tenantId: 'tenant-1',
      provider: 'github',
      name: 'GitHub Active',
      enabled: true,
      status: 'active',
      authType: 'token',
      baseUrl: '',
      providerConfigJson: '{}',
      scopes: ['issues'],
      lastTestedAt: '',
      lastTestStatus: 'ok',
      lastError: '',
      createdBy: 'admin',
      updatedBy: 'admin',
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T01:00:00Z',
      webhookSecretConfigured: true,
    }
    const pausedConnection = {
      ...connection,
      id: 'conn-2',
      name: 'GitHub Paused',
      enabled: false,
      status: 'quarantined',
      webhookSecretConfigured: false,
    }
    const mapping = {
      id: 'mapping-1',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      localObjectType: 'customer_request',
      externalObjectType: 'issue',
      direction: 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
      fieldMappingJson: '{"title":"title"}',
      statusMappingJson: '{}',
      conflictPolicy: 'manual',
      tombstonePolicy: 'mark_stale',
      enabled: true,
      mappingVersion: 1,
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T01:00:00Z',
    }
    const run = {
      id: 'run-error',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      mappingId: 'mapping-1',
      direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
      trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
      status: 'EXTERNAL_SYNC_RUN_STATUS_FAILED',
      attempts: 1,
      nextRetryAt: '',
      startedAt: '2026-07-08T02:00:00Z',
      finishedAt: '2026-07-08T02:01:00Z',
      cursorBeforeJson: '{}',
      cursorAfterJson: '{}',
      recordsSeen: 1,
      recordsChanged: 0,
      recordsFailed: 1,
      conflictsCreated: 1,
      errorKind: 'validation',
      errorMessage: 'bad record',
      actorId: 'admin',
      createdAt: '2026-07-08T02:00:00Z',
      updatedAt: '2026-07-08T02:01:00Z',
      inFlight: false,
    }
    const event = {
      id: 'event-1',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      mappingId: 'mapping-1',
      provider: 'github',
      eventType: 'issues',
      externalEventId: 'delivery-1',
      dedupeKey: 'github:issues:delivery-1',
      signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED',
      status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
      payloadDigest: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      normalizedPayloadJson: '{}',
      receivedAt: '2026-07-08T02:05:00Z',
      replayedAt: '',
      replayedBy: '',
      runId: '',
      failureReason: '',
      createdAt: '2026-07-08T02:05:00Z',
      updatedAt: '2026-07-08T02:05:00Z',
    }

    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: deleted ? 0 : 1,
          failingConnections: 1,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 1,
          deadRuns: 0,
          openConflicts: 1,
          newestSuccessfulRunAt: '',
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: deleted ? [] : [connection, pausedConnection],
        }),
      ),
      http.get(/\/fb\/v1\/console\/external-sync\/connections\/[^/]+\/schema$/, () =>
        HttpResponse.json({
          schemas: [
            {
              type: 'issue',
              fields: ['title', 'state'],
              requiredFields: ['title'],
              writableFields: ['title', 'state'],
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', ({ request }) => {
        const connectionID = new URL(request.url).searchParams.get('connection_id')
        return HttpResponse.json({ mappings: connectionID === 'conn-2' ? [] : [mapping] })
      }),
      http.get('/fb/v1/console/external-sync/runs', ({ request }) => {
        const beforeID = new URL(request.url).searchParams.get('before_id')
        return HttpResponse.json({
          runs: beforeID
            ? [{ ...run, id: 'run-older', status: 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED' }]
            : [run],
          nextBeforeId: beforeID ? '' : 'run-error',
        })
      }),
      http.get('/fb/v1/console/external-sync/runs/run-error', () => {
        runDetailRequests += 1
        return HttpResponse.json({
          ...runDetailResponse('run-error', run),
          failures: [
            {
              id: 'failure-1',
              tenantId: 'tenant-1',
              runId: 'run-error',
              mappingId: 'mapping-1',
              operation: 'pull',
              localObjectId: 'cr-1',
              externalKey: 'issue-1',
              failureKind: 'validation',
              message: 'invalid issue',
              payloadDigest: 'sha256:test',
              retryMode: 'refetch',
              normalizedPayloadJson: '{}',
              retryable: true,
              resolvedAt: '',
              resolvedBy: '',
              createdAt: '2026-07-08T02:01:00Z',
            },
          ],
          conflicts: [
            {
              id: 'conflict-1',
              tenantId: 'tenant-1',
              runId: 'run-error',
              mappingId: 'mapping-1',
              localObjectId: 'cr-1',
              externalKey: 'issue-1',
              conflictKind: 'field',
              status: 'open',
              resolution: '',
              localSnapshotJson: '{}',
              externalSnapshotJson: '{}',
              resolutionJson: '',
              resolvedBy: '',
              resolvedAt: '',
              createdAt: '2026-07-08T02:01:00Z',
            },
            {
              id: 'conflict-2',
              tenantId: 'tenant-1',
              runId: 'run-error',
              mappingId: 'mapping-1',
              localObjectId: 'cr-2',
              externalKey: 'issue-2',
              conflictKind: 'field',
              status: 'open',
              resolution: '',
              localSnapshotJson: '{}',
              externalSnapshotJson: '{}',
              resolutionJson: '',
              resolvedBy: '',
              resolvedAt: '',
              createdAt: '2026-07-08T02:02:00Z',
            },
          ],
        })
      }),
      http.get('/fb/v1/console/external-sync/events', ({ request }) => {
        const beforeID = new URL(request.url).searchParams.get('before_id')
        return HttpResponse.json({
          events: beforeID ? [{ ...event, id: 'event-older' }] : [event],
          nextBeforeId: beforeID ? '' : 'event-1',
        })
      }),
      http.get('/fb/v1/console/external-sync/events/event-1', () =>
        HttpResponse.json({
          ...event,
          normalizedPayloadJson: '{"action":"opened"}',
        }),
      ),
      http.post('/fb/v1/console/external-sync/connections', () => apiFailure('create failed')),
      http.patch('/fb/v1/console/external-sync/connections/conn-1', () =>
        apiFailure('update failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/connections\/conn-1:test$/, () => {
        testAttempts += 1
        if (testAttempts === 1) return HttpResponse.json({ ok: true, latencyMs: 18, error: '' })
        return apiFailure('test failed')
      }),
      http.post(/\/fb\/v1\/console\/external-sync\/connections\/conn-1:qualify$/, () =>
        apiFailure('qualify failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/connections\/conn-2:resume$/, () =>
        apiFailure('resume failed'),
      ),
      http.delete('/fb/v1/console/external-sync/connections/conn-1', () => {
        deleteAttempts += 1
        if (deleteAttempts === 1) return apiFailure('delete failed')
        deleted = true
        return new HttpResponse(null, { status: 204 })
      }),
      http.put('/fb/v1/console/external-sync/mappings/mapping-1', () =>
        apiFailure('mapping failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/mappings\/mapping-1:preview$/, () =>
        apiFailure('preview failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/mappings\/mapping-1:reset-cursor$/, () =>
        apiFailure('reset failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/mappings\/mapping-1:backfill$/, () =>
        apiFailure('backfill failed'),
      ),
      http.post('/fb/v1/console/external-sync/runs', () => apiFailure('run failed')),
      http.post(/\/fb\/v1\/console\/external-sync\/runs\/run-error:retry$/, () =>
        apiFailure('retry run failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/failures\/failure-1:retry$/, () =>
        apiFailure('retry failure failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/conflicts\/conflict-1:resolve$/, () =>
        apiFailure('resolve failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/conflicts:batch-resolve$/, () =>
        apiFailure('batch resolve failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/events\/event-1:replay$/, () =>
        apiFailure('replay failed'),
      ),
      http.post(/\/fb\/v1\/console\/external-sync\/records:timeline$/, () =>
        apiFailure('timeline failed'),
      ),
    )

    const { user } = renderWithProviders(<ExternalSyncPage />)

    await waitFor(() => {
      expect(screen.getByText('GitHub Active')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '新建连接' }))
    const createDialog = screen.getByRole('dialog', { name: '新建外部连接' })
    fireEvent.change(within(createDialog).getByLabelText('名称'), {
      target: { value: 'GitHub Failing' },
    })
    fireEvent.change(within(createDialog).getByLabelText('凭据'), {
      target: { value: 'token' },
    })
    await user.click(within(createDialog).getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('create failed')
    })
    await user.click(within(createDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByText('GitHub Paused'))
    await user.click(screen.getByText('GitHub Active'))

    await user.click(screen.getByLabelText('恢复连接'))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('resume failed')
    })

    await user.click(screen.getAllByLabelText('编辑')[0])
    const editDialog = screen.getByRole('dialog', { name: '编辑外部连接' })
    fireEvent.change(within(editDialog).getByLabelText('名称'), {
      target: { value: 'GitHub Broken' },
    })
    await user.click(within(editDialog).getByRole('button', { name: '保存' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('update failed')
    })
    await user.click(within(editDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getAllByLabelText('测试连接')[0])
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith('连接测试通过，耗时 18ms')
    })
    await user.click(screen.getAllByLabelText('测试连接')[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('test failed')
    })
    await user.click(screen.getAllByLabelText('资格检查')[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('qualify failed')
    })
    await user.click(screen.getAllByLabelText('请求同步')[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('run failed')
    })

    await user.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('mapping failed')
    })
    await user.click(screen.getByRole('button', { name: '预检 issue 映射' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('preview failed')
    })
    await user.click(screen.getByRole('button', { name: '重置 issue 同步游标' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('reset failed')
    })
    await user.click(screen.getByRole('button', { name: '回填 issue 记录' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('backfill failed')
    })

    const failedRun = screen
      .getAllByRole('button')
      .find((button) => button.textContent?.includes('见 1'))
    expect(failedRun).toBeDefined()
    await user.click(failedRun as HTMLButtonElement)
    await waitFor(() => {
      expect(runDetailRequests).toBeGreaterThan(0)
    })
    await waitFor(() => {
      expect(screen.getByText('运行 run-erro 的 attempts、失败记录和冲突。')).toBeInTheDocument()
    })

    await user.click(screen.getAllByRole('button', { name: '加载更多' })[1])
    await user.click(screen.getAllByRole('button', { name: '重试' })[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('retry run failed')
    })
    await user.click(screen.getAllByRole('button', { name: '重试' }).at(-1) as HTMLButtonElement)
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('retry failure failed')
    })
    await user.click(screen.getAllByRole('button', { name: '时间线' })[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('timeline failed')
    })
    await user.click(screen.getAllByRole('button', { name: '时间线' }).at(-1) as HTMLButtonElement)
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('timeline failed')
    })
    await user.click(screen.getAllByRole('button', { name: '处理冲突' })[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('resolve failed')
    })

    await user.click(screen.getByLabelText('查看事件 event-1'))
    await user.click(screen.getAllByRole('button', { name: '重放' })[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('replay failed')
    })
    await user.click(screen.getByRole('button', { name: '批量处理' }))
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('batch resolve failed')
    })

    await user.click(screen.getAllByLabelText('删除')[0])
    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('delete failed')
    })
    await user.click(screen.getAllByLabelText('删除')[0])
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith('外部连接已删除')
    })
    await waitFor(() => {
      expect(screen.getByText('暂无外部连接')).toBeInTheDocument()
    })
  })

  it('refreshes active runs until the worker status settles', async () => {
    let runCalls = 0
    const runRow = (status: string) => ({
      id: 'run-active-1',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      mappingId: 'mapping-1',
      direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
      trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
      status,
      attempts: status === 'EXTERNAL_SYNC_RUN_STATUS_QUEUED' ? 0 : 1,
      nextRetryAt: '',
      startedAt: '2026-07-08T02:00:00Z',
      finishedAt: status === 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED' ? '2026-07-08T02:01:00Z' : '',
      cursorBeforeJson: '{}',
      cursorAfterJson: '{}',
      recordsSeen: status === 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED' ? 2 : 0,
      recordsChanged: status === 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED' ? 2 : 0,
      recordsFailed: 0,
      conflictsCreated: 0,
      errorKind: '',
      errorMessage: '',
      actorId: 'admin',
      createdAt: '2026-07-08T02:00:00Z',
      updatedAt: '2026-07-08T02:01:00Z',
      inFlight: false,
    })

    server.use(
      http.get('/fb/v1/console/external-sync/health', () =>
        HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: runCalls <= 1 ? 1 : 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: '',
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: [
            {
              id: 'conn-1',
              tenantId: 'tenant-1',
              provider: 'github',
              name: 'GitHub Prod',
              enabled: true,
              status: 'active',
              authType: 'token',
              baseUrl: '',
              providerConfigJson: '{}',
              scopes: ['issues'],
              lastTestedAt: '',
              lastTestStatus: 'ok',
              lastError: '',
              createdBy: 'admin',
              updatedBy: 'admin',
              createdAt: '2026-07-08T01:00:00Z',
              updatedAt: '2026-07-08T01:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/connections/conn-1/schema', () =>
        HttpResponse.json({ schemas: [] }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', () => HttpResponse.json({ mappings: [] })),
      http.get('/fb/v1/console/external-sync/events', () => HttpResponse.json({ events: [] })),
      http.get('/fb/v1/console/external-sync/runs', () => {
        runCalls += 1
        return HttpResponse.json({
          runs: [
            runRow(
              runCalls <= 2
                ? 'EXTERNAL_SYNC_RUN_STATUS_QUEUED'
                : 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED',
            ),
          ],
          nextBeforeId: '',
        })
      }),
      http.get('/fb/v1/console/external-sync/runs/run-active-1', () =>
        HttpResponse.json(
          runDetailResponse(
            'run-active-1',
            runRow(
              runCalls <= 2
                ? 'EXTERNAL_SYNC_RUN_STATUS_QUEUED'
                : 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED',
            ),
          ),
        ),
      ),
    )

    renderWithProviders(<ExternalSyncPage />)

    await waitFor(
      () => {
        expect(screen.getByText('queued')).toBeInTheDocument()
      },
      { timeout: 5_500 },
    )
    await waitFor(
      () => {
        expect(runCalls).toBeGreaterThan(1)
        expect(screen.getByText('succeeded')).toBeInTheDocument()
        expect(screen.getByText('见 2')).toBeInTheDocument()
      },
      { timeout: 7_500 },
    )
  })

  it('edits mappings and dispatches mapping actions from the focused editor', async () => {
    const onSave = vi.fn()
    const onPreview = vi.fn()
    const onResetCursor = vi.fn()
    const onBackfill = vi.fn()
    const mapping = {
      id: 'mapping-focused',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      localObjectType: 'customer_request',
      externalObjectType: 'issue',
      direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
      fieldMappingJson: '{"title":"title"}',
      statusMappingJson: '{}',
      conflictPolicy: 'manual',
      tombstonePolicy: 'mark_stale',
      enabled: true,
      mappingVersion: 3,
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T02:00:00Z',
    }
    const { user } = renderWithProviders(
      <MappingEditor
        mapping={mapping as never}
        schemas={[
          {
            type: 'issue',
            fields: ['title', 'state', 'labels'],
            requiredFields: ['title'],
            writableFields: ['title', 'state'],
          },
        ]}
        pending={false}
        previewing={false}
        resetting={false}
        backfilling={false}
        onSave={onSave}
        onPreview={onPreview}
        onResetCursor={onResetCursor}
        onBackfill={onBackfill}
      />,
    )

    fireEvent.change(screen.getByLabelText('字段映射 JSON'), { target: { value: '[' } })
    expect(screen.getByText('字段映射 JSON 必须是 JSON 对象')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('字段映射 JSON'), {
      target: { value: ' {"title":"headline"} ' },
    })
    fireEvent.change(screen.getByLabelText('状态映射 JSON'), {
      target: { value: '[]' },
    })
    expect(screen.getByText('状态映射 JSON 必须是 JSON 对象')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('状态映射 JSON'), {
      target: { value: '   ' },
    })
    expect(screen.queryByText('状态映射 JSON 必须是 JSON 对象')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('状态映射 JSON'), {
      target: { value: '{"planned":"state"}' },
    })
    fireEvent.change(screen.getByLabelText('冲突策略'), { target: { value: 'external_wins' } })
    fireEvent.change(screen.getByLabelText('删除策略'), { target: { value: 'unlink' } })
    expect(screen.getByText('未匹配 provider schema：headline')).toBeInTheDocument()

    await user.click(screen.getByRole('combobox', { name: '方向' }))
    await user.click(screen.getByRole('option', { name: '双向' }))
    await user.click(screen.getByRole('checkbox', { name: '启用' }))
    await user.click(screen.getByRole('checkbox', { name: '启用' }))
    await user.click(screen.getByRole('checkbox', { name: '回填前重置游标' }))
    await user.click(screen.getByRole('button', { name: '预检 issue 映射' }))
    await user.click(screen.getByRole('button', { name: '重置 issue 同步游标' }))
    await user.click(screen.getByRole('button', { name: '回填 issue 记录' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    expect(onPreview).toHaveBeenCalledWith(
      'mapping-focused',
      '{"title":"headline"}',
      '{"planned":"state"}',
    )
    expect(onResetCursor).toHaveBeenCalledWith('mapping-focused')
    expect(onBackfill).toHaveBeenCalledWith('mapping-focused', true)
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
        enabled: true,
        fieldMappingJson: '{"title":"headline"}',
        statusMappingJson: '{"planned":"state"}',
        conflictPolicy: 'external_wins',
        tombstonePolicy: 'unlink',
      }),
    )
  })

  it('submits create and edit connection dialogs with normalized payloads', async () => {
    const onCreate = vi.fn()
    const onCreateOpenChange = vi.fn()
    const createRender = renderWithProviders(
      <CreateConnectionDialog
        open={true}
        pending={false}
        providers={[
          { provider: 'github', display: 'GitHub' },
          { provider: 'jira', display: 'Jira' },
        ]}
        onOpenChange={onCreateOpenChange}
        onSubmit={onCreate}
      />,
    )
    const createDialog = screen.getByRole('dialog', { name: '新建外部连接' })
    const createForm = within(createDialog).getByRole('button', { name: '新建' }).closest('form')
    expect(createForm).not.toBeNull()
    fireEvent.submit(createForm as HTMLFormElement)
    expect(onCreate).not.toHaveBeenCalled()

    await createRender.user.click(within(createDialog).getByRole('combobox', { name: 'Provider' }))
    await createRender.user.click(screen.getByRole('option', { name: 'Jira' }))
    fireEvent.change(within(createDialog).getByLabelText('名称'), {
      target: { value: ' GitHub App ' },
    })
    await createRender.user.click(within(createDialog).getByRole('combobox', { name: '认证类型' }))
    await createRender.user.click(screen.getByRole('option', { name: 'api_key' }))
    fireEvent.change(within(createDialog).getByLabelText('Base URL'), {
      target: { value: ' https://github.example.test/api/v3 ' },
    })
    fireEvent.change(within(createDialog).getByLabelText('凭据'), {
      target: { value: ' token-1 ' },
    })
    fireEvent.change(within(createDialog).getByLabelText('Webhook secret'), {
      target: { value: ' webhook-secret ' },
    })
    fireEvent.change(within(createDialog).getByLabelText('配置 JSON'), {
      target: { value: ' ' },
    })
    fireEvent.change(within(createDialog).getByLabelText('Scopes'), {
      target: { value: ' issues, pull,  ' },
    })
    await createRender.user.click(within(createDialog).getByRole('checkbox', { name: '启用连接' }))
    await createRender.user.click(within(createDialog).getByRole('button', { name: '新建' }))

    expect(onCreate).toHaveBeenCalledWith({
      provider: 'jira',
      name: 'GitHub App',
      authType: 'api_key',
      credential: 'token-1',
      webhookSecret: 'webhook-secret',
      baseUrl: 'https://github.example.test/api/v3',
      providerConfigJson: '{}',
      providerInstallationId: '',
      scopes: ['issues', 'pull'],
      enabled: false,
    })

    await createRender.user.keyboard('{Escape}')
    expect(onCreateOpenChange).toHaveBeenCalledWith(false)
    createRender.unmount()

    const onEdit = vi.fn()
    const onEditOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <EditConnectionDialog
        open={true}
        pending={false}
        connection={
          {
            id: 'conn-1',
            tenantId: 'tenant-1',
            provider: 'github',
            name: 'GitHub Prod',
            enabled: true,
            status: 'active',
            authType: 'token',
            baseUrl: 'https://api.github.com',
            providerConfigJson: '{"repo":"acme/app"}',
            scopes: ['issues'],
            lastTestedAt: '',
            lastTestStatus: 'ok',
            lastError: '',
            createdBy: 'admin',
            updatedBy: 'admin',
            createdAt: '2026-07-08T01:00:00Z',
            updatedAt: '2026-07-08T02:00:00Z',
            webhookSecretConfigured: true,
          } as never
        }
        onOpenChange={onEditOpenChange}
        onSubmit={onEdit}
      />,
    )
    const editDialog = screen.getByRole('dialog', { name: '编辑外部连接' })
    fireEvent.change(within(editDialog).getByLabelText('名称'), {
      target: { value: ' ' },
    })
    const editForm = within(editDialog).getByRole('button', { name: '保存' }).closest('form')
    expect(editForm).not.toBeNull()
    fireEvent.submit(editForm as HTMLFormElement)
    expect(onEdit).not.toHaveBeenCalled()

    fireEvent.change(within(editDialog).getByLabelText('名称'), {
      target: { value: ' GitHub Enterprise ' },
    })
    fireEvent.change(within(editDialog).getByLabelText('Base URL'), {
      target: { value: ' https://github.example.test/api/v3 ' },
    })
    fireEvent.change(within(editDialog).getByLabelText('凭据轮换'), {
      target: { value: ' token-2 ' },
    })
    fireEvent.change(within(editDialog).getByLabelText('Webhook secret 轮换'), {
      target: { value: ' webhook-secret-2 ' },
    })
    fireEvent.change(within(editDialog).getByLabelText('配置 JSON'), {
      target: { value: ' {"repo":"acme/prod"} ' },
    })
    fireEvent.change(within(editDialog).getByLabelText('Scopes'), {
      target: { value: ' issues, admin ' },
    })
    await user.click(within(editDialog).getByRole('checkbox', { name: '启用连接' }))
    await user.click(within(editDialog).getByRole('button', { name: '保存' }))
    await user.click(within(editDialog).getByRole('button', { name: '取消' }))

    expect(onEdit).toHaveBeenCalledWith({
      id: 'conn-1',
      name: 'GitHub Enterprise',
      enabled: false,
      baseUrl: 'https://github.example.test/api/v3',
      providerConfigJson: '{"repo":"acme/prod"}',
      scopes: ['issues', 'admin'],
      credential: 'token-2',
      webhookSecret: 'webhook-secret-2',
    })
    expect(onEditOpenChange).toHaveBeenCalledWith(false)
  })

  it('changes the batch conflict resolution before resolving', async () => {
    const onResolve = vi.fn()
    const view = renderWithProviders(
      <BatchConflictResolutionControls conflictCount={2} pending={false} onResolve={onResolve} />,
    )

    await view.user.click(screen.getByRole('combobox', { name: '2 个待处理冲突' }))
    await view.user.click(screen.getByRole('option', { name: '本地优先' }))
    await view.user.click(screen.getByRole('button', { name: '批量处理' }))

    expect(onResolve).toHaveBeenCalledWith(
      ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS,
    )
    view.unmount()

    const pendingView = renderWithProviders(
      <BatchConflictResolutionControls conflictCount={3} pending={true} onResolve={vi.fn()} />,
    )
    expect(screen.getByRole('button', { name: '批量处理' })).toBeDisabled()
    expect(pendingView.container.querySelector('.animate-spin')).not.toBeNull()
  })

  it('renders focused card loading, pending, and fallback states', async () => {
    const connection = {
      id: 'conn-card',
      tenantId: 'tenant-1',
      provider: 'github',
      name: 'GitHub Pending',
      enabled: false,
      status: 'quarantined',
      authType: 'token',
      baseUrl: '',
      providerConfigJson: '{}',
      scopes: ['issues'],
      lastTestedAt: '',
      lastTestStatus: '',
      lastError: '',
      createdBy: 'admin',
      updatedBy: 'admin',
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T01:00:00Z',
      webhookSecretConfigured: false,
    }
    const mapping = {
      id: 'mapping-card',
      tenantId: 'tenant-1',
      connectionId: 'conn-card',
      localObjectType: 'customer_request',
      externalObjectType: 'issue',
      direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
      fieldMappingJson: '{}',
      statusMappingJson: '{}',
      conflictPolicy: 'manual',
      tombstonePolicy: 'mark_stale',
      enabled: true,
      mappingVersion: 1,
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T01:00:00Z',
    }
    const run = {
      id: 'run-card',
      tenantId: 'tenant-1',
      connectionId: 'conn-card',
      mappingId: 'mapping-card',
      direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
      trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
      status: 'dead',
      attempts: 2,
      nextRetryAt: '',
      startedAt: '2026-07-08T02:00:00Z',
      finishedAt: '2026-07-08T02:01:00Z',
      cursorBeforeJson: '{}',
      cursorAfterJson: '{}',
      recordsSeen: 4,
      recordsChanged: 1,
      recordsFailed: 1,
      conflictsCreated: 0,
      errorKind: 'provider',
      errorMessage: 'provider failed',
      actorId: 'admin',
      createdAt: '2026-07-08T02:00:00Z',
      updatedAt: '2026-07-08T02:01:00Z',
      inFlight: false,
    }
    const event = {
      id: 'event-card',
      tenantId: 'tenant-1',
      connectionId: 'conn-card',
      mappingId: 'mapping-card',
      provider: 'github',
      eventType: '',
      externalEventId: '',
      dedupeKey: 'github:delivery',
      signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_NOT_REQUIRED',
      status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
      payloadDigest: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      normalizedPayloadJson: '{}',
      receivedAt: '2026-07-08T02:05:00Z',
      replayedAt: '',
      replayedBy: '',
      runId: 'run-card-abcdef',
      failureReason: 'signature mismatch',
      createdAt: '2026-07-08T02:05:00Z',
      updatedAt: '2026-07-08T02:05:00Z',
    }

    let view = renderWithProviders(
      <ConnectionsCard
        connections={[]}
        loading={true}
        selectedID=""
        requesting={false}
        selectedMapping={null}
        onSelect={vi.fn()}
        onEdit={vi.fn()}
        onTest={vi.fn()}
        onResume={vi.fn()}
        onQualify={vi.fn()}
        onDelete={vi.fn()}
        onRun={vi.fn()}
      />,
    )
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    view = renderWithProviders(
      <ConnectionsCard
        connections={[connection as never]}
        loading={false}
        selectedID="other-connection"
        testingID="conn-card"
        deletingID="conn-card"
        updatingID="conn-card"
        resumingID="conn-card"
        qualifyingID="conn-card"
        requesting={true}
        selectedMapping={mapping as never}
        onSelect={vi.fn()}
        onEdit={vi.fn()}
        onTest={vi.fn()}
        onResume={vi.fn()}
        onQualify={vi.fn()}
        onDelete={vi.fn()}
        onRun={vi.fn()}
      />,
    )
    expect(screen.getByText('untested')).toBeInTheDocument()
    expect(screen.queryByText('Webhook secret 已配置')).not.toBeInTheDocument()
    expect(screen.getByLabelText('请求同步')).toBeDisabled()
    expect(view.container.querySelectorAll('.animate-spin').length).toBeGreaterThanOrEqual(5)
    view.unmount()

    view = renderWithProviders(
      <RunsCard
        runs={[]}
        loading={true}
        selectedID=""
        hasNextPage={false}
        loadingMore={false}
        onSelect={vi.fn()}
        onRetry={vi.fn()}
        onLoadMore={vi.fn()}
      />,
    )
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    view = renderWithProviders(
      <RunsCard
        runs={[run as never]}
        loading={false}
        selectedID="other-run"
        retryingID="run-card"
        hasNextPage={true}
        loadingMore={true}
        onSelect={vi.fn()}
        onRetry={vi.fn()}
        onLoadMore={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '重试' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '加载更多' })).toBeDisabled()
    expect(view.container.querySelectorAll('.animate-spin').length).toBeGreaterThanOrEqual(2)
    view.unmount()

    view = renderWithProviders(
      <EventsCard
        events={[]}
        loading={true}
        selectedID=""
        hasNextPage={false}
        loadingMore={false}
        onSelect={vi.fn()}
        onReplay={vi.fn()}
        onLoadMore={vi.fn()}
      />,
    )
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    view = renderWithProviders(
      <EventsCard
        events={[event as never]}
        loading={false}
        selectedID="other-event"
        replayingID="event-card"
        hasNextPage={true}
        loadingMore={true}
        onSelect={vi.fn()}
        onReplay={vi.fn()}
        onLoadMore={vi.fn()}
      />,
    )
    expect(screen.getByText('signature mismatch')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重放' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '加载更多' })).toBeDisabled()
    expect(view.container.querySelectorAll('.animate-spin').length).toBeGreaterThanOrEqual(2)
    view.unmount()
  })

  it('renders focused detail components across empty and edge states', async () => {
    const target = {
      mappingId: 'mapping-card',
      localObjectId: '',
      externalKey: 'issue-9',
      label: 'issue-9',
    }
    let view = renderWithProviders(<EventDetailCard event={undefined} loading={true} />)
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    view = renderWithProviders(<EventDetailCard event={undefined} loading={false} />)
    expect(screen.getByText('未选择事件')).toBeInTheDocument()
    view.unmount()

    view = renderWithProviders(
      <EventDetailCard
        loading={false}
        event={
          {
            id: 'event-detail',
            tenantId: 'tenant-1',
            connectionId: 'conn-card',
            mappingId: 'mapping-card',
            provider: 'github',
            eventType: 'issues',
            externalEventId: 'delivery-1',
            dedupeKey: 'github:delivery-1',
            signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED',
            status: 'EXTERNAL_SYNC_EVENT_STATUS_FAILED',
            payloadDigest: 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
            normalizedPayloadJson: 'bad-json',
            receivedAt: 'not-a-date',
            replayedAt: '',
            replayedBy: '',
            runId: '',
            failureReason: 'signature mismatch',
            createdAt: '2026-07-08T02:05:00Z',
            updatedAt: '2026-07-08T02:05:00Z',
          } as never
        }
      />,
    )
    expect(screen.getByText('signature mismatch')).toBeInTheDocument()
    expect(screen.getByText('bad-json')).toBeInTheDocument()
    view.unmount()

    view = renderWithProviders(
      <RunDetailCard
        detail={undefined}
        loading={true}
        batchResolving={false}
        timelineEntries={[]}
        timelineLoading={false}
        timelineTarget={null}
        onRetryFailure={vi.fn()}
        onShowTimeline={vi.fn()}
        onResolveConflict={vi.fn()}
        onBatchResolveConflicts={vi.fn()}
      />,
    )
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    const run = {
      id: 'run-edge',
      tenantId: 'tenant-1',
      connectionId: 'conn-card',
      mappingId: 'mapping-card',
      direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
      trigger: 'EXTERNAL_SYNC_RUN_TRIGGER_MANUAL',
      status: 'EXTERNAL_SYNC_RUN_STATUS_FAILED',
      attempts: 1,
      nextRetryAt: '',
      startedAt: '',
      finishedAt: '',
      cursorBeforeJson: '',
      cursorAfterJson: 'not-json',
      recordsSeen: 1,
      recordsChanged: 0,
      recordsFailed: 1,
      conflictsCreated: 1,
      errorKind: 'validation',
      errorMessage: 'bad record',
      actorId: 'admin',
      createdAt: '2026-07-08T02:00:00Z',
      updatedAt: '2026-07-08T02:01:00Z',
      inFlight: false,
    }
    view = renderWithProviders(
      <RunDetailCard
        detail={
          {
            run,
            attempts: [
              {
                id: 'attempt-edge',
                runId: 'run-edge',
                attemptNumber: 1,
                startedAt: 'bad-date',
                finishedAt: '',
                result: 'failed',
                httpStatus: 0,
                providerRequestId: '',
                retryAfter: '',
                errorKind: '',
                errorMessage: '',
              },
            ],
            failures: [
              {
                id: 'failure-edge',
                tenantId: 'tenant-1',
                runId: 'run-edge',
                mappingId: 'mapping-card',
                operation: 'pull',
                localObjectId: '',
                externalKey: '',
                failureKind: 'validation',
                message: '',
                payloadDigest: '',
                retryMode: '',
                normalizedPayloadJson: '',
                retryable: false,
                resolvedAt: '',
                resolvedBy: '',
                createdAt: '2026-07-08T02:01:00Z',
              },
            ],
            conflicts: [
              {
                id: 'conflict-edge',
                tenantId: 'tenant-1',
                mappingId: 'mapping-card',
                localObjectId: '',
                externalKey: '',
                conflictKind: 'field',
                status: 'resolved',
                localSnapshotJson: '',
                externalSnapshotJson: '',
                resolution: 'ignored',
                resolvedAt: '2026-07-08T02:02:00Z',
                resolvedBy: 'admin',
                createdAt: '2026-07-08T02:01:00Z',
                updatedAt: '2026-07-08T02:02:00Z',
              },
            ],
          } as never
        }
        loading={false}
        retryingFailureID="failure-edge"
        resolvingConflictID="conflict-edge"
        batchResolving={true}
        timelineEntries={[]}
        timelineLoading={false}
        timelineTarget={null}
        onRetryFailure={vi.fn()}
        onShowTimeline={vi.fn()}
        onResolveConflict={vi.fn()}
        onBatchResolveConflicts={vi.fn()}
      />,
    )
    expect(screen.getAllByText('pull').length).toBeGreaterThan(0)
    expect(screen.getByText('field · resolved')).toBeInTheDocument()
    expect(view.container.querySelectorAll('.animate-spin').length).toBeGreaterThanOrEqual(2)
    view.unmount()

    view = renderWithProviders(
      <RecordTimelinePanel target={target as never} entries={[]} loading={true} />,
    )
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    view = renderWithProviders(
      <RecordTimelinePanel target={target as never} entries={[]} loading={false} />,
    )
    expect(screen.getByText('暂无这条记录的同步事件')).toBeInTheDocument()
    view.unmount()

    view = renderWithProviders(
      <RecordTimelinePanel
        target={target as never}
        loading={false}
        entries={[
          {
            kind: 'failure',
            occurredAt: 'bad-date',
            runId: '',
            status: '',
            operation: '',
            localObjectId: '',
            externalKey: '',
            summary: '',
            detailJson: '',
          } as never,
        ]}
      />,
    )
    expect(screen.getAllByText('failure').length).toBeGreaterThan(0)
    view.unmount()

    const emptyRows = renderWithProviders(
      <DiagnosticRows rows={[{ label: 'empty', value: '   ' }]} />,
    )
    expect(emptyRows.container).toBeEmptyDOMElement()
    emptyRows.unmount()

    renderWithProviders(
      <>
        <StatusPill value="failed" />
        <StatusPill value="running" />
        <StatusPill value="synced" />
      </>,
    )
    expect(screen.getByText('failed')).toBeInTheDocument()
    expect(screen.getByText('running')).toBeInTheDocument()
    expect(screen.getByText('synced')).toBeInTheDocument()
  })

  it('keeps focused dialog and conflict controls stable in pending states', async () => {
    const onCreate = vi.fn()
    let view = renderWithProviders(
      <CreateConnectionDialog
        open={true}
        pending={true}
        onOpenChange={vi.fn()}
        onSubmit={onCreate}
      />,
    )
    expect(screen.getByRole('button', { name: '新建' })).toBeDisabled()
    expect(document.body.querySelector('.animate-spin')).not.toBeNull()
    await view.user.click(screen.getByRole('button', { name: '新建' }))
    expect(onCreate).not.toHaveBeenCalled()
    view.unmount()

    const nullEdit = renderWithProviders(
      <EditConnectionDialog
        open={true}
        pending={false}
        connection={null}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(nullEdit.container).toBeEmptyDOMElement()
    nullEdit.unmount()

    view = renderWithProviders(
      <EditConnectionDialog
        open={true}
        pending={true}
        connection={
          {
            id: 'conn-blank-config',
            tenantId: 'tenant-1',
            provider: 'github',
            name: 'GitHub Blank Config',
            enabled: true,
            status: 'active',
            authType: 'token',
            baseUrl: '',
            providerConfigJson: '',
            scopes: [],
            lastTestedAt: '',
            lastTestStatus: 'ok',
            lastError: '',
            createdBy: 'admin',
            updatedBy: 'admin',
            createdAt: '2026-07-08T01:00:00Z',
            updatedAt: '2026-07-08T02:00:00Z',
            webhookSecretConfigured: false,
          } as never
        }
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
    expect(screen.getByLabelText('配置 JSON')).toHaveValue('{}')
    expect(document.body.querySelector('.animate-spin')).not.toBeNull()
    view.unmount()

    const onResolve = vi.fn()
    view = renderWithProviders(
      <ConflictResolutionControls
        conflictID="conflict-open"
        status="open"
        pending={false}
        onResolve={onResolve}
      />,
    )
    await view.user.click(screen.getByRole('combobox', { name: '处理方式' }))
    await view.user.click(screen.getByRole('option', { name: '手动合并' }))
    await view.user.click(screen.getByRole('button', { name: '处理冲突' }))
    expect(onResolve).toHaveBeenCalledWith(
      ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE,
    )
    view.unmount()

    view = renderWithProviders(
      <ConflictResolutionControls
        conflictID="conflict-resolved"
        status="resolved"
        pending={true}
        onResolve={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '处理冲突' })).toBeDisabled()
    expect(view.container.querySelector('.animate-spin')).not.toBeNull()
  })

  it('computes active action ids from mutation pending state', () => {
    expect(
      activeConnectionActionIDsFromMutations({
        deleting: { isPending: true, variables: 'conn-delete' },
        qualifying: { isPending: true, variables: 'conn-qualify' },
        resuming: { isPending: true, variables: 'conn-resume' },
        testing: { isPending: true, variables: 'conn-test' },
        updating: { isPending: true, variables: { id: 'conn-update' } },
      }),
    ).toEqual({
      deleting: 'conn-delete',
      qualifying: 'conn-qualify',
      resuming: 'conn-resume',
      testing: 'conn-test',
      updating: 'conn-update',
    })
    expect(
      activeConnectionActionIDsFromMutations({
        deleting: { isPending: false, variables: 'conn-delete' },
        qualifying: { isPending: false, variables: 'conn-qualify' },
        resuming: { isPending: false, variables: 'conn-resume' },
        testing: { isPending: false, variables: 'conn-test' },
        updating: { isPending: false, variables: { id: 'conn-update' } },
      }),
    ).toEqual({
      deleting: undefined,
      qualifying: undefined,
      resuming: undefined,
      testing: undefined,
      updating: undefined,
    })

    expect(
      activeMappingActionIDsFromMutations({
        backfilling: { isPending: true, variables: { id: 'mapping-backfill' } },
        previewing: { isPending: true, variables: { id: 'mapping-preview' } },
        resetting: { isPending: true, variables: { id: 'mapping-reset' } },
        saving: { isPending: true, variables: { id: 'mapping-save' } },
      }),
    ).toEqual({
      backfilling: 'mapping-backfill',
      previewing: 'mapping-preview',
      resetting: 'mapping-reset',
      saving: 'mapping-save',
    })
    expect(
      activeMappingActionIDsFromMutations({
        backfilling: { isPending: false, variables: { id: 'mapping-backfill' } },
        previewing: { isPending: false, variables: { id: 'mapping-preview' } },
        resetting: { isPending: false, variables: { id: 'mapping-reset' } },
        saving: { isPending: false, variables: { id: 'mapping-save' } },
      }),
    ).toEqual({
      backfilling: undefined,
      previewing: undefined,
      resetting: undefined,
      saving: undefined,
    })

    expect(
      activeRunActionIDsFromMutations({
        replayingEvent: { isPending: true, variables: 'event-replay' },
        resolvingConflict: { isPending: true, variables: { id: 'conflict-resolve' } },
        retryingFailure: { isPending: true, variables: 'failure-retry' },
        retryingRun: { isPending: true, variables: 'run-retry' },
      }),
    ).toEqual({
      replayingEvent: 'event-replay',
      resolvingConflict: 'conflict-resolve',
      retryingFailure: 'failure-retry',
      retryingRun: 'run-retry',
    })
    expect(
      activeRunActionIDsFromMutations({
        replayingEvent: { isPending: false, variables: 'event-replay' },
        resolvingConflict: { isPending: false, variables: { id: 'conflict-resolve' } },
        retryingFailure: { isPending: false, variables: 'failure-retry' },
        retryingRun: { isPending: false, variables: 'run-retry' },
      }),
    ).toEqual({
      replayingEvent: undefined,
      resolvingConflict: undefined,
      retryingFailure: undefined,
      retryingRun: undefined,
    })
  })

  it('uses mapping editor defaults and pending guards for sparse mappings', async () => {
    const onSave = vi.fn()
    const mapping = {
      id: 'mapping-sparse',
      tenantId: 'tenant-1',
      connectionId: 'conn-1',
      localObjectType: 'customer_request',
      externalObjectType: 'issue',
      direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH,
      fieldMappingJson: '',
      statusMappingJson: '',
      conflictPolicy: '',
      tombstonePolicy: '',
      enabled: false,
      mappingVersion: 1,
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T02:00:00Z',
    }
    const { user } = renderWithProviders(
      <MappingEditor
        mapping={mapping as never}
        schemas={[]}
        pending={true}
        previewing={true}
        resetting={true}
        backfilling={true}
        onSave={onSave}
        onPreview={vi.fn()}
        onResetCursor={vi.fn()}
        onBackfill={vi.fn()}
      />,
    )

    expect(screen.getByLabelText('字段映射 JSON')).toHaveValue('{}')
    expect(screen.getByLabelText('状态映射 JSON')).toHaveValue('{}')
    expect(screen.getByLabelText('冲突策略')).toHaveValue('manual')
    expect(screen.getByLabelText('删除策略')).toHaveValue('mark_stale')
    expect(screen.getByRole('checkbox', { name: '回填前重置游标' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '预检 issue 映射' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重置 issue 同步游标' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '回填 issue 记录' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(onSave).not.toHaveBeenCalled()
  })

  it('keeps external sync helper behavior stable', () => {
    expect(statusLabel('EXTERNAL_SYNC_RUN_STATUS_FAILED')).toBe('failed')
    expect(statusLabel('failed')).toBe('failed')
    expect(eventStatusLabel('EXTERNAL_SYNC_EVENT_STATUS_RECEIVED')).toBe('received')
    expect(eventSignatureLabel('EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_NOT_REQUIRED')).toBe(
      'not_required',
    )
    expect(isRetryableRunStatus('failed')).toBe(true)
    expect(isRetryableRunStatus('EXTERNAL_SYNC_RUN_STATUS_DEAD')).toBe(true)
    expect(isRetryableRunStatus('EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED')).toBe(false)
    expect(directionLabel('EXTERNAL_SYNC_DIRECTION_PUSH')).toBe('push')
    expect(formatDate('')).toBe('')
    expect(formatDate('not-a-date')).toBe('not-a-date')
    expect(formatDate('2026-07-08T02:00:00Z')).toContain('2026')
    expect(capabilityGrade('{"grade":"full_app"}')).toBe('full_app')
    expect(capabilityGrade('{"ready":true}')).toBe('')
    expect(capabilityGrade('not-json')).toBe('')
    expect(shortID('123456789')).toBe('12345678')
    expect(normalizeJSONInput('   ')).toBe('{}')
    expect(normalizeJSONInput(' {"a":1} ')).toBe('{"a":1}')
    const installationResource = {
      id: 'resource-1',
      tenantId: 'tenant-1',
      installationId: 'installation-1',
      provider: 'github',
      resourceType: 'repository',
      externalResourceId: '100',
      resourceKey: 'acme/app.git',
      displayName: 'acme/app',
      htmlUrl: 'https://github.com/acme/app',
      selected: true,
      status: 'active',
      permissionsJson: '{}',
      lastSeenAt: '2026-07-08T02:00:00Z',
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T02:00:00Z',
    }
    expect(providerConfigFromInstallationResources([installationResource])).toBe(
      '{"owner":"acme","repo":"app"}',
    )
    expect(
      providerConfigFromInstallationResources([
        installationResource,
        { ...installationResource, id: 'resource-2', resourceKey: 'acme/other' },
      ]),
    ).toBe('')
    expect(
      providerConfigFromInstallationResources([{ ...installationResource, selected: false }]),
    ).toBe('')
    expect(
      providerConfigFromInstallationResources([
        { ...installationResource, resourceType: 'organization' },
      ]),
    ).toBe('')
    expect(mappingAllowsPull(ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL)).toBe(true)
    expect(mappingAllowsPull(ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL)).toBe(
      true,
    )
    expect(mappingAllowsPull(ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH)).toBe(false)

    expect(
      unknownSchemaFields(
        {
          field: 'title',
          nested: [{ remote: 'state' }, { remote: ['unknown_field', 'labels'] }],
          ignored: 42,
        },
        ['title', 'state', 'labels'],
      ),
    ).toEqual(['unknown_field'])
    expect(unknownSchemaFields({ local_title: '   ', other: 1 }, ['known'])).toEqual([
      'local_title',
      'other',
    ])

    expect(
      recordTimelineTargetFromFailure({
        id: 'failure-123456',
        mappingId: 'mapping-1',
        localObjectId: '',
        externalKey: 'ISSUE-7',
      } as never),
    ).toMatchObject({ mappingId: 'mapping-1', externalKey: 'ISSUE-7', label: 'ISSUE-7' })
    expect(
      recordTimelineTargetFromFailure({
        id: 'failure-123456',
        mappingId: 'mapping-1',
        localObjectId: '',
        externalKey: '',
      } as never),
    ).toMatchObject({ mappingId: 'mapping-1', label: 'failure-' })
    expect(
      recordTimelineTargetFromConflict({
        id: 'conflict-abcdef',
        mappingId: 'mapping-2',
        localObjectId: 'cr-9',
        externalKey: '',
      } as never),
    ).toMatchObject({ mappingId: 'mapping-2', localObjectId: 'cr-9', label: 'cr-9' })
    expect(
      recordTimelineTargetFromConflict({
        id: 'conflict-abcdef',
        mappingId: 'mapping-2',
        localObjectId: '',
        externalKey: '',
      } as never),
    ).toMatchObject({ mappingId: 'mapping-2', label: 'conflict' })
    expect(canShowRecordTimeline('', '')).toBe(false)
    expect(canShowRecordTimeline('cr-7', '')).toBe(true)
    expect(canShowRecordTimeline('', 'ISSUE-7')).toBe(true)

    expect(prettyJSON('')).toBe('{}')
    expect(prettyJSON('{"b":2,"a":1}')).toContain('\n')
    expect(prettyJSON('not-json')).toBe('not-json')
    expect(errorMessage(new Error('boom'))).toBe('boom')
    expect(errorMessage('boom')).toBe('failed')
    expect(
      qualificationToastDescription([
        { status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK', summary: 'ok' },
        { status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_WARNING', summary: 'warn' },
        { status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_FAILED', summary: 'failed' },
      ]),
    ).toBe('warn\nfailed')
    expect(qualificationToastDescription([{ status: 'ok', summary: 'ok only' }])).toBe('ok only')

    expect(isActiveRun({ inFlight: true, status: 'anything' } as never)).toBe(true)
    expect(
      isActiveRun({ inFlight: false, status: 'EXTERNAL_SYNC_RUN_STATUS_RUNNING' } as never),
    ).toBe(true)
    expect(
      isActiveRun({ inFlight: false, status: 'EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED' } as never),
    ).toBe(false)
    expect(
      isReplayableEvent({
        status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
        signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED',
      } as never),
    ).toBe(true)
    expect(
      isReplayableEvent({
        status: 'EXTERNAL_SYNC_EVENT_STATUS_RECEIVED',
        signatureStatus: 'EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED',
      } as never),
    ).toBe(false)
  })
})
