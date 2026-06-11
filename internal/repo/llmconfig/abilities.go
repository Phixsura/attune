// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

var ErrAbilityNotFound = errors.New("llmconfig: ability not found")

func (r *Repo) ListAbilities(ctx context.Context, channelID uuid.UUID) ([]Ability, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, channel_id, logical_model, provider_model, enabled,
		       priority, weight, created_at, updated_at
		  FROM llm_channel_abilities
		 WHERE channel_id = $1
		 ORDER BY logical_model ASC`, channelID)
	if err != nil {
		return nil, fmt.Errorf("list llm abilities: %w", err)
	}
	defer rows.Close()
	var out []Ability
	for rows.Next() {
		row, err := scanAbility(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertAbility(ctx context.Context, a Ability) (*Ability, error) {
	row, err := scanAbility(r.pool.QueryRow(ctx, `
		INSERT INTO llm_channel_abilities
		 (channel_id, logical_model, provider_model, enabled, priority, weight)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (channel_id, logical_model) DO UPDATE SET
		 provider_model = EXCLUDED.provider_model,
		 enabled = EXCLUDED.enabled,
		 priority = EXCLUDED.priority,
		 weight = EXCLUDED.weight,
		 updated_at = NOW()
		RETURNING id, channel_id, logical_model, provider_model, enabled,
		          priority, weight, created_at, updated_at`,
		a.ChannelID, a.LogicalModel, a.ProviderModel, a.Enabled, a.Priority, a.Weight,
	))
	if err != nil {
		if pgxutil.IsForeignKeyViolation(err) {
			return nil, ErrChannelNotFound
		}
		return nil, fmt.Errorf("upsert llm ability: %w", err)
	}
	return ptrext.Of(row), nil
}

func (r *Repo) DeleteAbility(ctx context.Context, channelID uuid.UUID, logicalModel string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM llm_channel_abilities
		 WHERE channel_id = $1 AND logical_model = $2`,
		channelID, logicalModel,
	)
	if err != nil {
		return fmt.Errorf("delete llm ability: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAbilityNotFound
	}
	return nil
}

func (r *Repo) HasEligibleAbility(ctx context.Context, logicalModel string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM llm_channel_abilities a
			  JOIN llm_channels c ON c.id = a.channel_id
			 WHERE a.logical_model = $1
			   AND a.enabled = TRUE
			   AND c.status = 'enabled'
		)`,
		logicalModel,
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check llm ability eligibility: %w", err)
	}
	return ok, nil
}

type abilityScanner interface {
	Scan(dest ...any) error
}

func scanAbility(row abilityScanner) (Ability, error) {
	var a Ability
	err := row.Scan(
		&a.ID, &a.ChannelID, &a.LogicalModel, &a.ProviderModel, &a.Enabled,
		&a.Priority, &a.Weight, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ability{}, ErrAbilityNotFound
	}
	return a, err
}
