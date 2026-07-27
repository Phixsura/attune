// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	core "github.com/Phixsura/attune/internal/externalsync"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const (
	ProviderInstallationGradeFullApp       = "full_app"
	ProviderInstallationGradeLimitedApp    = "limited_app"
	ProviderInstallationGradeTokenFallback = "token_fallback"
	ProviderInstallationGradeManualSetup   = "manual_setup"
	ProviderInstallationGradeBlocked       = "blocked"
)

func (s *Service) ListProviderInstallations(ctx context.Context, tenantID string) ([]repo.ProviderInstallation, error) {
	return s.repo.ListProviderInstallations(ctx, strings.TrimSpace(tenantID))
}

func (s *Service) CreateProviderInstallation(ctx context.Context, in CreateProviderInstallationInput) (*repo.ProviderInstallation, []repo.ProviderInstallationResource, error) {
	normalized, resources, err := normalizeProviderInstallationInput(in)
	if err != nil {
		return nil, nil, err
	}
	row, resourceRows, err := s.repo.CreateProviderInstallation(ctx, repo.ProviderInstallationWithResources{
		Installation: normalized,
		Resources:    resources,
	})
	if err != nil {
		return nil, nil, err
	}
	s.record(ctx, in.AuditActor, normalized.TenantID, "external_provider_installation.create",
		"external_provider_installation", row.ID.String(), "Created external provider installation",
		nil, providerInstallationAudit(row, resourceRows))
	return row, resourceRows, nil
}

func (s *Service) DeleteProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) error {
	tenantID = strings.TrimSpace(tenantID)
	actor.ID = strings.TrimSpace(actor.ID)
	if actor.ID == "" {
		return fmt.Errorf("%w: actor is required", ErrValidation)
	}
	before, err := s.repo.GetProviderInstallation(ctx, tenantID, id)
	if err != nil {
		return err
	}
	resources, err := s.repo.ListProviderInstallationResources(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteProviderInstallation(ctx, tenantID, id, actor.ID); err != nil {
		return err
	}
	s.record(ctx, auditActor, tenantID, "external_provider_installation.delete",
		"external_provider_installation", id.String(), "Deleted external provider installation",
		providerInstallationAudit(before, resources), nil)
	return nil
}

func (s *Service) ListProviderInstallationResources(ctx context.Context, tenantID string, installationID uuid.UUID) ([]repo.ProviderInstallationResource, error) {
	return s.repo.ListProviderInstallationResources(ctx, strings.TrimSpace(tenantID), installationID)
}

func (s *Service) SelectProviderInstallationResources(ctx context.Context, in SelectProviderInstallationResourcesInput) ([]repo.ProviderInstallationResource, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if in.Actor.ID == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	before, err := s.repo.ListProviderInstallationResources(ctx, in.TenantID, in.InstallationID)
	if err != nil {
		return nil, err
	}
	selected, err := s.repo.SelectProviderInstallationResources(ctx, in.TenantID, in.InstallationID, uniqueUUIDs(in.ResourceIDs), in.Actor.ID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, in.AuditActor, in.TenantID, "external_provider_installation.resources_select",
		"external_provider_installation", in.InstallationID.String(), "Selected external provider installation resources",
		providerResourcesAudit(before), providerResourcesAudit(selected))
	return selected, nil
}

func (s *Service) QualifyProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID, actor Actor, auditActor auditlogsvc.Actor) (ProviderInstallationQualificationResult, error) {
	tenantID = strings.TrimSpace(tenantID)
	actor.ID = strings.TrimSpace(actor.ID)
	if actor.ID == "" {
		return ProviderInstallationQualificationResult{}, fmt.Errorf("%w: actor is required", ErrValidation)
	}
	installation, err := s.repo.GetProviderInstallation(ctx, tenantID, id)
	if err != nil {
		return ProviderInstallationQualificationResult{}, err
	}
	resources, err := s.repo.ListProviderInstallationResources(ctx, tenantID, id)
	if err != nil {
		return ProviderInstallationQualificationResult{}, err
	}
	result := qualifyProviderInstallation(ptrext.Indirect(installation), resources)
	profile := providerInstallationCapabilityProfile(result, resources)
	status, lastError := providerInstallationQualificationStatus(result)
	updated, err := s.repo.UpdateProviderInstallationQualification(ctx, tenantID, id, status, lastError, []byte(profile), actor.ID)
	if err != nil {
		return ProviderInstallationQualificationResult{}, err
	}
	result.Installation = ptrext.Indirect(updated)
	s.auditProviderInstallationQualification(ctx, auditActor, tenantID, result, resources)
	return result, nil
}

