// SPDX-License-Identifier: Apache-2.0

// Package cohortsync provides the Console API handlers for cohort sync
// source management, cohort listing, sync triggering, and health.
package cohortsync

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Phixsura/attune/internal/cohortsync"
	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repo "github.com/Phixsura/attune/internal/repo/cohortsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	svc "github.com/Phixsura/attune/internal/service/cohortsync"
)

type service interface {
	CreateSource(ctx context.Context, in svc.CreateSourceInput) (*repo.Source, error)
	UpdateSource(ctx context.Context, in svc.UpdateSourceInput) (*repo.Source, error)
	TestSource(ctx context.Context, tenantID string, id uuid.UUID, auditActor auditlogsvc.Actor) (cohortsync.CheckResult, error)
	GetSource(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Source, error)
	ListSources(ctx context.Context, tenantID string) ([]repo.Source, error)
	DeleteSource(ctx context.Context, tenantID string, id uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) error
	GetCohort(ctx context.Context, tenantID string, id uuid.UUID) (*repo.Cohort, error)
	ListAllCohorts(ctx context.Context, tenantID string) ([]repo.Cohort, error)
	ListCohorts(ctx context.Context, tenantID string, sourceID uuid.UUID) ([]repo.Cohort, error)
	UpdateCohort(ctx context.Context, in svc.UpdateCohortInput) (*repo.Cohort, error)
	ListMembers(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]repo.Membership, error)
	SyncNow(ctx context.Context, tenantID string, cohortID uuid.UUID, actor svc.Actor, auditActor auditlogsvc.Actor) (*svc.SyncRunResult, error)
	ListRuns(ctx context.Context, tenantID string, cohortID uuid.UUID, limit int) ([]repo.SyncRun, error)
	Health(ctx context.Context, tenantID string) (svc.HealthSummary, error)
}

// Handler is the Console cohort sync handler.
type Handler struct {
	service        service
	webhookBaseURL string
}

// NewHandler builds a Console cohort sync handler.
// webhookBaseURL is the public-facing base URL (e.g. "https://attune.example.com")
// used to construct complete webhook URLs that operators can copy to their provider.
func NewHandler(service service, webhookBaseURL string) *Handler {
	return ptrext.Of(Handler{service: service, webhookBaseURL: webhookBaseURL})
}

// ---------- Sources ----------

// ListSources returns all cohort sources for the tenant.
func (h *Handler) ListSources(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListCohortSourcesRequest,
) (dispatcher.Result[*attunev1.ListCohortSourcesResponse], error) {
	sources, err := h.service.ListSources(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.ListCohortSourcesResponse](ctx, "ListSources", err)
	}
	items := make([]*attunev1.CohortSource, 0, len(sources))
	for i := range sources {
		items = append(items, h.sourceToProto(sources[i]))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListCohortSourcesResponse{Sources: items}))
}

// GetSource returns a single cohort source.
func (h *Handler) GetSource(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetCohortSourceRequest,
) (dispatcher.Result[*attunev1.CohortSource], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CohortSource](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source id")
	}
	source, err := h.service.GetSource(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.CohortSource](ctx, "GetSource", err)
	}
	return dispatcher.OK(h.sourceToProto(ptrext.Indirect(source)))
}

// CreateSource creates a new cohort source.
func (h *Handler) CreateSource(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateCohortSourceRequest,
) (dispatcher.Result[*attunev1.CohortSource], error) {
	src, err := h.service.CreateSource(ctx, svc.CreateSourceInput{
		TenantID:       ctx.Auth.TenantID,
		Provider:       req.GetProvider(),
		Name:           req.GetName(),
		AuthType:       req.GetAuthType(),
		Credential:     req.GetCredential(),
		PullCredential: req.GetPullCredential(),
		WebhookSecret:  req.GetWebhookSecret(),
		BaseURL:        req.GetBaseUrl(),
		ProviderConfig: req.GetProviderConfigJson(),
		Enabled:        req.GetEnabled(),
		Actor:          svc.Actor{Type: ctx.Auth.UserType, ID: ctx.Auth.UserID},
		AuditActor:     auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.CohortSource](ctx, "CreateSource", err)
	}
	return dispatcher.OK(h.sourceToProto(ptrext.Indirect(src)))
}

// DeleteSource deletes a cohort source.
func (h *Handler) DeleteSource(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteCohortSourceRequest,
) (dispatcher.Result[*attunev1.DeleteCohortSourceResponse], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.DeleteCohortSourceResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source id")
	}
	if err := h.service.DeleteSource(ctx, ctx.Auth.TenantID, id,
		svc.Actor{Type: ctx.Auth.UserType, ID: ctx.Auth.UserID},
		auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	); err != nil {
		return mapError[*attunev1.DeleteCohortSourceResponse](ctx, "DeleteSource", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteCohortSourceResponse{}))
}

