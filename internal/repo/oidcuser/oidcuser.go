// SPDX-License-Identifier: Apache-2.0

// Package oidcuser provides persistence for OIDC-authenticated users.
package oidcuser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrNotFound is returned when no user matches the query.
var ErrNotFound = errors.New("oidc user not found")

// Repo provides OIDC user persistence operations.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a new OIDC user repository.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

// GetByExternalID finds a user by provider and external ID (sub claim).
func (r *Repo) GetByExternalID(ctx context.Context, provider, externalID string) (domain.OIDCUser, error) {
	const q = `
		SELECT id, provider, external_id, email, display_name, role, groups,
		       created_at, updated_at, last_login_at
		FROM oidc_users
		WHERE provider = $1 AND external_id = $2`

	var u domain.OIDCUser
	var groupsJSON []byte
	var displayName *string
	var lastLogin *time.Time

	err := r.pool.QueryRow(ctx, q, provider, externalID).Scan(
		&u.ID, &u.Provider, &u.ExternalID, &u.Email, &displayName,
		&u.Role, &groupsJSON, &u.CreatedAt, &u.UpdatedAt, &lastLogin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OIDCUser{}, ErrNotFound
	}
	if err != nil {
		return domain.OIDCUser{}, err
	}

	u.DisplayName = ptrext.Indirect(displayName)
	if groupsJSON != nil {
		if err := json.Unmarshal(groupsJSON, &u.Groups); err != nil {
			logext.Errorf(ctx, "[oidcuser.GetByExternalID] groups JSON unmarshal failed,id:%s,err:%s", u.ID, err.Error())
			return domain.OIDCUser{}, fmt.Errorf("groups JSON unmarshal: %w", err)
		}
	} else {
		u.Groups = []string{} // ensure non-nil
	}
	u.LastLoginAt = ptrext.Indirect(lastLogin)

	return u, nil
}

// GetByID finds a user by internal ID.
func (r *Repo) GetByID(ctx context.Context, id string) (domain.OIDCUser, error) {
	const q = `
		SELECT id, provider, external_id, email, display_name, role, groups,
		       created_at, updated_at, last_login_at
		FROM oidc_users
		WHERE id = $1`

	var u domain.OIDCUser
	var groupsJSON []byte
	var displayName *string
	var lastLogin *time.Time

	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Provider, &u.ExternalID, &u.Email, &displayName,
		&u.Role, &groupsJSON, &u.CreatedAt, &u.UpdatedAt, &lastLogin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OIDCUser{}, ErrNotFound
	}
	if err != nil {
		return domain.OIDCUser{}, err
	}

	u.DisplayName = ptrext.Indirect(displayName)
	if groupsJSON != nil {
		if err := json.Unmarshal(groupsJSON, &u.Groups); err != nil {
			logext.Errorf(ctx, "[oidcuser.GetByID] groups JSON unmarshal failed,id:%s,err:%s", u.ID, err.Error())
			return domain.OIDCUser{}, fmt.Errorf("groups JSON unmarshal: %w", err)
		}
	} else {
		u.Groups = []string{} // ensure non-nil
	}
	u.LastLoginAt = ptrext.Indirect(lastLogin)

	return u, nil
}

// Upsert creates or updates an OIDC user and returns the result with ID.
func (r *Repo) Upsert(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error) {
	if u.Groups == nil {
		u.Groups = []string{}
	}
	groupsJSON, err := json.Marshal(u.Groups)
	if err != nil {
		return domain.OIDCUser{}, fmt.Errorf("groups JSON marshal: %w", err)
	}

	const q = `
		INSERT INTO oidc_users (provider, external_id, email, display_name, role, groups, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (provider, external_id)
		DO UPDATE SET
		    email = EXCLUDED.email,
		    display_name = EXCLUDED.display_name,
		    role = EXCLUDED.role,
		    groups = EXCLUDED.groups,
		    updated_at = NOW(),
		    last_login_at = NOW()
		RETURNING id, created_at, updated_at, last_login_at`

	err = r.pool.QueryRow(
		ctx, q,
		u.Provider, u.ExternalID, u.Email, u.DisplayName, u.Role, groupsJSON,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return domain.OIDCUser{}, err
	}

	return u, nil
}
