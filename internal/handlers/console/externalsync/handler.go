// SPDX-License-Identifier: Apache-2.0

// Package externalsync exposes the Console API for external sync connections,
// mappings, sync runs, failures, conflicts, and health.
package externalsync

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	externalsynccore "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/externalsync"
)

type service interface {
	ListConnections(ctx context.Context, tenantID string) ([]repo.Connection, error)
	ListProviderInstallations(ctx context.Context, tenantID string) ([]repo.ProviderInstallation, error)
	CreateProviderInstallation(ctx context.Context, in svc.CreateProviderInstallationInput) (*repo.ProviderInstallation, []repo.ProviderInstallationResource, error)
	DeleteProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) error
	QualifyProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) (svc.ProviderInstallationQualificationResult, error)
	ListProviderInstallationResources(ctx context.Context, tenantID string, installationID uuid.UUID) ([]repo.ProviderInstallationResource, error)
	SelectProviderInstallationResources(ctx context.Context, in svc.SelectProviderInstallationResourcesInput) ([]repo.ProviderInstallationResource, error)
	CreateConnection(ctx context.Context, in svc.CreateConnectionInput) (*repo.Connection, error)
	UpdateConnection(ctx context.Context, in svc.UpdateConnectionInput) (*repo.Connection, error)
	DeleteConnection(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) error
	TestConnection(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (externalsynccore.CheckResult, error)
	ResumeConnection(ctx context.Context, in svc.ResumeConnectionInput) (*repo.Connection, error)
	QualifyConnection(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (svc.QualificationResult, error)
	DiscoverConnectionSchema(ctx context.Context, tenantID string, id uuid.UUID) ([]externalsynccore.ObjectSchema, error)
	ListMappings(ctx context.Context, tenantID string, connectionID uuid.UUID) ([]repo.Mapping, error)
	UpdateMapping(ctx context.Context, in svc.UpdateMappingInput) (*repo.Mapping, error)
	PreviewMapping(ctx context.Context, in svc.PreviewMappingInput) (svc.MappingPreview, error)
	ResetCursor(ctx context.Context, in svc.ResetCursorInput) (*repo.ResetCursorResult, error)
	RequestBackfill(ctx context.Context, in svc.BackfillInput) (*repo.BackfillResult, error)
	RequestRun(ctx context.Context, in svc.RequestRunInput) (*repo.SyncRun, error)
	ListRuns(ctx context.Context, in svc.ListRunsInput) (repo.ListRunsResult, error)
	GetRunDetail(ctx context.Context, tenantID string, id uuid.UUID) (*repo.RunDetail, error)
	RecordTimeline(ctx context.Context, in svc.RecordTimelineInput) ([]repo.RecordTimelineEntry, error)
	RetryRun(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) (*repo.SyncRun, error)
	RetryFailure(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) (*repo.RecordFailure, error)
	ResolveConflict(ctx context.Context, tenantID string, id uuid.UUID, resolution string, actor svc.Actor, auditActor auditlogsvc.Actor) (*repo.ConflictRow, error)
	BatchResolveConflicts(ctx context.Context, in svc.BatchResolveConflictsInput) (repo.BatchResolveConflictsResult, error)
	ListEvents(ctx context.Context, in svc.ListEventsInput) (repo.ListEventsResult, error)
	GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*repo.SyncEvent, error)
	ReplayEvent(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) (*repo.SyncEvent, *repo.SyncRun, error)
	Health(ctx context.Context, tenantID string) (repo.Health, error)
}

type Handler struct {
	service service
}

const invalidProtoDirection = "__invalid_direction__"

func NewHandler(service *svc.Service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func (h *Handler) ListConnections(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListExternalConnectionsRequest) (dispatcher.Result[*attunev1.ListExternalConnectionsResponse], error) {
	rows, err := h.service.ListConnections(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.ListExternalConnectionsResponse](ctx, "ListConnections", err)
	}
	out := make([]*attunev1.ExternalConnection, 0, len(rows))
	for _, row := range rows {
		out = append(out, connectionToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalConnectionsResponse{Connections: out}))
}

func (h *Handler) ListProviders(_ *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListExternalSyncProvidersRequest) (dispatcher.Result[*attunev1.ListExternalSyncProvidersResponse], error) {
	entries := externalsynccore.Providers()
	out := make([]*attunev1.ExternalSyncProvider, 0, len(entries))
	for _, entry := range entries {
		if entry.Provider == "noop" {
			continue
		}
		out = append(out, ptrext.Of(attunev1.ExternalSyncProvider{
			Provider: entry.Provider,
			Display:  entry.Display,
		}))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalSyncProvidersResponse{Providers: out}))
}

func (h *Handler) ListProviderInstallations(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListExternalProviderInstallationsRequest) (dispatcher.Result[*attunev1.ListExternalProviderInstallationsResponse], error) {
	rows, err := h.service.ListProviderInstallations(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.ListExternalProviderInstallationsResponse](ctx, "ListProviderInstallations", err)
	}
	out := make([]*attunev1.ExternalProviderInstallation, 0, len(rows))
	for _, row := range rows {
		out = append(out, providerInstallationToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalProviderInstallationsResponse{Installations: out}))
}

func (h *Handler) CreateProviderInstallation(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateExternalProviderInstallationRequest) (dispatcher.Result[*attunev1.ExternalProviderInstallation], error) {
	row, _, err := h.service.CreateProviderInstallation(ctx, svc.CreateProviderInstallationInput{
		TenantID:               ctx.Auth.TenantID,
		Provider:               req.GetProvider(),
		DisplayName:            req.GetDisplayName(),
		InstallationKind:       req.GetInstallationKind(),
		ExternalInstallationID: req.GetExternalInstallationId(),
		AccountLogin:           req.GetAccountLogin(),
		AccountID:              req.GetAccountId(),
		AccountURL:             req.GetAccountUrl(),
		BaseURL:                req.GetBaseUrl(),
		PermissionsJSON:        req.GetPermissionsJson(),
		CapabilityProfileJSON:  req.GetCapabilityProfileJson(),
		ResourceSelection:      req.GetResourceSelection(),
		Resources:              resourceInputsFromProto(req.GetResources()),
		Actor:                  actor(ctx.Auth),
		AuditActor:             auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalProviderInstallation](ctx, "CreateProviderInstallation", err)
	}
	return dispatcher.OK(providerInstallationToProto(ptrext.Indirect(row)))
}

