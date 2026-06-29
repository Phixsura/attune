//go:build integration

package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/auditevidence"
	"github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/repo/idempotency"
	"github.com/Phixsura/attune/internal/repo/llmconfig"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// isolationCase defines one cross-tenant access attempt. Exec tries an
// operation that should be blocked by tenant scoping. It returns nil when
// isolation holds (the cross-tenant access was correctly denied) and a
// descriptive error when a breach is detected.
type isolationCase struct {
	Domain    string
	Operation string
	Exec      func(ctx context.Context, f *Fixture) error
}

func TestRepoIsolationContract(t *testing.T) {
	f := NewFixture(t)

	var cases []isolationCase
	cases = append(cases, coreDomainCases()...)
	cases = append(cases, expandedDomainCases()...)

	for _, tc := range cases {
		t.Run(tc.Domain+"/"+tc.Operation, func(t *testing.T) {
			err := tc.Exec(f.Ctx, f)
			if err != nil {
				t.Errorf("ISOLATION BREACH: domain=%s op=%s: %v", tc.Domain, tc.Operation, err)
			}
		})
	}
}

// coreDomainCases returns isolation cases for the original 8 domains:
// feedback, tags, workflow, outbox, audit_log, api_keys, notify_targets, gdpr.
func coreDomainCases() []isolationCase {
	return []isolationCase{
		// ── feedback ───────────────────────────────────────────────
		{
			Domain:    "feedback",
			Operation: "SampleEnrichedByTenant_list_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				rows, err := f.Feedback.SampleEnrichedByTenant(ctx, f.TenantA.TenantID, time.Time{}, 100)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, r := range rows {
					if r.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A sample returned row %d belonging to tenant %s", r.ID, r.TenantID)
					}
				}
				return nil
			},
		},

		// ── tags ───────────────────────────────────────────────────
		{
			Domain:    "tags",
			Operation: "GetByID_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Tags.GetByID(ctx, f.TenantA.TenantID, f.TenantB.TagID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's tag %s", f.TenantB.TagID)
				}
				if !errors.Is(err, feedbacktag.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "tags",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				tags, err := f.Tags.List(ctx, f.TenantA.TenantID, true)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, tag := range tags {
					if tag.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned tag %s belonging to tenant %s", tag.ID, tag.TenantID)
					}
				}
				return nil
			},
		},

		// ── workflow ──────────────────────────────────────────────
		{
			Domain:    "workflow",
			Operation: "GetByTenantAndID_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Workflow.GetByTenantAndID(ctx, f.TenantA.TenantID, f.TenantB.WorkflowID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's workflow state %s", f.TenantB.WorkflowID)
				}
				if !errors.Is(err, workflowstate.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "workflow",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				states, err := f.Workflow.List(ctx, f.TenantA.TenantID, true)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, s := range states {
					if s.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned workflow state %s belonging to tenant %s", s.ID, s.TenantID)
					}
				}
				return nil
			},
		},

		// ── outbox ────────────────────────────────────────────────
		{
			Domain:    "outbox",
			Operation: "GetByID_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Outbox.GetByID(ctx, f.TenantA.TenantID, f.TenantB.OutboxID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's outbox row %d", f.TenantB.OutboxID)
				}
				if !errors.Is(err, outbox.ErrOutboxNotFound) {
					return fmt.Errorf("expected ErrOutboxNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "outbox",
			Operation: "ListByStatus_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				rows, err := f.Outbox.ListByStatus(ctx, f.TenantA.TenantID,
					[]string{outbox.OutboxStatusPending, outbox.OutboxStatusFailed, outbox.OutboxStatusDead, outbox.OutboxStatusDelivered},
					200, 0)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, r := range rows {
					if r.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned outbox row %d belonging to tenant %s", r.ID, r.TenantID)
					}
				}
				return nil
			},
		},

		// ── audit_log ─────────────────────────────────────────────
		{
			Domain:    "audit_log",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				result, err := f.AuditLog.List(ctx, auditlog.ListFilter{
					TenantID: f.TenantA.TenantID,
					Limit:    100,
				})
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, entry := range result.Items {
					if entry.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned audit entry %d belonging to tenant %s", entry.ID, entry.TenantID)
					}
				}
				return nil
			},
		},

		// ── api_keys ──────────────────────────────────────────────
		{
			Domain:    "api_keys",
			Operation: "GetByID_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.APIKeys.GetByID(ctx, f.TenantA.TenantID, f.TenantB.APIKeyID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's API key %s", f.TenantB.APIKeyID)
				}
				if !errors.Is(err, apikey.ErrAPIKeyNotFound) {
					return fmt.Errorf("expected ErrAPIKeyNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "api_keys",
			Operation: "ListByTenant_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				keys, err := f.APIKeys.ListByTenant(ctx, f.TenantA.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, k := range keys {
					if k.ID == f.TenantB.APIKeyID {
						return fmt.Errorf("tenant A list returned tenant B's API key %s", k.ID)
					}
				}
				return nil
			},
		},
		{
			Domain:    "api_keys",
			Operation: "Revoke_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				err := f.APIKeys.Revoke(ctx, f.TenantA.TenantID, f.TenantB.APIKeyID)
				if err == nil {
					return fmt.Errorf("tenant A revoked tenant B's API key %s", f.TenantB.APIKeyID)
				}
				if !errors.Is(err, apikey.ErrAPIKeyNotFound) {
					return fmt.Errorf("expected ErrAPIKeyNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── notify_targets ────────────────────────────────────────
		{
			Domain:    "notify_targets",
			Operation: "GetByID_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.NotifyTargets.GetByID(ctx, f.TenantA.TenantID, f.TenantB.NotifyID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's notify target %s", f.TenantB.NotifyID)
				}
				if !errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
					return fmt.Errorf("expected ErrNotifyTargetNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "notify_targets",
			Operation: "Delete_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				err := f.NotifyTargets.Delete(ctx, f.TenantA.TenantID, f.TenantB.NotifyID)
				if err == nil {
					return fmt.Errorf("tenant A deleted tenant B's notify target %s", f.TenantB.NotifyID)
				}
				if !errors.Is(err, notifytarget.ErrNotifyTargetNotFound) {
					return fmt.Errorf("expected ErrNotifyTargetNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── gdpr ──────────────────────────────────────────────────
		{
			Domain:    "gdpr",
			Operation: "Export_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.GDPR.Export(ctx, f.TenantA.TenantID, "nonexistent-subject-in-A")
				if err == nil {
					return fmt.Errorf("tenant A exported data for a subject that should not exist in A")
				}
				if !errors.Is(err, gdpr.ErrSubjectNotFound) {
					return fmt.Errorf("expected ErrSubjectNotFound, got: %w", err)
				}
				return nil
			},
		},
	}
}

// expandedDomainCases returns isolation cases for the 10 expanded domains:
// mcp_clients, feedback_job, audit_evidence, digest_subscription,
// system_settings, tenant_members, llm_config, idempotency,
// feedback_audit, embedding_tasks.
func expandedDomainCases() []isolationCase {
	return []isolationCase{
		// ── mcp_clients ──────────────────────────────────────────
		{
			Domain:    "mcp_clients",
			Operation: "ListByTenant_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				clients, err := f.MCPClients.ListByTenant(ctx, f.TenantA.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, c := range clients {
					if c.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned MCP client %s belonging to tenant %s", c.ID, c.TenantID)
					}
				}
				return nil
			},
		},

		// ── feedback_job ─────────────────────────────────────────
		{
			Domain:    "feedback_job",
			Operation: "Get_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.FeedbackJobs.Get(ctx, f.TenantA.TenantID, f.TenantB.FeedbackJobID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's feedback job %s", f.TenantB.FeedbackJobID)
				}
				if !errors.Is(err, feedbackjob.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},
		{
			Domain:    "feedback_job",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				jobs, _, err := f.FeedbackJobs.List(ctx, f.TenantA.TenantID, nil, 100, "")
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, j := range jobs {
					if j.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned feedback job %s belonging to tenant %s", j.ID, j.TenantID)
					}
				}
				return nil
			},
		},

		// ── audit_evidence ───────────────────────────────────────
		{
			Domain:    "audit_evidence",
			Operation: "GetJob_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.AuditEvidence.GetJob(ctx, f.TenantA.TenantID, f.TenantB.AuditEvidenceID)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's audit evidence job %s", f.TenantB.AuditEvidenceID)
				}
				if !errors.Is(err, auditevidence.ErrJobNotFound) {
					return fmt.Errorf("expected ErrJobNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── digest_subscription ──────────────────────────────────
		{
			Domain:    "digest_subscription",
			Operation: "GetByTenant_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				sub, err := f.DigestSubs.GetByTenant(ctx, f.TenantA.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				if sub.TenantID != f.TenantA.TenantID {
					return fmt.Errorf("tenant A GetByTenant returned subscription %s belonging to tenant %s", sub.ID, sub.TenantID)
				}
				return nil
			},
		},

		// ── system_settings ──────────────────────────────────────
		{
			Domain:    "system_settings",
			Operation: "Get_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				// Tenant A reads a key that only tenant B would have.
				_, err := f.SystemSettings.Get(ctx, f.TenantA.TenantID, "iso-key-only-in-B")
				if err == nil {
					return fmt.Errorf("tenant A retrieved a setting that should not exist for A")
				}
				if !errors.Is(err, systemsettings.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── tenant_members ───────────────────────────────────────
		{
			Domain:    "tenant_members",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				members, err := f.TenantMembers.List(ctx, f.TenantA.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, m := range members {
					if m.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned member %s belonging to tenant %s", m.ID, m.TenantID)
					}
				}
				return nil
			},
		},
		{
			Domain:    "tenant_members",
			Operation: "GetByUser_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.TenantMembers.GetByUser(ctx, f.TenantA.TenantID, "admin", fmt.Sprintf("iso-member-%s", f.TenantB.Slug))
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's member via GetByUser")
				}
				if !errors.Is(err, tenantmember.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── llm_config ───────────────────────────────────────────
		{
			Domain:    "llm_config",
			Operation: "DeleteRoute_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				err := f.LLMConfig.DeleteRoute(ctx, f.TenantA.TenantID, "iso-test-only-B")
				if err == nil {
					return fmt.Errorf("tenant A deleted a route with purpose that should not exist for A")
				}
				if !errors.Is(err, llmconfig.ErrRouteNotFound) {
					return fmt.Errorf("expected ErrRouteNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── idempotency ──────────────────────────────────────────
		{
			Domain:    "idempotency",
			Operation: "Get_cross_tenant",
			Exec: func(ctx context.Context, f *Fixture) error {
				_, err := f.Idempotency.Get(ctx, f.TenantA.TenantID, f.TenantB.IdempotencyKey)
				if err == nil {
					return fmt.Errorf("tenant A retrieved tenant B's idempotency key %s", f.TenantB.IdempotencyKey)
				}
				if !errors.Is(err, idempotency.ErrNotFound) {
					return fmt.Errorf("expected ErrNotFound, got: %w", err)
				}
				return nil
			},
		},

		// ── feedback_audit ───────────────────────────────────────
		{
			Domain:    "feedback_audit",
			Operation: "List_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				entries, _, err := f.FeedbackAudit.List(ctx, f.TenantA.TenantID, f.TenantA.FeedbackID, "", 100)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				for _, e := range entries {
					if e.TenantID != f.TenantA.TenantID {
						return fmt.Errorf("tenant A list returned feedback audit entry %d belonging to tenant %s", e.ID, e.TenantID)
					}
				}
				// Verify tenant A cannot read tenant B's feedback audit trail.
				crossEntries, _, err := f.FeedbackAudit.List(ctx, f.TenantA.TenantID, f.TenantB.FeedbackID, "", 100)
				if err != nil {
					return fmt.Errorf("unexpected error on cross-tenant list: %w", err)
				}
				if len(crossEntries) > 0 {
					return fmt.Errorf("tenant A retrieved %d audit entries for tenant B's feedback ID %d", len(crossEntries), f.TenantB.FeedbackID)
				}
				return nil
			},
		},

		// ── embedding_tasks ──────────────────────────────────────
		{
			Domain:    "embedding_tasks",
			Operation: "QueueDepth_isolation",
			Exec: func(ctx context.Context, f *Fixture) error {
				depthA, err := f.EmbeddingTasks.QueueDepth(ctx, f.TenantA.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				depthB, err := f.EmbeddingTasks.QueueDepth(ctx, f.TenantB.TenantID)
				if err != nil {
					return fmt.Errorf("unexpected error: %w", err)
				}
				if depthA != 1 {
					return fmt.Errorf("tenant A queue depth = %d, want 1", depthA)
				}
				if depthB != 1 {
					return fmt.Errorf("tenant B queue depth = %d, want 1", depthB)
				}
				return nil
			},
		},
	}
}