// UpdateSource updates a cohort source.
func (h *Handler) UpdateSource(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateCohortSourceRequest,
) (dispatcher.Result[*attunev1.CohortSource], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CohortSource](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source id")
	}
	updated, err := h.service.UpdateSource(ctx, svc.UpdateSourceInput{
		TenantID:       ctx.Auth.TenantID,
		ID:             id,
		Name:           req.Name,
		Enabled:        req.Enabled,
		Credential:     req.Credential,
		PullCredential: req.PullCredential,
		BaseURL:        req.BaseUrl,
		ProviderConfig: req.ProviderConfigJson,
		Actor:          svc.Actor{Type: ctx.Auth.UserType, ID: ctx.Auth.UserID},
		AuditActor:     auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.CohortSource](ctx, "UpdateSource", err)
	}
	return dispatcher.OK(h.sourceToProto(ptrext.Indirect(updated)))
}

// TestSource tests connectivity of a cohort source.
func (h *Handler) TestSource(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.TestCohortSourceRequest,
) (dispatcher.Result[*attunev1.TestCohortSourceResponse], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.TestCohortSourceResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source id")
	}
	result, testErr := h.service.TestSource(ctx, ctx.Auth.TenantID, id,
		auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()))
	if testErr != nil {
		// If the test could not run at all (e.g., source not found, credential
		// decrypt failed), return a proper error response instead of the ok/error
		// shape. Only return TestCohortSourceResponse when the test ran.
		if result.Error == "" {
			return mapError[*attunev1.TestCohortSourceResponse](ctx, "TestSource", testErr)
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.TestCohortSourceResponse{
		Ok:    result.OK,
		Error: result.Error,
	}))
}

// ---------- Cohorts ----------

// ListCohorts returns cohorts, optionally filtered by source.
func (h *Handler) ListCohorts(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListCohortsRequest,
) (dispatcher.Result[*attunev1.ListCohortsResponse], error) {
	// Build source lookup for enriching cohort with source_name/source_provider.
	sources, err := h.service.ListSources(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.ListCohortsResponse](ctx, "ListCohorts", err)
	}
	sourceMap := buildSourceMap(sources)

	var cohorts []repo.Cohort
	if sid := strings.TrimSpace(req.GetSourceId()); sid != "" {
		sourceID, parseErr := uuid.Parse(sid)
		if parseErr != nil {
			return dispatcher.Fail[*attunev1.ListCohortsResponse](
				http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid source_id")
		}
		cohorts, err = h.service.ListCohorts(ctx, ctx.Auth.TenantID, sourceID)
	} else {
		cohorts, err = h.service.ListAllCohorts(ctx, ctx.Auth.TenantID)
	}
	if err != nil {
		return internalError[*attunev1.ListCohortsResponse](ctx, "ListCohorts", err)
	}
	items := make([]*attunev1.Cohort, 0, len(cohorts))
	for i := range cohorts {
		items = append(items, cohortToProto(cohorts[i], sourceMap))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListCohortsResponse{Cohorts: items}))
}

// GetCohort returns a single cohort.
func (h *Handler) GetCohort(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.GetCohortRequest,
) (dispatcher.Result[*attunev1.Cohort], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.Cohort](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid cohort id")
	}
	cohort, err := h.service.GetCohort(ctx, ctx.Auth.TenantID, id)
	if err != nil {
		return mapError[*attunev1.Cohort](ctx, "GetCohort", err)
	}
	// Fetch the source to populate source_name/source_provider.
	source, _ := h.service.GetSource(ctx, ctx.Auth.TenantID, cohort.CohortSourceID)
	sourceMap := make(map[uuid.UUID]sourceInfo)
	if source != nil {
		sourceMap[source.ID] = sourceInfo{Name: source.Name, Provider: source.Provider}
	}
	return dispatcher.OK(cohortToProto(ptrext.Indirect(cohort), sourceMap))
}

// UpdateCohort updates mutable cohort fields.
func (h *Handler) UpdateCohort(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateCohortRequest,
) (dispatcher.Result[*attunev1.Cohort], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.Cohort](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid cohort id")
	}
	updated, err := h.service.UpdateCohort(ctx, svc.UpdateCohortInput{
		TenantID:     ctx.Auth.TenantID,
		ID:           id,
		Name:         req.Name,
		Description:  req.Description,
		StaleTTLDays: intPtrFromInt32(req.StaleTtlDays),
		Enabled:      req.Enabled,
		Actor:        svc.Actor{Type: ctx.Auth.UserType, ID: ctx.Auth.UserID},
		AuditActor:   auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	})
	if err != nil {
		return mapError[*attunev1.Cohort](ctx, "UpdateCohort", err)
	}
	return dispatcher.OK(cohortToProto(ptrext.Indirect(updated), nil))
}