func (h *Handler) DeleteProviderInstallation(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteExternalProviderInstallationRequest) (dispatcher.Result[*attunev1.DeleteExternalProviderInstallationResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.DeleteExternalProviderInstallationResponse]("invalid provider installation id")
	}
	err = h.service.DeleteProviderInstallation(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.DeleteExternalProviderInstallationResponse](ctx, "DeleteProviderInstallation", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteExternalProviderInstallationResponse{}))
}

func (h *Handler) QualifyProviderInstallation(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.QualifyExternalProviderInstallationRequest) (dispatcher.Result[*attunev1.QualifyExternalProviderInstallationResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.QualifyExternalProviderInstallationResponse]("invalid provider installation id")
	}
	result, err := h.service.QualifyProviderInstallation(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.QualifyExternalProviderInstallationResponse](ctx, "QualifyProviderInstallation", err)
	}
	return dispatcher.OK(providerInstallationQualificationToProto(result))
}

func (h *Handler) ListProviderInstallationResources(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListExternalProviderInstallationResourcesRequest) (dispatcher.Result[*attunev1.ListExternalProviderInstallationResourcesResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ListExternalProviderInstallationResourcesResponse]("invalid provider installation id")
	}
	rows, err := h.service.ListProviderInstallationResources(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.ListExternalProviderInstallationResourcesResponse](ctx, "ListProviderInstallationResources", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalProviderInstallationResourcesResponse{
		Resources: providerInstallationResourcesToProto(rows),
	}))
}

func (h *Handler) SelectProviderInstallationResources(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.SelectExternalProviderInstallationResourcesRequest) (dispatcher.Result[*attunev1.SelectExternalProviderInstallationResourcesResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.SelectExternalProviderInstallationResourcesResponse]("invalid provider installation id")
	}
	resourceIDs, err := parseUUIDs(req.GetResourceIds())
	if err != nil {
		return badID[*attunev1.SelectExternalProviderInstallationResourcesResponse]("invalid provider installation resource id")
	}
	rows, err := h.service.SelectProviderInstallationResources(ctx, svc.SelectProviderInstallationResourcesInput{
		TenantID:       ctx.Auth.TenantID,
		InstallationID: id,
		ResourceIDs:    resourceIDs,
		Actor:          actor(ctx.Auth),
		AuditActor:     auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.SelectExternalProviderInstallationResourcesResponse](ctx, "SelectProviderInstallationResources", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.SelectExternalProviderInstallationResourcesResponse{
		Resources: providerInstallationResourcesToProto(rows),
	}))
}

func (h *Handler) CreateConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateExternalConnectionRequest) (dispatcher.Result[*attunev1.ExternalConnection], error) {
	enabled := true
	if req.Enabled != nil {
		enabled = req.GetEnabled()
	}
	providerInstallationID, err := parseOptionalUUID(req.GetProviderInstallationId())
	if err != nil {
		return badID[*attunev1.ExternalConnection]("invalid provider installation id")
	}
	row, err := h.service.CreateConnection(ctx, svc.CreateConnectionInput{
		TenantID:               ctx.Auth.TenantID,
		ProviderInstallationID: providerInstallationID,
		Provider:               req.GetProvider(),
		Name:                   req.GetName(),
		AuthType:               req.GetAuthType(),
		Credential:             req.GetCredential(),
		WebhookSecret:          req.GetWebhookSecret(),
		BaseURL:                req.GetBaseUrl(),
		ProviderConfigJSON:     req.GetProviderConfigJson(),
		Scopes:                 req.GetScopes(),
		Enabled:                enabled,
		Actor:                  actor(ctx.Auth),
		AuditActor:             auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalConnection](ctx, "CreateConnection", err)
	}
	return dispatcher.OK(connectionToProto(ptrext.Indirect(row)))
}

func (h *Handler) UpdateConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateExternalConnectionRequest) (dispatcher.Result[*attunev1.ExternalConnection], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalConnection]("invalid connection id")
	}
	row, err := h.service.UpdateConnection(ctx, svc.UpdateConnectionInput{
		TenantID:           ctx.Auth.TenantID,
		ID:                 id,
		Name:               req.Name,
		Enabled:            req.Enabled,
		Credential:         req.Credential,
		WebhookSecret:      req.WebhookSecret,
		BaseURL:            req.BaseUrl,
		ProviderConfigJSON: req.ProviderConfigJson,
		Scopes:             req.GetScopes(),
		Actor:              actor(ctx.Auth),
		AuditActor:         auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalConnection](ctx, "UpdateConnection", err)
	}
	return dispatcher.OK(connectionToProto(ptrext.Indirect(row)))
}

func (h *Handler) DeleteConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteExternalConnectionRequest) (dispatcher.Result[*attunev1.DeleteExternalConnectionResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.DeleteExternalConnectionResponse]("invalid connection id")
	}
	if err := h.service.DeleteConnection(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request())); err != nil {
		return mapError[*attunev1.DeleteExternalConnectionResponse](ctx, "DeleteConnection", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteExternalConnectionResponse{}))
}

func (h *Handler) TestConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.TestExternalConnectionRequest) (dispatcher.Result[*attunev1.TestExternalConnectionResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.TestExternalConnectionResponse]("invalid connection id")
	}
	result, err := h.service.TestConnection(ctx, ctx.Auth.TenantID, id, auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	resp := ptrext.Of(attunev1.TestExternalConnectionResponse{
		Ok:        result.OK,
		Error:     result.Error,
		LatencyMs: result.Latency.Milliseconds(),
	})
	if err != nil {
		if errors.Is(err, svc.ErrProviderUnavailable) {
			return dispatcher.OK(resp)
		}
		return mapError[*attunev1.TestExternalConnectionResponse](ctx, "TestConnection", err)
	}
	return dispatcher.OK(resp)
}