func normalizeProviderInstallationInput(in CreateProviderInstallationInput) (repo.ProviderInstallation, []repo.ProviderInstallationResource, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Provider = strings.TrimSpace(strings.ToLower(in.Provider))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.InstallationKind = normalizeInstallationKind(in.InstallationKind)
	in.ExternalInstallationID = truncateString(in.ExternalInstallationID, 200)
	in.AccountLogin = truncateString(in.AccountLogin, 200)
	in.AccountID = truncateString(in.AccountID, 200)
	in.AccountURL = truncateString(in.AccountURL, 500)
	in.BaseURL = truncateString(in.BaseURL, 500)
	in.ResourceSelection = normalizeResourceSelection(in.ResourceSelection, len(in.Resources))
	in.Actor.ID = strings.TrimSpace(in.Actor.ID)
	if err := validateProviderInstallationShape(in); err != nil {
		return repo.ProviderInstallation{}, nil, err
	}
	permissions, err := normalizeJSONObject(in.PermissionsJSON, "permissions_json")
	if err != nil {
		return repo.ProviderInstallation{}, nil, err
	}
	profile, err := normalizeJSONObject(in.CapabilityProfileJSON, "capability_profile_json")
	if err != nil {
		return repo.ProviderInstallation{}, nil, err
	}
	id := uuid.New()
	resources, err := normalizeProviderInstallationResources(in, id)
	if err != nil {
		return repo.ProviderInstallation{}, nil, err
	}
	return repo.ProviderInstallation{
		ID:                     id,
		TenantID:               in.TenantID,
		Provider:               in.Provider,
		DisplayName:            in.DisplayName,
		InstallationKind:       in.InstallationKind,
		Status:                 installationStatusForInput(in),
		ExternalInstallationID: in.ExternalInstallationID,
		AccountLogin:           in.AccountLogin,
		AccountID:              in.AccountID,
		AccountURL:             in.AccountURL,
		BaseURL:                in.BaseURL,
		Permissions:            []byte(permissions),
		CapabilityProfile:      []byte(profile),
		ResourceSelection:      in.ResourceSelection,
		QualificationStatus:    repo.TestStatusUntested,
		CreatedBy:              in.Actor.ID,
		UpdatedBy:              in.Actor.ID,
	}, resources, nil
}

