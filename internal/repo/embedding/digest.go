// SPDX-License-Identifier: Apache-2.0

package embedding

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DigestCluster is one cluster's window roll-up for the daily digest (#27):
// the cluster id, its in-window member count, and its cached label.
type DigestCluster struct {
	ClusterID uuid.UUID
	Count     int
	Label     string
}

// DigestExample is a representative feedback row for a digest theme.
type DigestExample struct {
	ID    int64
	Title string
}

// TopClustersInWindow returns the top `limit` clusters by in-window member count
// for a tenant, over [from, to) on created_at, restricted to enriched rows. The
// count and label are SQL-derived — the LLM never produces them. Unclustered
// (cluster_id NULL) rows are excluded here and surfaced separately in the digest
// totals.
func (r *TaskRepo) TopClustersInWindow(
	ctx context.Context, tenantID string, from, to time.Time, limit int,
) ([]DigestCluster, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cluster_id, COUNT(*) AS count, COALESCE(MAX(cluster_label), '')
		FROM user_feedback
		WHERE tenant_id = $1
		  AND cluster_id IS NOT NULL
		  AND enrichment_status = 'done'
		  AND created_at >= $2 AND created_at < $3
		GROUP BY cluster_id
		ORDER BY COUNT(*) DESC, cluster_id DESC
		LIMIT $4`, tenantID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("top clusters in window: %w", err)
	}
	defer rows.Close()

	var out []DigestCluster
	for rows.Next() {
		var c DigestCluster
		if err := rows.Scan(&c.ClusterID, &c.Count, &c.Label); err != nil {
			return nil, fmt.Errorf("scan digest cluster: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ClusterExamplesInWindow returns up to `limit` representative feedback rows
// (most recent first) for one cluster within [from, to), for the digest's
// "top-N example IDs". IDs are real feedback ids — never LLM-fabricated.
func (r *TaskRepo) ClusterExamplesInWindow(
	ctx context.Context, tenantID string, clusterID uuid.UUID, from, to time.Time, limit int,
) ([]DigestExample, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, COALESCE(NULLIF(enriched_title, ''), LEFT(content, 100))
		FROM user_feedback
		WHERE tenant_id = $1
		  AND cluster_id = $2
		  AND enrichment_status = 'done'
		  AND created_at >= $3 AND created_at < $4
		ORDER BY created_at DESC
		LIMIT $5`, tenantID, clusterID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("cluster examples in window: %w", err)
	}
	defer rows.Close()

	var out []DigestExample
	for rows.Next() {
		var e DigestExample
		if err := rows.Scan(&e.ID, &e.Title); err != nil {
			return nil, fmt.Errorf("scan digest example: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