func (h *Handler) ResumeConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResumeExternalConnectionRequest) (dispatcher.Result[*attunev1.ExternalConnection], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalConnection]("invalid connection id")
	}
	row, err := h.service.ResumeConnection(ctx, svc.ResumeConnectionInput{
		TenantID:   ctx.Auth.TenantID,
		ID:         id,
		Actor:      actor(ctx.Auth),
		AuditActor: auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalConnection](ctx, "ResumeConnection", err)
	}
	return dispatcher.OK(connectionToProto(ptrext.Indirect(row)))
}

func (h *Handler) QualifyConnection(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.QualifyExternalConnectionRequest) (dispatcher.Result[*attunev1.QualifyExternalConnectionResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.QualifyExternalConnectionResponse]("invalid connection id")
	}
	result, err := h.service.QualifyConnection(ctx, ctx.Auth.TenantID, id, auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.QualifyExternalConnectionResponse](ctx, "QualifyConnection", err)
	}
	return dispatcher.OK(qualificationToProto(result))
}

func (h *Handler) DiscoverConnectionSchema(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DiscoverExternalConnectionSchemaRequest) (dispatcher.Result[*attunev1.DiscoverExternalConnectionSchemaResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.DiscoverExternalConnectionSchemaResponse]("invalid connection id")
	}
	schemas, err := h.service.DiscoverConnectionSchema(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.DiscoverExternalConnectionSchemaResponse](ctx, "DiscoverConnectionSchema", err)
	}
	out := make([]*attunev1.ExternalObjectSchema, 0, len(schemas))
	for _, schema := range schemas {
		out = append(out, schemaToProto(schema))
	}
	return dispatcher.OK(ptrext.Of(attunev1.DiscoverExternalConnectionSchemaResponse{Schemas: out}))
}

func (h *Handler) ListMappings(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListExternalObjectMappingsRequest) (dispatcher.Result[*attunev1.ListExternalObjectMappingsResponse], error) {
	connectionID := uuid.Nil
	if req.GetConnectionId() != "" {
		id, err := parseUUID(req.GetConnectionId())
		if err != nil {
			return badID[*attunev1.ListExternalObjectMappingsResponse]("invalid connection id")
		}
		connectionID = id
	}
	rows, err := h.service.ListMappings(ctx, ctx.Auth.TenantID, connectionID)
	if err != nil {
		return internalError[*attunev1.ListExternalObjectMappingsResponse](ctx, "ListMappings", err)
	}
	out := make([]*attunev1.ExternalObjectMapping, 0, len(rows))
	for _, row := range rows {
		out = append(out, mappingToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalObjectMappingsResponse{Mappings: out}))
}

func (h *Handler) UpdateMapping(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateExternalObjectMappingRequest) (dispatcher.Result[*attunev1.ExternalObjectMapping], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalObjectMapping]("invalid mapping id")
	}
	row, err := h.service.UpdateMapping(ctx, svc.UpdateMappingInput{
		TenantID:          ctx.Auth.TenantID,
		ID:                id,
		Direction:         mappingDirectionFromProto(req.GetDirection()),
		FieldMappingJSON:  req.GetFieldMappingJson(),
		StatusMappingJSON: req.GetStatusMappingJson(),
		ConflictPolicy:    req.GetConflictPolicy(),
		TombstonePolicy:   req.GetTombstonePolicy(),
		Enabled:           req.Enabled,
		Actor:             actor(ctx.Auth),
		AuditActor:        auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalObjectMapping](ctx, "UpdateMapping", err)
	}
	return dispatcher.OK(mappingToProto(ptrext.Indirect(row)))
}

func (h *Handler) PreviewMapping(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PreviewExternalObjectMappingRequest) (dispatcher.Result[*attunev1.PreviewExternalObjectMappingResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.PreviewExternalObjectMappingResponse]("invalid mapping id")
	}
	result, err := h.service.PreviewMapping(ctx, svc.PreviewMappingInput{
		TenantID:          ctx.Auth.TenantID,
		ID:                id,
		FieldMappingJSON:  req.FieldMappingJson,
		StatusMappingJSON: req.StatusMappingJson,
	})
	if err != nil {
		return mapError[*attunev1.PreviewExternalObjectMappingResponse](ctx, "PreviewMapping", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.PreviewExternalObjectMappingResponse{
		Schema:   schemaToProto(result.Schema),
		Errors:   result.Errors,
		Warnings: result.Warnings,
	}))
}

func (h *Handler) ResetCursor(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResetExternalSyncCursorRequest) (dispatcher.Result[*attunev1.ResetExternalSyncCursorResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ResetExternalSyncCursorResponse]("invalid mapping id")
	}
	result, err := h.service.ResetCursor(ctx, svc.ResetCursorInput{
		TenantID:   ctx.Auth.TenantID,
		ID:         id,
		Actor:      actor(ctx.Auth),
		AuditActor: auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ResetExternalSyncCursorResponse](ctx, "ResetCursor", err)
	}
	return dispatcher.Success(http.StatusAccepted, ptrext.Of(attunev1.ResetExternalSyncCursorResponse{
		Mapping: mappingToProto(result.Mapping),
		Run:     runToProto(result.Run),
	}))
}

func (h *Handler) RequestBackfill(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RequestExternalSyncBackfillRequest) (dispatcher.Result[*attunev1.RequestExternalSyncBackfillResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.RequestExternalSyncBackfillResponse]("invalid mapping id")
	}
	result, err := h.service.RequestBackfill(ctx, svc.BackfillInput{
		TenantID:    ctx.Auth.TenantID,
		ID:          id,
		ResetCursor: req.GetResetCursor(),
		Actor:       actor(ctx.Auth),
		AuditActor:  auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.RequestExternalSyncBackfillResponse](ctx, "RequestBackfill", err)
	}
	return dispatcher.Success(http.StatusAccepted, ptrext.Of(attunev1.RequestExternalSyncBackfillResponse{
		Mapping: mappingToProto(result.Mapping),
		Run:     runToProto(result.Run),
	}))
}