func validateProviderInstallationShape(in CreateProviderInstallationInput) error {
	if in.TenantID == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if err := core.ValidateProviderToken(in.Provider); err != nil {
		return fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if in.DisplayName == "" || len(in.DisplayName) > 200 || !utf8.ValidString(in.DisplayName) {
		return fmt.Errorf("%w: display_name must be 1..200 valid UTF-8 bytes", ErrValidation)
	}
	if in.InstallationKind == "" {
		return fmt.Errorf("%w: invalid installation_kind", ErrValidation)
	}
	if in.ResourceSelection == "" {
		return fmt.Errorf("%w: invalid resource_selection", ErrValidation)
	}
	if in.Actor.ID == "" {
		return fmt.Errorf("%w: actor is required", ErrValidation)
	}
	return nil
}

func normalizeProviderInstallationResources(in CreateProviderInstallationInput, installationID uuid.UUID) ([]repo.ProviderInstallationResource, error) {
	out := make([]repo.ProviderInstallationResource, 0, len(in.Resources))
	for _, resource := range in.Resources {
		row, err := normalizeProviderInstallationResource(in, installationID, resource)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func normalizeProviderInstallationResource(in CreateProviderInstallationInput, installationID uuid.UUID, resource ProviderInstallationResourceInput) (repo.ProviderInstallationResource, error) {
	resource.ResourceType = normalizeResourceType(resource.ResourceType)
	resource.ResourceKey = truncateString(resource.ResourceKey, 512)
	resource.DisplayName = strings.TrimSpace(resource.DisplayName)
	resource.ExternalResourceID = truncateString(resource.ExternalResourceID, 200)
	resource.HTMLURL = truncateString(resource.HTMLURL, 500)
	resource.Status = normalizeResourceStatus(resource.Status)
	permissions, err := normalizeJSONObject(resource.PermissionsJSON, "resource.permissions_json")
	if err != nil {
		return repo.ProviderInstallationResource{}, err
	}
	if resource.ResourceType == "" || resource.ResourceKey == "" || resource.Status == "" {
		return repo.ProviderInstallationResource{}, fmt.Errorf("%w: resource_type, resource_key, and status are required", ErrValidation)
	}
	if resource.DisplayName == "" {
		resource.DisplayName = resource.ResourceKey
	}
	return repo.ProviderInstallationResource{
		ID:                 uuid.New(),
		TenantID:           in.TenantID,
		InstallationID:     installationID,
		Provider:           in.Provider,
		ResourceType:       resource.ResourceType,
		ExternalResourceID: resource.ExternalResourceID,
		ResourceKey:        resource.ResourceKey,
		DisplayName:        resource.DisplayName,
		HTMLURL:            resource.HTMLURL,
		Selected:           resource.Selected,
		Status:             resource.Status,
		Permissions:        []byte(permissions),
	}, nil
}

func qualifyProviderInstallation(installation repo.ProviderInstallation, resources []repo.ProviderInstallationResource) ProviderInstallationQualificationResult {
	result := ptrext.Of(ProviderInstallationQualificationResult{Installation: installation, Ready: true})
	if _, ok := core.Lookup(installation.Provider); ok {
		result.addCheck("provider_registered", QualificationStatusOK, "Provider adapter is registered",
			map[string]any{"provider": installation.Provider})
	} else {
		result.addCheck("provider_registered", QualificationStatusFailed, "Provider adapter is not registered",
			map[string]any{"provider": installation.Provider})
	}
	addInstallationKindCheck(result, installation)
	addInstallationPermissionCheck(result, installation)
	addInstallationResourceCheck(result, installation, resources)
	result.Grade = providerInstallationGrade(ptrext.Indirect(result), installation)
	return ptrext.Indirect(result)
}

func addInstallationKindCheck(result *ProviderInstallationQualificationResult, installation repo.ProviderInstallation) {
	switch installation.InstallationKind {
	case repo.InstallationKindGitHubApp, repo.InstallationKindOAuthApp:
		if installation.ExternalInstallationID == "" {
			result.addCheck("installation_identity", QualificationStatusFailed, "App installation is missing an external installation id", nil)
			return
		}
		result.addCheck("installation_identity", QualificationStatusOK, "App installation identity is present",
			map[string]any{"external_installation_id": installation.ExternalInstallationID})
	case repo.InstallationKindToken:
		result.addCheck("installation_identity", QualificationStatusWarning, "Token fallback is configured without app installation isolation", nil)
	case repo.InstallationKindManual:
		result.addCheck("installation_identity", QualificationStatusWarning, "Manual setup requires operator-managed provider permissions", nil)
	default:
		result.addCheck("installation_identity", QualificationStatusFailed, "Installation kind is invalid", nil)
	}
}

func addInstallationPermissionCheck(result *ProviderInstallationQualificationResult, installation repo.ProviderInstallation) {
	permissions := parsePermissionObject(installation.Permissions)
	if installation.Provider != "github" {
		result.addCheck("permission_profile", QualificationStatusWarning, "Provider-specific permission profile is not enforced yet",
			map[string]any{"provider": installation.Provider})
		return
	}
	missing := []string{}
	if !permissionAllows(permissions, "metadata", "read", "write", "admin") {
		missing = append(missing, "metadata:read")
	}
	if !permissionAllows(permissions, "issues", "write", "admin") {
		missing = append(missing, "issues:write")
	}
	if len(missing) > 0 {
		result.addCheck("permission_profile", QualificationStatusFailed, "GitHub installation is missing required issue-sync permissions",
			map[string]any{"missing": missing})
		return
	}
	result.addCheck("permission_profile", QualificationStatusOK, "GitHub installation exposes required issue-sync permissions", nil)
}

func addInstallationResourceCheck(result *ProviderInstallationQualificationResult, installation repo.ProviderInstallation, resources []repo.ProviderInstallationResource) {
	selected := selectedProviderResources(resources)
	if installation.ResourceSelection == repo.ResourceSelectionAll {
		result.addCheck("resource_selection", QualificationStatusOK, "Installation grants all provider resources",
			map[string]any{"visible_resources": len(resources)})
		return
	}
	if len(selected) == 0 {
		result.addCheck("resource_selection", QualificationStatusFailed, "No provider resources are selected for sync", nil)
		return
	}
	result.addCheck("resource_selection", QualificationStatusOK, "Installation has selected provider resources",
		map[string]any{"selected_resources": len(selected), "visible_resources": len(resources)})
}

func providerInstallationGrade(result ProviderInstallationQualificationResult, installation repo.ProviderInstallation) string {
	if !result.Ready {
		return ProviderInstallationGradeBlocked
	}
	switch installation.InstallationKind {
	case repo.InstallationKindGitHubApp, repo.InstallationKindOAuthApp:
		if hasQualificationWarnings(result.Checks) {
			return ProviderInstallationGradeLimitedApp
		}
		return ProviderInstallationGradeFullApp
	case repo.InstallationKindToken:
		return ProviderInstallationGradeTokenFallback
	default:
		return ProviderInstallationGradeManualSetup
	}
}

func providerInstallationCapabilityProfile(result ProviderInstallationQualificationResult, resources []repo.ProviderInstallationResource) string {
	checkCounts := map[string]int{}
	for _, check := range result.Checks {
		checkCounts[check.Status]++
	}
	return mustMarshalJSONObject(map[string]any{
		"grade":              result.Grade,
		"ready":              result.Ready,
		"check_counts":       checkCounts,
		"resource_count":     len(resources),
		"selected_resources": len(selectedProviderResources(resources)),
	})
}

func providerInstallationQualificationStatus(result ProviderInstallationQualificationResult) (string, string) {
	failures := []string{}
	warnings := 0
	for _, check := range result.Checks {
		if check.Status == QualificationStatusFailed {
			failures = append(failures, check.Summary)
		}
		if check.Status == QualificationStatusWarning {
			warnings++
		}
	}
	if len(failures) > 0 {
		return QualificationStatusFailed, strings.Join(failures, "; ")
	}
	if warnings > 0 {
		return QualificationStatusWarning, ""
	}
	return QualificationStatusOK, ""
}

func (r *ProviderInstallationQualificationResult) addCheck(name, status, summary string, detail map[string]any) {
	if status == QualificationStatusFailed {
		r.Ready = false
	}
	r.Checks = append(r.Checks, QualificationCheck{
		Name:       name,
		Status:     status,
		Summary:    summary,
		DetailJSON: qualificationDetail(detail),
	})
}

func normalizeInstallationKind(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.InstallationKindGitHubApp:
		return repo.InstallationKindGitHubApp
	case repo.InstallationKindOAuthApp:
		return repo.InstallationKindOAuthApp
	case repo.InstallationKindToken:
		return repo.InstallationKindToken
	case "", repo.InstallationKindManual:
		return repo.InstallationKindManual
	default:
		return ""
	}
}

func normalizeResourceSelection(raw string, resourceCount int) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case repo.ResourceSelectionAll:
		return repo.ResourceSelectionAll
	case repo.ResourceSelectionNone:
		return repo.ResourceSelectionNone
	case repo.ResourceSelectionSelected:
		return repo.ResourceSelectionSelected
	case "":
		if resourceCount == 0 {
			return repo.ResourceSelectionNone
		}
		return repo.ResourceSelectionSelected
	default:
		return ""
	}
}

