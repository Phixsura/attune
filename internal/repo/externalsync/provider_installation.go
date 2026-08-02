// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

func (r *Repo) ListProviderInstallations(ctx context.Context, tenantID string) ([]ProviderInstallation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, provider, display_name, installation_kind, status,
		       external_installation_id, account_login, account_id, account_url, base_url,
		       permissions, capability_profile, resource_selection, qualification_status,
		       last_qualified_at, last_error, created_by, updated_by, created_at, updated_at
		  FROM external_provider_installations
		 WHERE tenant_id = $1
		   AND deleted_at IS NULL
		 ORDER BY provider ASC, display_name ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list external provider installations: %w", err)
	}
	defer rows.Close()
	var out []ProviderInstallation
	for rows.Next() {
		row, err := scanProviderInstallation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) GetProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID) (*ProviderInstallation, error) {
	row, err := scanProviderInstallation(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, provider, display_name, installation_kind, status,
		       external_installation_id, account_login, account_id, account_url, base_url,
		       permissions, capability_profile, resource_selection, qualification_status,
		       last_qualified_at, last_error, created_by, updated_by, created_at, updated_at
		  FROM external_provider_installations
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL`, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInstallationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get external provider installation: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) CreateProviderInstallation(ctx context.Context, in ProviderInstallationWithResources) (*ProviderInstallation, []ProviderInstallationResource, error) {
	var installation ProviderInstallation
	var resources []ProviderInstallationResource
	err := secretlock.WithTx(ctx, r.secretPool, true, func(ctx context.Context, tx secretlock.Tx) error {
		var scanErr error
		installation, scanErr = scanProviderInstallation(tx.QueryRow(ctx, `
			INSERT INTO external_provider_installations
			  (id, tenant_id, provider, display_name, installation_kind, status,
			   external_installation_id, account_login, account_id, account_url, base_url,
			   permissions, capability_profile, resource_selection, qualification_status,
			   last_qualified_at, last_error, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        $12::jsonb, $13::jsonb, $14, $15, $16, $17, $18, $19)
			RETURNING id, tenant_id, provider, display_name, installation_kind, status,
			          external_installation_id, account_login, account_id, account_url, base_url,
			          permissions, capability_profile, resource_selection, qualification_status,
			          last_qualified_at, last_error, created_by, updated_by, created_at, updated_at`,
			in.Installation.ID, in.Installation.TenantID, in.Installation.Provider,
			in.Installation.DisplayName, in.Installation.InstallationKind, in.Installation.Status,
			in.Installation.ExternalInstallationID, in.Installation.AccountLogin,
			in.Installation.AccountID, in.Installation.AccountURL, in.Installation.BaseURL,
			string(in.Installation.Permissions), string(in.Installation.CapabilityProfile),
			in.Installation.ResourceSelection, in.Installation.QualificationStatus,
			in.Installation.LastQualifiedAt, in.Installation.LastError,
			in.Installation.CreatedBy, in.Installation.UpdatedBy))
		if scanErr != nil {
			return scanErr
		}
		resources, scanErr = upsertProviderInstallationResources(ctx, tx, in.Resources)
		return scanErr
	})
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, nil, fmt.Errorf("%w: provider installation already exists", ErrConflict)
		}
		return nil, nil, fmt.Errorf("create external provider installation: %w", err)
	}
	return ptrext.Of(installation), resources, nil
}

func (r *Repo) UpdateProviderInstallationQualification(
	ctx context.Context,
	tenantID string,
	id uuid.UUID,
	status string,
	lastError string,
	capabilityProfile []byte,
	actor string,
) (*ProviderInstallation, error) {
	row, err := scanProviderInstallation(r.pool.QueryRow(ctx, `
		UPDATE external_provider_installations
		   SET qualification_status = $3,
		       last_qualified_at = NOW(),
		       last_error = $4,
		       capability_profile = $5::jsonb,
		       updated_by = $6,
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND id = $2
		   AND deleted_at IS NULL
		RETURNING id, tenant_id, provider, display_name, installation_kind, status,
		          external_installation_id, account_login, account_id, account_url, base_url,
		          permissions, capability_profile, resource_selection, qualification_status,
		          last_qualified_at, last_error, created_by, updated_by, created_at, updated_at`,
		tenantID, id, status, truncate(lastError, 2000), string(capabilityProfile), actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInstallationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update external provider installation qualification: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) DeleteProviderInstallation(ctx context.Context, tenantID string, id uuid.UUID, actor string) error {
	err := secretlock.WithTx(ctx, r.secretPool, true, func(ctx context.Context, tx secretlock.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE external_provider_installations
			   SET status = 'deleted',
			       deleted_at = NOW(),
			       updated_by = $3,
			       updated_at = NOW()
			 WHERE tenant_id = $1
			   AND id = $2
			   AND deleted_at IS NULL`, tenantID, id, actor)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInstallationNotFound
		}
		_, err = tx.Exec(ctx, `
			UPDATE external_provider_installation_resources
			   SET status = 'removed',
			       selected = FALSE,
			       deleted_at = NOW(),
			       updated_at = NOW()
			 WHERE tenant_id = $1
			   AND installation_id = $2
			   AND deleted_at IS NULL`, tenantID, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete external provider installation: %w", err)
	}
	return nil
}