func (h *Handler) RequestRun(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RequestExternalSyncRunRequest) (dispatcher.Result[*attunev1.ExternalSyncRun], error) {
	connectionID, err := parseUUID(req.GetConnectionId())
	if err != nil {
		return badID[*attunev1.ExternalSyncRun]("invalid connection id")
	}
	var mappingID *uuid.UUID
	if req.GetMappingId() != "" {
		id, err := parseUUID(req.GetMappingId())
		if err != nil {
			return badID[*attunev1.ExternalSyncRun]("invalid mapping id")
		}
		mappingID = ptrext.Of(id)
	}
	row, err := h.service.RequestRun(ctx, svc.RequestRunInput{
		TenantID:      ctx.Auth.TenantID,
		ConnectionID:  connectionID,
		MappingID:     mappingID,
		Direction:     runDirectionFromProto(req.GetDirection()),
		LocalObjectID: req.GetLocalObjectId(),
		ExternalKey:   req.GetExternalKey(),
		Actor:         actor(ctx.Auth),
		AuditActor:    auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalSyncRun](ctx, "RequestRun", err)
	}
	return dispatcher.Success(http.StatusAccepted, runToProto(ptrext.Indirect(row)))
}

func (h *Handler) ListRuns(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListExternalSyncRunsRequest) (dispatcher.Result[*attunev1.ListExternalSyncRunsResponse], error) {
	in, err := listRunsInput(ctx.Auth.TenantID, req)
	if err != nil {
		return badID[*attunev1.ListExternalSyncRunsResponse](err.Error())
	}
	result, err := h.service.ListRuns(ctx, in)
	if err != nil {
		return mapError[*attunev1.ListExternalSyncRunsResponse](ctx, "ListRuns", err)
	}
	out := make([]*attunev1.ExternalSyncRun, 0, len(result.Runs))
	for _, row := range result.Runs {
		out = append(out, runToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalSyncRunsResponse{
		Runs:         out,
		NextBeforeId: result.NextBeforeID,
	}))
}

func (h *Handler) GetRun(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetExternalSyncRunRequest) (dispatcher.Result[*attunev1.ExternalSyncRunDetail], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalSyncRunDetail]("invalid run id")
	}
	detail, err := h.service.GetRunDetail(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.ExternalSyncRunDetail](ctx, "GetRun", err)
	}
	return dispatcher.OK(detailToProto(ptrext.Indirect(detail)))
}

func (h *Handler) RecordTimeline(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetExternalSyncRecordTimelineRequest) (dispatcher.Result[*attunev1.ExternalSyncRecordTimelineResponse], error) {
	mappingID, err := parseUUID(req.GetMappingId())
	if err != nil {
		return badID[*attunev1.ExternalSyncRecordTimelineResponse]("invalid mapping id")
	}
	rows, err := h.service.RecordTimeline(ctx, svc.RecordTimelineInput{
		TenantID:      ctx.Auth.TenantID,
		MappingID:     mappingID,
		LocalObjectID: req.GetLocalObjectId(),
		ExternalKey:   req.GetExternalKey(),
		Limit:         int(req.GetLimit()),
	})
	if err != nil {
		return mapError[*attunev1.ExternalSyncRecordTimelineResponse](ctx, "RecordTimeline", err)
	}
	out := make([]*attunev1.ExternalSyncRecordTimelineEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, timelineEntryToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ExternalSyncRecordTimelineResponse{Entries: out}))
}

func (h *Handler) RetryRun(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RetryExternalSyncRunRequest) (dispatcher.Result[*attunev1.ExternalSyncRun], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalSyncRun]("invalid run id")
	}
	row, err := h.service.RetryRun(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.ExternalSyncRun](ctx, "RetryRun", err)
	}
	return dispatcher.Success(http.StatusAccepted, runToProto(ptrext.Indirect(row)))
}

func (h *Handler) RetryFailure(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RetryExternalSyncFailureRequest) (dispatcher.Result[*attunev1.ExternalSyncRecordFailure], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalSyncRecordFailure]("invalid failure id")
	}
	row, err := h.service.RetryFailure(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.ExternalSyncRecordFailure](ctx, "RetryFailure", err)
	}
	return dispatcher.OK(failureToProto(ptrext.Indirect(row)))
}

func (h *Handler) ResolveConflict(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResolveExternalSyncConflictRequest) (dispatcher.Result[*attunev1.ExternalSyncConflict], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalSyncConflict]("invalid conflict id")
	}
	row, err := h.service.ResolveConflict(ctx, ctx.Auth.TenantID, id, resolutionFromProto(req.GetResolution()), actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.ExternalSyncConflict](ctx, "ResolveConflict", err)
	}
	return dispatcher.OK(conflictToProto(ptrext.Indirect(row)))
}

func (h *Handler) BatchResolveConflicts(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.BatchResolveExternalSyncConflictsRequest) (dispatcher.Result[*attunev1.BatchResolveExternalSyncConflictsResponse], error) {
	ids, err := parseUUIDs(req.GetIds())
	if err != nil {
		return badID[*attunev1.BatchResolveExternalSyncConflictsResponse]("invalid conflict id")
	}
	result, err := h.service.BatchResolveConflicts(ctx, svc.BatchResolveConflictsInput{
		TenantID:   ctx.Auth.TenantID,
		IDs:        ids,
		Resolution: resolutionFromProto(req.GetResolution()),
		Actor:      actor(ctx.Auth),
		AuditActor: auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.BatchResolveExternalSyncConflictsResponse](ctx, "BatchResolveConflicts", err)
	}
	conflicts := make([]*attunev1.ExternalSyncConflict, 0, len(result.Conflicts))
	for _, row := range result.Conflicts {
		conflicts = append(conflicts, conflictToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.BatchResolveExternalSyncConflictsResponse{
		Conflicts:     conflicts,
		ResolvedCount: int32(len(conflicts)),
	}))
}

func (h *Handler) ListEvents(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListExternalSyncEventsRequest) (dispatcher.Result[*attunev1.ListExternalSyncEventsResponse], error) {
	in, err := listEventsInput(ctx.Auth.TenantID, req)
	if err != nil {
		return badID[*attunev1.ListExternalSyncEventsResponse](err.Error())
	}
	result, err := h.service.ListEvents(ctx, in)
	if err != nil {
		return mapError[*attunev1.ListExternalSyncEventsResponse](ctx, "ListEvents", err)
	}
	out := make([]*attunev1.ExternalSyncEvent, 0, len(result.Events))
	for _, row := range result.Events {
		out = append(out, eventToProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListExternalSyncEventsResponse{
		Events:       out,
		NextBeforeId: result.NextBeforeID,
	}))
}