func normalizeResourceType(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", repo.ResourceTypeRepository:
		return repo.ResourceTypeRepository
	case repo.ResourceTypeProject:
		return repo.ResourceTypeProject
	case repo.ResourceTypeWorkspace:
		return repo.ResourceTypeWorkspace
	case repo.ResourceTypeOrganization:
		return repo.ResourceTypeOrganization
	default:
		return ""
	}
}

func normalizeResourceStatus(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", repo.ResourceStatusActive:
		return repo.ResourceStatusActive
	case repo.ResourceStatusRemoved:
		return repo.ResourceStatusRemoved
	case repo.ResourceStatusUnknown:
		return repo.ResourceStatusUnknown
	default:
		return ""
	}
}

func installationStatusForInput(in CreateProviderInstallationInput) string {
	switch in.InstallationKind {
	case repo.InstallationKindGitHubApp, repo.InstallationKindOAuthApp:
		if in.ExternalInstallationID == "" {
			return repo.InstallationStatusPending
		}
		return repo.InstallationStatusActive
	case repo.InstallationKindToken, repo.InstallationKindManual:
		return repo.InstallationStatusLimited
	default:
		return repo.InstallationStatusPending
	}
}

func parsePermissionObject(raw []byte) map[string]any {
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return map[string]any{}
	}
	return object
}