// ListMembers returns active members of a cohort.
func (h *Handler) ListMembers(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListCohortMembersRequest,
) (dispatcher.Result[*attunev1.ListCohortMembersResponse], error) {
	cohortID, err := uuid.Parse(req.GetCohortId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListCohortMembersResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid cohort_id")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 50
	}
	members, err := h.service.ListMembers(ctx, ctx.Auth.TenantID, cohortID, limit)
	if err != nil {
		return internalError[*attunev1.ListCohortMembersResponse](ctx, "ListMembers", err)
	}
	items := make([]*attunev1.CohortMembership, 0, len(members))
	for i := range members {
		items = append(items, memberToProto(members[i]))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListCohortMembersResponse{Members: items}))
}

// SyncCohort triggers an on-demand pull for a cohort.
func (h *Handler) SyncCohort(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SyncCohortRequest,
) (dispatcher.Result[*attunev1.SyncCohortResponse], error) {
	id, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.SyncCohortResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid cohort id")
	}
	result, err := h.service.SyncNow(ctx, ctx.Auth.TenantID, id,
		svc.Actor{Type: ctx.Auth.UserType, ID: ctx.Auth.UserID},
		auditlogsvc.ActorFromRequest(ctx.Auth.UserType, ctx.Auth.UserID, ctx.Request()),
	)
	if err != nil {
		return mapError[*attunev1.SyncCohortResponse](ctx, "SyncCohort", err)
	}
	return dispatcher.OK(ptrext.Of(attunev1.SyncCohortResponse{
		Run: runToProto(result.Run),
	}))
}

// ListSyncRuns returns sync run history for a cohort.
func (h *Handler) ListSyncRuns(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.ListCohortSyncRunsRequest,
) (dispatcher.Result[*attunev1.ListCohortSyncRunsResponse], error) {
	cohortID, err := uuid.Parse(req.GetCohortId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListCohortSyncRunsResponse](
			http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid cohort_id")
	}
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = 20
	}
	runs, err := h.service.ListRuns(ctx, ctx.Auth.TenantID, cohortID, limit)
	if err != nil {
		return internalError[*attunev1.ListCohortSyncRunsResponse](ctx, "ListSyncRuns", err)
	}
	items := make([]*attunev1.CohortSyncRun, 0, len(runs))
	for i := range runs {
		items = append(items, runToProto(runs[i]))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListCohortSyncRunsResponse{Runs: items}))
}

// ---------- Health ----------

// Health returns cohort sync health for the tenant.
func (h *Handler) Health(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.GetCohortSyncHealthRequest,
) (dispatcher.Result[*attunev1.CohortSyncHealth], error) {
	health, err := h.service.Health(ctx, ctx.Auth.TenantID)
	if err != nil {
		return internalError[*attunev1.CohortSyncHealth](ctx, "Health", err)
	}
	out := ptrext.Of(attunev1.CohortSyncHealth{
		SourceCount:        int32(health.SourceCount),
		ActiveSources:      int32(health.ActiveSources),
		ErrorSources:       int32(health.ErrorSources),
		DisabledSources:    int32(health.DisabledSources),
		CohortCount:        int32(health.CohortCount),
		TotalActiveMembers: int32(health.TotalActiveMembers),
		SyncsLast_24H:      int32(health.SyncsLast24h),
	})
	if health.LastSyncAt != nil {
		out.LastSyncAt = timestamppb.New(ptrext.Indirect(health.LastSyncAt))
	}
	return dispatcher.OK(out)
}

// ---------- converters ----------

type sourceInfo struct {
	Name     string
	Provider string
}

func buildSourceMap(sources []repo.Source) map[uuid.UUID]sourceInfo {
	m := make(map[uuid.UUID]sourceInfo, len(sources))
	for _, s := range sources {
		m[s.ID] = sourceInfo{Name: s.Name, Provider: s.Provider}
	}
	return m
}