func (h *Handler) GetEvent(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetExternalSyncEventRequest) (dispatcher.Result[*attunev1.ExternalSyncEvent], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ExternalSyncEvent]("invalid event id")
	}
	event, err := h.service.GetEvent(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.ExternalSyncEvent](ctx, "GetEvent", err)
	}
	return dispatcher.OK(eventToProto(ptrext.Indirect(event)))
}

func (h *Handler) ReplayEvent(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ReplayExternalSyncEventRequest) (dispatcher.Result[*attunev1.ReplayExternalSyncEventResponse], error) {
	id, err := parseUUID(req.GetId())
	if err != nil {
		return badID[*attunev1.ReplayExternalSyncEventResponse]("invalid event id")
	}
	event, run, err := h.service.ReplayEvent(ctx, ctx.Auth.TenantID, id, actor(ctx.Auth), auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if err != nil {
		return mapError[*attunev1.ReplayExternalSyncEventResponse](ctx, "ReplayEvent", err)
	}
	return dispatcher.Success(http.StatusAccepted, ptrext.Of(attunev1.ReplayExternalSyncEventResponse{
		Event: eventToProto(ptrext.Indirect(event)),
		Run:   runToProto(ptrext.Indirect(run)),
	}))
}

func (h *Handler) Health(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.GetExternalSyncHealthRequest) (dispatcher.Result[*attunev1.ExternalSyncHealthResponse], error) {
	health, err := h.service.Health(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.ExternalSyncHealthResponse](ctx, "Health", err)
	}
	return dispatcher.OK(healthToProto(health))
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func listRunsInput(tenantID string, req *attunev1.ListExternalSyncRunsRequest) (svc.ListRunsInput, error) {
	connectionID, err := parseOptionalUUID(req.GetConnectionId())
	if err != nil {
		return svc.ListRunsInput{}, errors.New("invalid connection id")
	}
	mappingID, err := parseOptionalUUID(req.GetMappingId())
	if err != nil {
		return svc.ListRunsInput{}, errors.New("invalid mapping id")
	}
	beforeID, err := parseOptionalUUID(req.GetBeforeId())
	if err != nil {
		return svc.ListRunsInput{}, errors.New("invalid before id")
	}
	return svc.ListRunsInput{
		TenantID:     tenantID,
		ConnectionID: connectionID,
		MappingID:    mappingID,
		Status:       req.GetStatus(),
		BeforeID:     beforeID,
		Limit:        int(req.GetLimit()),
	}, nil
}

func listEventsInput(tenantID string, req *attunev1.ListExternalSyncEventsRequest) (svc.ListEventsInput, error) {
	connectionID, err := parseOptionalUUID(req.GetConnectionId())
	if err != nil {
		return svc.ListEventsInput{}, errors.New("invalid connection id")
	}
	beforeID, err := parseOptionalUUID(req.GetBeforeId())
	if err != nil {
		return svc.ListEventsInput{}, errors.New("invalid before id")
	}
	return svc.ListEventsInput{
		TenantID:     tenantID,
		ConnectionID: connectionID,
		Status:       req.GetStatus(),
		BeforeID:     beforeID,
		Limit:        int(req.GetLimit()),
	}, nil
}

func parseOptionalUUID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := parseUUID(raw)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(id), nil
}

func parseUUIDs(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := parseUUID(value)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func actor(auth *session.AuthCtx) svc.Actor {
	return svc.Actor{Type: auth.UserType, ID: auth.UserID}
}

func mapError[T proto.Message](ctx context.Context, where string, err error) (dispatcher.Result[T], error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		return dispatcher.Fail[T](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	case errors.Is(err, svc.ErrProviderUnavailable):
		return dispatcher.Fail[T](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	case errors.Is(err, repo.ErrConnectionNotFound), errors.Is(err, repo.ErrMappingNotFound), errors.Is(err, repo.ErrRunNotFound), errors.Is(err, repo.ErrFailureNotFound), errors.Is(err, repo.ErrConflictNotFound), errors.Is(err, repo.ErrEventNotFound), errors.Is(err, repo.ErrInstallationNotFound), errors.Is(err, repo.ErrResourceNotFound):
		return dispatcher.Fail[T](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "external sync resource not found")
	case errors.Is(err, repo.ErrConflict):
		return dispatcher.Fail[T](http.StatusConflict, attunev1.ErrorCode_CONFLICT, err.Error())
	default:
		return internalError[T](ctx, where, err)
	}
}

func internalError[T proto.Message](ctx context.Context, where string, err error) (dispatcher.Result[T], error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return dispatcher.Result[T]{}, err
	}
	logext.Errorf(ctx, "[console.externalsync.%s] failed,err_type:%T", where, err)
	return dispatcher.Fail[T](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "external sync operation failed")
}

func badID[T proto.Message](msg string) (dispatcher.Result[T], error) {
	return dispatcher.Fail[T](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, msg)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(ptrext.Indirect(t))
}

func formatUUIDPtr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return ptrext.Indirect(id).String()
}

func directionFromProto(direction attunev1.ExternalSyncDirection) string {
	switch direction {
	case attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PULL:
		return repo.DirectionPull
	case attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH:
		return repo.DirectionPush
	case attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL:
		return repo.DirectionBidirectional
	default:
		return invalidProtoDirection
	}
}

func runDirectionFromProto(direction attunev1.ExternalSyncDirection) string {
	if direction == attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_UNSPECIFIED {
		return ""
	}
	return directionFromProto(direction)
}

func mappingDirectionFromProto(direction attunev1.ExternalSyncDirection) string {
	if direction == attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_UNSPECIFIED {
		return ""
	}
	return directionFromProto(direction)
}

func directionToProto(direction string) attunev1.ExternalSyncDirection {
	switch direction {
	case repo.DirectionPush:
		return attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PUSH
	case repo.DirectionBidirectional:
		return attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL
	default:
		return attunev1.ExternalSyncDirection_EXTERNAL_SYNC_DIRECTION_PULL
	}
}

func triggerToProto(trigger string) attunev1.ExternalSyncRunTrigger {
	switch trigger {
	case "schedule":
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_SCHEDULE
	case repo.TriggerRetry:
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_RETRY
	case repo.TriggerSystem:
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_SYSTEM
	case repo.TriggerWebhook:
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_WEBHOOK
	case repo.TriggerBackfill:
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_BACKFILL
	default:
		return attunev1.ExternalSyncRunTrigger_EXTERNAL_SYNC_RUN_TRIGGER_MANUAL
	}
}