func (r *Repo) ListProviderInstallationResources(ctx context.Context, tenantID string, installationID uuid.UUID) ([]ProviderInstallationResource, error) {
	if _, err := r.GetProviderInstallation(ctx, tenantID, installationID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, installation_id, provider, resource_type, external_resource_id,
		       resource_key, display_name, html_url, selected, status, permissions,
		       last_seen_at, created_at, updated_at
		  FROM external_provider_installation_resources
		 WHERE tenant_id = $1
		   AND installation_id = $2
		   AND deleted_at IS NULL
		 ORDER BY selected DESC, resource_type ASC, resource_key ASC`, tenantID, installationID)
	if err != nil {
		return nil, fmt.Errorf("list external provider installation resources: %w", err)
	}
	defer rows.Close()
	var out []ProviderInstallationResource
	for rows.Next() {
		row, err := scanProviderInstallationResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) SelectProviderInstallationResources(
	ctx context.Context,
	tenantID string,
	installationID uuid.UUID,
	resourceIDs []uuid.UUID,
	actor string,
) ([]ProviderInstallationResource, error) {
	resourceIDs = normalizedUUIDArray(resourceIDs)
	var resources []ProviderInstallationResource
	err := secretlock.WithTx(ctx, r.secretPool, true, func(ctx context.Context, tx secretlock.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE external_provider_installations
			   SET resource_selection = CASE WHEN cardinality($3::uuid[]) = 0 THEN 'none' ELSE 'selected' END,
			       updated_by = $4,
			       updated_at = NOW()
			 WHERE tenant_id = $1
			   AND id = $2
			   AND deleted_at IS NULL`, tenantID, installationID, resourceIDs, actor)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInstallationNotFound
		}
		if err := updateResourceSelection(ctx, tx, tenantID, installationID, resourceIDs); err != nil {
			return err
		}
		var scanErr error
		resources, scanErr = listProviderInstallationResourcesTx(ctx, tx, tenantID, installationID)
		return scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("select external provider installation resources: %w", err)
	}
	return resources, nil
}