func (h *Handler) sourceToProto(s repo.Source) *attunev1.CohortSource {
	out := ptrext.Of(attunev1.CohortSource{
		Id:          s.ID.String(),
		Provider:    s.Provider,
		Name:        s.Name,
		AuthType:    s.AuthType,
		BaseUrl:     s.BaseURL,
		Enabled:     s.Enabled,
		Status:      s.Status,
		LastError:   s.LastError,
		WebhookUrl:  buildWebhookURLs(s, h.webhookBaseURL)[0],
		WebhookUrls: buildWebhookURLs(s, h.webhookBaseURL),
		CreatedAt:   timestamppb.New(s.CreatedAt),
		UpdatedAt:   timestamppb.New(s.UpdatedAt),
	})
	if s.LastSyncAt != nil {
		out.LastSyncAt = timestamppb.New(ptrext.Indirect(s.LastSyncAt))
	}
	return out
}

func buildWebhookURLs(s repo.Source, base string) []string {
	prefix := base + "/v1/cohort-sync"
	sid := s.ID.String()
	switch s.Provider {
	case "amplitude":
		return []string{
			prefix + "/amplitude/" + s.TenantID + "/" + sid + "/create",
			prefix + "/amplitude/" + s.TenantID + "/" + sid + "/add",
			prefix + "/amplitude/" + s.TenantID + "/" + sid + "/remove",
		}
	case "mixpanel":
		return []string{prefix + "/mixpanel/" + s.TenantID + "/" + sid}
	default:
		return []string{prefix + "/" + s.Provider + "/" + s.TenantID + "/" + sid}
	}
}

func cohortToProto(c repo.Cohort, sourceMap map[uuid.UUID]sourceInfo) *attunev1.Cohort {
	out := ptrext.Of(attunev1.Cohort{
		Id:               c.ID.String(),
		CohortSourceId:   c.CohortSourceID.String(),
		ExternalCohortId: c.ExternalCohortID,
		Name:             c.Name,
		Description:      c.Description,
		StaleTtlDays:     int32(c.StaleTTLDays),
		MemberCount:      int32(c.MemberCount),
		Enabled:          c.Enabled,
		LastError:        c.LastError,
		CreatedAt:        timestamppb.New(c.CreatedAt),
		UpdatedAt:        timestamppb.New(c.UpdatedAt),
	})
	if c.LastSyncedAt != nil {
		out.LastSyncedAt = timestamppb.New(ptrext.Indirect(c.LastSyncedAt))
	}
	if si, ok := sourceMap[c.CohortSourceID]; ok {
		out.SourceName = si.Name
		out.SourceProvider = si.Provider
	}
	return out
}

func memberToProto(m repo.Membership) *attunev1.CohortMembership {
	return ptrext.Of(attunev1.CohortMembership{
		Id:             m.ID.String(),
		ExternalUserId: m.ExternalUserID,
		Email:          m.Email,
		DisplayName:    m.DisplayName,
		JoinedAt:       timestamppb.New(m.JoinedAt),
		LastSeenAt:     timestamppb.New(m.LastSeenAt),
	})
}

func runToProto(r repo.SyncRun) *attunev1.CohortSyncRun {
	out := ptrext.Of(attunev1.CohortSyncRun{
		Id:             r.ID.String(),
		CohortId:       r.CohortID.String(),
		Trigger:        r.Trigger,
		Status:         r.Status,
		MembersAdded:   int32(r.MembersAdded),
		MembersRemoved: int32(r.MembersRemoved),
		MembersTotal:   int32(r.MembersTotal),
		ErrorMessage:   r.ErrorMessage,
		StartedAt:      timestamppb.New(r.StartedAt),
		CreatedAt:      timestamppb.New(r.CreatedAt),
	})
	if r.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(ptrext.Indirect(r.FinishedAt))
	}
	return out
}

func intPtrFromInt32(p *int32) *int {
	if p == nil {
		return nil
	}
	v := int(ptrext.Indirect(p))
	return ptrext.Of(v)
}

// ---------- error mapping ----------

func mapError[T proto.Message](ctx context.Context, where string, err error) (dispatcher.Result[T], error) {
	switch {
	case errors.Is(err, svc.ErrValidation):
		return dispatcher.Fail[T](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	case errors.Is(err, repo.ErrSourceNotFound), errors.Is(err, repo.ErrCohortNotFound):
		return dispatcher.Fail[T](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, err.Error())
	case errors.Is(err, repo.ErrConflict):
		return dispatcher.Fail[T](http.StatusConflict, attunev1.ErrorCode_CONFLICT, err.Error())
	case cohortsync.IsUnavailableError(err):
		return dispatcher.Fail[T](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
	default:
		return internalError[T](ctx, where, err)
	}
}

func internalError[T proto.Message](ctx context.Context, where string, err error) (dispatcher.Result[T], error) {
	logext.Errorf(ctx, "[console.CohortSyncHandler.%s] internal error: %s", where, err.Error())
	return dispatcher.Fail[T](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "cohort sync operation failed")
}