func statusToProto(status string) attunev1.ExternalSyncRunStatus {
	switch status {
	case repo.RunStatusRunning:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_RUNNING
	case repo.RunStatusSucceeded:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED
	case repo.RunStatusPartial:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_PARTIAL
	case repo.RunStatusFailed:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_FAILED
	case "cancelled":
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_CANCELLED
	case repo.RunStatusDead:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_DEAD
	default:
		return attunev1.ExternalSyncRunStatus_EXTERNAL_SYNC_RUN_STATUS_QUEUED
	}
}

func eventSignatureToProto(status string) attunev1.ExternalSyncEventSignatureStatus {
	switch status {
	case repo.EventSignatureFailed:
		return attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED
	case repo.EventSignatureNotRequired:
		return attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_NOT_REQUIRED
	case repo.EventSignatureVerified:
		return attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED
	default:
		return attunev1.ExternalSyncEventSignatureStatus_EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_UNSPECIFIED
	}
}

func eventStatusToProto(status string) attunev1.ExternalSyncEventStatus {
	switch status {
	case repo.EventStatusReplayed:
		return attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_REPLAYED
	case repo.EventStatusIgnored:
		return attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_IGNORED
	case repo.EventStatusFailed:
		return attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_FAILED
	case repo.EventStatusReceived:
		return attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_RECEIVED
	default:
		return attunev1.ExternalSyncEventStatus_EXTERNAL_SYNC_EVENT_STATUS_UNSPECIFIED
	}
}

func resolutionFromProto(resolution attunev1.ExternalSyncConflictResolution) string {
	switch resolution {
	case attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_LOCAL_WINS:
		return "local_wins"
	case attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS:
		return "external_wins"
	case attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_MANUAL_MERGE:
		return "manual_merge"
	case attunev1.ExternalSyncConflictResolution_EXTERNAL_SYNC_CONFLICT_RESOLUTION_IGNORED:
		return "ignored"
	default:
		return ""
	}
}

func qualificationStatusToProto(status string) attunev1.ExternalSyncQualificationCheckStatus {
	switch status {
	case svc.QualificationStatusOK:
		return attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK
	case svc.QualificationStatusWarning:
		return attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_WARNING
	case svc.QualificationStatusFailed:
		return attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_FAILED
	default:
		return attunev1.ExternalSyncQualificationCheckStatus_EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_UNSPECIFIED
	}
}

func connectionToProto(row repo.Connection) *attunev1.ExternalConnection {
	return ptrext.Of(attunev1.ExternalConnection{
		Id:                      row.ID.String(),
		TenantId:                row.TenantID,
		Provider:                row.Provider,
		Name:                    row.Name,
		Enabled:                 row.Enabled,
		Status:                  row.Status,
		AuthType:                row.AuthType,
		BaseUrl:                 row.BaseURL,
		ProviderConfigJson:      string(row.ProviderConfig),
		Scopes:                  row.Scopes,
		LastTestedAt:            formatTimePtr(row.LastTestedAt),
		LastTestStatus:          row.LastTestStatus,
		LastError:               row.LastError,
		CreatedBy:               row.CreatedBy,
		UpdatedBy:               row.UpdatedBy,
		CreatedAt:               formatTime(row.CreatedAt),
		UpdatedAt:               formatTime(row.UpdatedAt),
		WebhookSecretConfigured: row.WebhookSecretKeyID != "" && len(row.WebhookSecretCiphertext) > 0,
		ProviderInstallationId:  formatUUIDPtr(row.ProviderInstallationID),
	})
}

func providerInstallationToProto(row repo.ProviderInstallation) *attunev1.ExternalProviderInstallation {
	return ptrext.Of(attunev1.ExternalProviderInstallation{
		Id:                     row.ID.String(),
		TenantId:               row.TenantID,
		Provider:               row.Provider,
		DisplayName:            row.DisplayName,
		InstallationKind:       row.InstallationKind,
		Status:                 row.Status,
		ExternalInstallationId: row.ExternalInstallationID,
		AccountLogin:           row.AccountLogin,
		AccountId:              row.AccountID,
		AccountUrl:             row.AccountURL,
		BaseUrl:                row.BaseURL,
		PermissionsJson:        string(row.Permissions),
		CapabilityProfileJson:  string(row.CapabilityProfile),
		ResourceSelection:      row.ResourceSelection,
		QualificationStatus:    row.QualificationStatus,
		LastQualifiedAt:        formatTimePtr(row.LastQualifiedAt),
		LastError:              row.LastError,
		CreatedBy:              row.CreatedBy,
		UpdatedBy:              row.UpdatedBy,
		CreatedAt:              formatTime(row.CreatedAt),
		UpdatedAt:              formatTime(row.UpdatedAt),
	})
}

func providerInstallationResourcesToProto(rows []repo.ProviderInstallationResource) []*attunev1.ExternalProviderInstallationResource {
	out := make([]*attunev1.ExternalProviderInstallationResource, 0, len(rows))
	for _, row := range rows {
		out = append(out, providerInstallationResourceToProto(row))
	}
	return out
}

func providerInstallationResourceToProto(row repo.ProviderInstallationResource) *attunev1.ExternalProviderInstallationResource {
	return ptrext.Of(attunev1.ExternalProviderInstallationResource{
		Id:                 row.ID.String(),
		TenantId:           row.TenantID,
		InstallationId:     row.InstallationID.String(),
		Provider:           row.Provider,
		ResourceType:       row.ResourceType,
		ExternalResourceId: row.ExternalResourceID,
		ResourceKey:        row.ResourceKey,
		DisplayName:        row.DisplayName,
		HtmlUrl:            row.HTMLURL,
		Selected:           row.Selected,
		Status:             row.Status,
		PermissionsJson:    string(row.Permissions),
		LastSeenAt:         formatTimePtr(row.LastSeenAt),
		CreatedAt:          formatTime(row.CreatedAt),
		UpdatedAt:          formatTime(row.UpdatedAt),
	})
}