func permissionAllows(permissions map[string]any, key string, allowed ...string) bool {
	value, ok := permissions[key]
	if !ok {
		return false
	}
	allowedSet := stringSet(allowed)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return allowedSet[strings.TrimSpace(strings.ToLower(typed))]
	default:
		return false
	}
}

func hasQualificationWarnings(checks []QualificationCheck) bool {
	for _, check := range checks {
		if check.Status == QualificationStatusWarning {
			return true
		}
	}
	return false
}

func selectedProviderResources(resources []repo.ProviderInstallationResource) []repo.ProviderInstallationResource {
	out := []repo.ProviderInstallationResource{}
	for _, resource := range resources {
		if resource.Selected && resource.Status == repo.ResourceStatusActive {
			out = append(out, resource)
		}
	}
	return out
}

func providerInstallationAudit(row *repo.ProviderInstallation, resources []repo.ProviderInstallationResource) map[string]any {
	if row == nil {
		return nil
	}
	return map[string]any{
		"id":                       row.ID.String(),
		"provider":                 row.Provider,
		"display_name":             row.DisplayName,
		"installation_kind":        row.InstallationKind,
		"status":                   row.Status,
		"resource_selection":       row.ResourceSelection,
		"qualification_status":     row.QualificationStatus,
		"external_installation_id": row.ExternalInstallationID,
		"account_login":            row.AccountLogin,
		"resource_count":           len(resources),
		"selected_resources":       len(selectedProviderResources(resources)),
	}
}

func providerResourcesAudit(resources []repo.ProviderInstallationResource) map[string]any {
	out := make([]map[string]any, 0, len(resources))
	for _, resource := range resources {
		out = append(out, map[string]any{
			"id":            resource.ID.String(),
			"resource_key":  resource.ResourceKey,
			"resource_type": resource.ResourceType,
			"selected":      resource.Selected,
			"status":        resource.Status,
		})
	}
	return map[string]any{"resources": out}
}

func (s *Service) auditProviderInstallationQualification(
	ctx context.Context,
	actor auditlogsvc.Actor,
	tenantID string,
	result ProviderInstallationQualificationResult,
	resources []repo.ProviderInstallationResource,
) {
	counts := map[string]int{}
	checks := make([]map[string]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		counts[check.Status]++
		checks = append(checks, map[string]string{"name": check.Name, "status": check.Status})
	}
	s.record(ctx, actor, tenantID, "external_provider_installation.qualify",
		"external_provider_installation", result.Installation.ID.String(),
		"Qualified external provider installation", nil,
		map[string]any{
			"provider":           result.Installation.Provider,
			"ready":              result.Ready,
			"grade":              result.Grade,
			"check_counts":       counts,
			"check_results":      checks,
			"resource_count":     len(resources),
			"selected_resources": len(selectedProviderResources(resources)),
		})
}