func upsertProviderInstallationResources(
	ctx context.Context,
	tx secretlock.Tx,
	rows []ProviderInstallationResource,
) ([]ProviderInstallationResource, error) {
	out := make([]ProviderInstallationResource, 0, len(rows))
	for _, in := range rows {
		row, err := scanProviderInstallationResource(tx.QueryRow(ctx, `
			INSERT INTO external_provider_installation_resources
			  (id, tenant_id, installation_id, provider, resource_type, external_resource_id,
			   resource_key, display_name, html_url, selected, status, permissions, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, COALESCE($13::timestamptz, NOW()))
			ON CONFLICT (tenant_id, installation_id, resource_type, resource_key)
			  WHERE deleted_at IS NULL
			DO UPDATE SET
			  external_resource_id = EXCLUDED.external_resource_id,
			  display_name = EXCLUDED.display_name,
			  html_url = EXCLUDED.html_url,
			  selected = EXCLUDED.selected,
			  status = EXCLUDED.status,
			  permissions = EXCLUDED.permissions,
			  last_seen_at = EXCLUDED.last_seen_at,
			  updated_at = NOW()
			RETURNING id, tenant_id, installation_id, provider, resource_type, external_resource_id,
			          resource_key, display_name, html_url, selected, status, permissions,
			          last_seen_at, created_at, updated_at`,
			in.ID, in.TenantID, in.InstallationID, in.Provider, in.ResourceType,
			in.ExternalResourceID, in.ResourceKey, in.DisplayName, in.HTMLURL,
			in.Selected, in.Status, string(in.Permissions), in.LastSeenAt))
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func updateResourceSelection(
	ctx context.Context,
	tx secretlock.Tx,
	tenantID string,
	installationID uuid.UUID,
	resourceIDs []uuid.UUID,
) error {
	distinctResourceIDs := normalizedUUIDArray(resourceIDs)
	if len(distinctResourceIDs) > 0 {
		var matched int
		if err := tx.QueryRow(ctx, `
			SELECT count(*)
			  FROM external_provider_installation_resources
			 WHERE tenant_id = $1
			   AND installation_id = $2
			   AND id = ANY($3::uuid[])
			   AND deleted_at IS NULL`, tenantID, installationID, distinctResourceIDs).Scan(&matched); err != nil {
			return err
		}
		if matched != len(distinctResourceIDs) {
			return ErrResourceNotFound
		}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE external_provider_installation_resources
		   SET selected = (id = ANY($3::uuid[])),
		       updated_at = NOW()
		 WHERE tenant_id = $1
		   AND installation_id = $2
		   AND deleted_at IS NULL`, tenantID, installationID, distinctResourceIDs)
	if err != nil {
		return err
	}
	if len(distinctResourceIDs) > 0 && tag.RowsAffected() == 0 {
		return ErrResourceNotFound
	}
	return nil
}

func normalizedUUIDArray(ids []uuid.UUID) []uuid.UUID {
	distinct := distinctUUIDs(ids)
	if distinct == nil {
		return []uuid.UUID{}
	}
	return distinct
}

func distinctUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) <= 1 {
		return ids
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func listProviderInstallationResourcesTx(
	ctx context.Context,
	tx secretlock.Tx,
	tenantID string,
	installationID uuid.UUID,
) ([]ProviderInstallationResource, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, installation_id, provider, resource_type, external_resource_id,
		       resource_key, display_name, html_url, selected, status, permissions,
		       last_seen_at, created_at, updated_at
		  FROM external_provider_installation_resources
		 WHERE tenant_id = $1
		   AND installation_id = $2
		   AND deleted_at IS NULL
		 ORDER BY selected DESC, resource_type ASC, resource_key ASC`, tenantID, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderInstallationResource
	for rows.Next() {
		row, err := scanProviderInstallationResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func scanProviderInstallation(row scanner) (ProviderInstallation, error) {
	var installation ProviderInstallation
	err := row.Scan(&installation.ID, &installation.TenantID, &installation.Provider,
		&installation.DisplayName, &installation.InstallationKind, &installation.Status,
		&installation.ExternalInstallationID, &installation.AccountLogin,
		&installation.AccountID, &installation.AccountURL, &installation.BaseURL,
		&installation.Permissions, &installation.CapabilityProfile,
		&installation.ResourceSelection, &installation.QualificationStatus,
		&installation.LastQualifiedAt, &installation.LastError, &installation.CreatedBy,
		&installation.UpdatedBy, &installation.CreatedAt, &installation.UpdatedAt)
	return installation, err
}

func scanProviderInstallationResource(row scanner) (ProviderInstallationResource, error) {
	var resource ProviderInstallationResource
	err := row.Scan(&resource.ID, &resource.TenantID, &resource.InstallationID,
		&resource.Provider, &resource.ResourceType, &resource.ExternalResourceID,
		&resource.ResourceKey, &resource.DisplayName, &resource.HTMLURL,
		&resource.Selected, &resource.Status, &resource.Permissions,
		&resource.LastSeenAt, &resource.CreatedAt, &resource.UpdatedAt)
	return resource, err
}