func providerInstallationQualificationToProto(row svc.ProviderInstallationQualificationResult) *attunev1.QualifyExternalProviderInstallationResponse {
	checks := make([]*attunev1.ExternalSyncQualificationCheck, 0, len(row.Checks))
	for _, check := range row.Checks {
		checks = append(checks, ptrext.Of(attunev1.ExternalSyncQualificationCheck{
			Name:       check.Name,
			Status:     qualificationStatusToProto(check.Status),
			Summary:    check.Summary,
			DetailJson: check.DetailJSON,
		}))
	}
	return ptrext.Of(attunev1.QualifyExternalProviderInstallationResponse{
		InstallationId: row.Installation.ID.String(),
		Ready:          row.Ready,
		Grade:          row.Grade,
		Checks:         checks,
		Installation:   providerInstallationToProto(row.Installation),
	})
}

func resourceInputsFromProto(rows []*attunev1.ExternalProviderInstallationResourceInput) []svc.ProviderInstallationResourceInput {
	out := make([]svc.ProviderInstallationResourceInput, 0, len(rows))
	for _, row := range rows {
		out = append(out, svc.ProviderInstallationResourceInput{
			ResourceType:       row.GetResourceType(),
			ExternalResourceID: row.GetExternalResourceId(),
			ResourceKey:        row.GetResourceKey(),
			DisplayName:        row.GetDisplayName(),
			HTMLURL:            row.GetHtmlUrl(),
			Selected:           row.GetSelected(),
			Status:             row.GetStatus(),
			PermissionsJSON:    row.GetPermissionsJson(),
		})
	}
	return out
}

func mappingToProto(row repo.Mapping) *attunev1.ExternalObjectMapping {
	return ptrext.Of(attunev1.ExternalObjectMapping{
		Id:                 row.ID.String(),
		TenantId:           row.TenantID,
		ConnectionId:       row.ConnectionID.String(),
		LocalObjectType:    row.LocalObjectType,
		ExternalObjectType: row.ExternalObjectType,
		Direction:          directionToProto(row.Direction),
		FieldMappingJson:   string(row.FieldMapping),
		StatusMappingJson:  string(row.StatusMapping),
		ConflictPolicy:     row.ConflictPolicy,
		TombstonePolicy:    row.TombstonePolicy,
		Enabled:            row.Enabled,
		MappingVersion:     int32(row.MappingVersion),
		CreatedAt:          formatTime(row.CreatedAt),
		UpdatedAt:          formatTime(row.UpdatedAt),
	})
}

func schemaToProto(row externalsynccore.ObjectSchema) *attunev1.ExternalObjectSchema {
	return ptrext.Of(attunev1.ExternalObjectSchema{
		Type:           row.Type,
		Fields:         row.Fields,
		RequiredFields: row.RequiredFields,
		WritableFields: row.WritableFields,
	})
}

func qualificationToProto(row svc.QualificationResult) *attunev1.QualifyExternalConnectionResponse {
	checks := make([]*attunev1.ExternalSyncQualificationCheck, 0, len(row.Checks))
	for _, check := range row.Checks {
		checks = append(checks, ptrext.Of(attunev1.ExternalSyncQualificationCheck{
			Name:       check.Name,
			Status:     qualificationStatusToProto(check.Status),
			Summary:    check.Summary,
			DetailJson: check.DetailJSON,
		}))
	}
	return ptrext.Of(attunev1.QualifyExternalConnectionResponse{
		ConnectionId: row.ConnectionID.String(),
		Ready:        row.Ready,
		Checks:       checks,
	})
}

func runToProto(row repo.SyncRun) *attunev1.ExternalSyncRun {
	return ptrext.Of(attunev1.ExternalSyncRun{
		Id:                row.ID.String(),
		TenantId:          row.TenantID,
		ConnectionId:      row.ConnectionID.String(),
		MappingId:         uuidToString(row.MappingID),
		Direction:         directionToProto(row.Direction),
		Trigger:           triggerToProto(row.Trigger),
		Status:            statusToProto(row.Status),
		Attempts:          int32(row.Attempts),
		NextRetryAt:       formatTime(row.NextRetryAt),
		StartedAt:         formatTimePtr(row.StartedAt),
		FinishedAt:        formatTimePtr(row.FinishedAt),
		CursorBeforeJson:  string(row.CursorBefore),
		CursorAfterJson:   string(row.CursorAfter),
		InputMetadataJson: string(row.InputMetadata),
		RecordsSeen:       int32(row.RecordsSeen),
		RecordsChanged:    int32(row.RecordsChanged),
		RecordsFailed:     int32(row.RecordsFailed),
		ConflictsCreated:  int32(row.ConflictsCreated),
		ErrorKind:         row.ErrorKind,
		ErrorMessage:      row.ErrorMessage,
		ActorId:           row.ActorID,
		CreatedAt:         formatTime(row.CreatedAt),
		UpdatedAt:         formatTime(row.UpdatedAt),
		InFlight:          row.ClaimedAt != nil,
	})
}

func detailToProto(detail repo.RunDetail) *attunev1.ExternalSyncRunDetail {
	attempts := make([]*attunev1.ExternalSyncAttempt, 0, len(detail.Attempts))
	for _, attempt := range detail.Attempts {
		attempts = append(attempts, attemptToProto(attempt))
	}
	failures := make([]*attunev1.ExternalSyncRecordFailure, 0, len(detail.Failures))
	for _, failure := range detail.Failures {
		failures = append(failures, failureToProto(failure))
	}
	conflicts := make([]*attunev1.ExternalSyncConflict, 0, len(detail.Conflicts))
	for _, conflict := range detail.Conflicts {
		conflicts = append(conflicts, conflictToProto(conflict))
	}
	return ptrext.Of(attunev1.ExternalSyncRunDetail{
		Run:       runToProto(detail.Run),
		Attempts:  attempts,
		Failures:  failures,
		Conflicts: conflicts,
	})
}

func attemptToProto(row repo.SyncAttempt) *attunev1.ExternalSyncAttempt {
	return ptrext.Of(attunev1.ExternalSyncAttempt{
		Id:                row.ID,
		RunId:             row.RunID.String(),
		AttemptNumber:     int32(row.AttemptNumber),
		StartedAt:         formatTime(row.StartedAt),
		FinishedAt:        formatTimePtr(row.FinishedAt),
		Result:            row.Result,
		HttpStatus:        int32(row.HTTPStatus),
		ProviderRequestId: row.ProviderRequestID,
		RetryAfter:        formatTimePtr(row.RetryAfter),
		ErrorKind:         row.ErrorKind,
		ErrorMessage:      row.ErrorMessage,
	})
}

func failureToProto(row repo.RecordFailure) *attunev1.ExternalSyncRecordFailure {
	return ptrext.Of(attunev1.ExternalSyncRecordFailure{
		Id:                    row.ID.String(),
		TenantId:              row.TenantID,
		RunId:                 row.RunID.String(),
		MappingId:             row.MappingID.String(),
		Operation:             row.Operation,
		LocalObjectId:         row.LocalObjectID,
		ExternalKey:           row.ExternalKey,
		FailureKind:           row.FailureKind,
		Message:               row.Message,
		PayloadDigest:         row.PayloadDigest,
		RetryMode:             row.RetryMode,
		NormalizedPayloadJson: string(row.NormalizedPayload),
		Retryable:             row.Retryable,
		ResolvedAt:            formatTimePtr(row.ResolvedAt),
		ResolvedBy:            row.ResolvedBy,
		CreatedAt:             formatTime(row.CreatedAt),
	})
}

func conflictToProto(row repo.ConflictRow) *attunev1.ExternalSyncConflict {
	return ptrext.Of(attunev1.ExternalSyncConflict{
		Id:                   row.ID.String(),
		TenantId:             row.TenantID,
		MappingId:            row.MappingID.String(),
		LocalObjectId:        row.LocalObjectID,
		ExternalKey:          row.ExternalKey,
		ConflictKind:         row.ConflictKind,
		Status:               row.Status,
		LocalSnapshotJson:    string(row.LocalSnapshot),
		ExternalSnapshotJson: string(row.ExternalSnapshot),
		Resolution:           row.Resolution,
		ResolvedAt:           formatTimePtr(row.ResolvedAt),
		ResolvedBy:           row.ResolvedBy,
		CreatedAt:            formatTime(row.CreatedAt),
		UpdatedAt:            formatTime(row.UpdatedAt),
	})
}

func timelineEntryToProto(row repo.RecordTimelineEntry) *attunev1.ExternalSyncRecordTimelineEntry {
	return ptrext.Of(attunev1.ExternalSyncRecordTimelineEntry{
		Kind:          row.Kind,
		OccurredAt:    formatTime(row.OccurredAt),
		RunId:         uuidToString(row.RunID),
		Status:        row.Status,
		Operation:     row.Operation,
		LocalObjectId: row.LocalObjectID,
		ExternalKey:   row.ExternalKey,
		Summary:       row.Summary,
		DetailJson:    string(row.Detail),
	})
}

func eventToProto(row repo.SyncEvent) *attunev1.ExternalSyncEvent {
	return ptrext.Of(attunev1.ExternalSyncEvent{
		Id:                    row.ID.String(),
		TenantId:              row.TenantID,
		ConnectionId:          row.ConnectionID.String(),
		MappingId:             uuidToString(row.MappingID),
		Provider:              row.Provider,
		EventType:             row.EventType,
		ExternalEventId:       row.ExternalEventID,
		DedupeKey:             row.DedupeKey,
		SignatureStatus:       eventSignatureToProto(row.SignatureStatus),
		Status:                eventStatusToProto(row.Status),
		PayloadDigest:         row.PayloadDigest,
		NormalizedPayloadJson: string(row.NormalizedPayload),
		ReceivedAt:            formatTime(row.ReceivedAt),
		ReplayedAt:            formatTimePtr(row.ReplayedAt),
		ReplayedBy:            row.ReplayedBy,
		RunId:                 uuidToString(row.RunID),
		FailureReason:         row.FailureReason,
		CreatedAt:             formatTime(row.CreatedAt),
		UpdatedAt:             formatTime(row.UpdatedAt),
	})
}

func healthToProto(row repo.Health) *attunev1.ExternalSyncHealthResponse {
	return ptrext.Of(attunev1.ExternalSyncHealthResponse{
		EnabledConnections:      int32(row.EnabledConnections),
		FailingConnections:      int32(row.FailingConnections),
		StaleConnections:        int32(row.StaleConnections),
		ActiveRuns:              int32(row.ActiveRuns),
		RetryableRuns:           int32(row.RetryableRuns),
		DeadRuns:                int32(row.DeadRuns),
		OpenConflicts:           int32(row.OpenConflicts),
		NewestSuccessfulRunAt:   formatTimePtr(row.NewestSuccessfulRunAt),
		DisabledConnections:     int32(row.DisabledConnections),
		ThrottledRuns:           int32(row.ThrottledRuns),
		UnauthorizedRuns:        int32(row.UnauthorizedRuns),
		ProviderUnavailableRuns: int32(row.ProviderUnavailableRuns),
		DelayedRetryRuns:        int32(row.DelayedRetryRuns),
		NewestRetryAfter:        formatTimePtr(row.NewestRetryAfter),
		DegradedConnections:     int32(row.DegradedConnections),
		QuarantinedConnections:  int32(row.QuarantinedConnections),
	})
}

func uuidToString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func BindListRunsRequest(r *http.Request, req *attunev1.ListExternalSyncRunsRequest) error {
	req.ConnectionId = queryValue(r, "connection_id", "connectionId")
	req.MappingId = queryValue(r, "mapping_id", "mappingId")
	req.Status = queryValue(r, "status")
	req.BeforeId = queryValue(r, "before_id", "beforeId")
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid limit")
		}
		req.Limit = int32(n)
	}
	return nil
}

func BindListEventsRequest(r *http.Request, req *attunev1.ListExternalSyncEventsRequest) error {
	req.ConnectionId = queryValue(r, "connection_id", "connectionId")
	req.Status = queryValue(r, "status")
	req.BeforeId = queryValue(r, "before_id", "beforeId")
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid limit")
		}
		req.Limit = int32(n)
	}
	return nil
}

func queryValue(r *http.Request, names ...string) string {
	values := r.URL.Query()
	for _, name := range names {
		if value := values.Get(name); value != "" {
			return value
		}
	}
	return ""
}
