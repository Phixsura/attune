//go:build integration

package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/auditevidence"
	"github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/digestsubscription"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	"github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/repo/guardpolicy"
	"github.com/Phixsura/attune/internal/repo/idempotency"
	"github.com/Phixsura/attune/internal/repo/llmconfig"
	"github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	"github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/replydraft"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

// TenantData holds identifiers for every seeded row belonging to one tenant.
type TenantData struct {
	TenantID   string
	Slug       string
	FeedbackID int64
	TagID      uuid.UUID
	WorkflowID string
	OutboxID   int64
	APIKeyID   uuid.UUID
	APIKeyHash []byte
	NotifyID   uuid.UUID

	MCPClientID     uuid.UUID
	FeedbackJobID   string
	AuditEvidenceID string
	DigestSubID     uuid.UUID
	LLMRouteID      uuid.UUID
	IdempotencyKey  string
	EmbeddingTaskID    int64
	MemberID           string
	TagName            string
	GDPRDeleteReqID    string
	GDPRExportJobID    string
	GuardPolicyID      string
	ReplyDraftFBID     int64
}

// Fixture is the shared test environment for tenant-isolation contracts.
// It seeds two fully-independent tenants across all data domains.
type Fixture struct {
	Pool    *pgxpool.Pool
	Ctx     context.Context
	TenantA TenantData
	TenantB TenantData

	// Repo instances for direct use in test assertions.
	Feedback      *feedback.FeedbackRepo
	Tags          *feedbacktag.Repo
	Workflow      *workflowstate.Repo
	Outbox        *outbox.OutboxRepo
	AuditLog      *auditlog.Repo
	APIKeys       *apikey.APIKeyRepo
	NotifyTargets *notifytarget.NotifyTargetRepo
	GDPR          *gdpr.Repo

	TagAssign      *feedbacktagassignment.Repo
	MCPClients     *mcp.ClientsRepo
	FeedbackJobs   *feedbackjob.Repo
	AuditEvidence  *auditevidence.Repo
	DigestSubs     *digestsubscription.Repo
	SystemSettings *systemsettings.Repo
	TenantMembers  *tenantmember.Repo
	LLMConfig      *llmconfig.Repo
	Idempotency    *idempotency.Repo
	FeedbackAudit  *feedbackaudit.Repo
	EmbeddingTasks *embedding.TaskRepo
	GuardPolicy    *guardpolicy.Repo
	ReplyDrafts    *replydraft.DraftTaskRepo
}

// NewFixture creates two tenants and seeds data for each across feedback,
// tags, workflow states, outbox, API keys, notify targets, and audit log.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()

	pool := testdb.NewPool(t)
	ctx := context.Background()

	f := &Fixture{
		Pool:          pool,
		Ctx:           ctx,
		Feedback:      feedback.NewFeedback(pool),
		Tags:          feedbacktag.New(pool),
		Workflow:      workflowstate.New(pool),
		Outbox:        outbox.NewOutbox(pool),
		AuditLog:      auditlog.New(pool),
		APIKeys:       apikey.NewAPIKey(pool),
		NotifyTargets: notifytarget.NewNotifyTarget(pool),
		GDPR:          gdpr.New(pool),

		TagAssign:      feedbacktagassignment.New(pool),
		MCPClients:     mcp.NewClients(pool),
		FeedbackJobs:   feedbackjob.New(pool),
		AuditEvidence:  auditevidence.New(pool),
		DigestSubs:     digestsubscription.New(pool),
		SystemSettings: systemsettings.NewRepo(pool),
		TenantMembers:  tenantmember.NewRepo(pool),
		LLMConfig:      llmconfig.New(pool),
		Idempotency:    idempotency.New(pool),
		FeedbackAudit:  feedbackaudit.New(pool),
		EmbeddingTasks: embedding.NewTaskRepo(pool),
		GuardPolicy:    guardpolicy.New(pool),
		ReplyDrafts:    replydraft.NewDraftTaskRepo(pool),
	}

	f.TenantA = seedTenant(t, f, "iso-alpha", "Isolation Alpha")
	f.TenantB = seedTenant(t, f, "iso-bravo", "Isolation Bravo")

	return f
}

// seedTenant creates a tenant and populates every data domain with one row.
func seedTenant(t *testing.T, f *Fixture, slug, name string) TenantData {
	t.Helper()

	ctx := f.Ctx
	pool := f.Pool

	tid, err := tenant.NewTenant(pool).Create(ctx, slug, name)
	if err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}

	td := TenantData{
		TenantID: tid,
		Slug:     slug,
	}

	// --- feedback ---
	td.FeedbackID = seedFeedback(t, ctx, pool, tid)

	// --- tag ---
	td.TagName = fmt.Sprintf("iso-tag-%s", slug)
	td.TagID = seedTag(t, ctx, f.Tags, tid, td.TagName)

	// --- workflow state ---
	td.WorkflowID = seedWorkflowState(t, ctx, f.Workflow, tid)

	// --- outbox (raw SQL — Insert requires a pgx.Tx and an existing feedback row) ---
	td.OutboxID = seedOutbox(t, ctx, pool, tid, td.FeedbackID)

	// --- API key ---
	td.APIKeyHash, td.APIKeyID = seedAPIKey(t, ctx, f.APIKeys, tid, slug)

	// --- notify target ---
	td.NotifyID = seedNotifyTarget(t, ctx, f.NotifyTargets, tid, slug)

	// --- audit log ---
	seedAuditLog(t, ctx, f.AuditLog, tid)

	// --- MCP client ---
	td.MCPClientID = seedMCPClient(t, ctx, f.MCPClients, tid, slug)

	// --- feedback job ---
	td.FeedbackJobID = seedFeedbackJob(t, ctx, f.FeedbackJobs, tid)

	// --- audit evidence ---
	td.AuditEvidenceID = seedAuditEvidence(t, ctx, f.AuditEvidence, tid)

	// --- digest subscription ---
	td.DigestSubID = seedDigestSubscription(t, ctx, f.DigestSubs, tid)

	// --- system setting ---
	seedSystemSetting(t, ctx, f.SystemSettings, tid, slug)

	// --- tenant member ---
	td.MemberID = seedTenantMember(t, ctx, f.TenantMembers, tid, slug)

	// --- LLM route ---
	td.LLMRouteID = seedLLMRoute(t, ctx, f.LLMConfig, tid)

	// --- idempotency key ---
	td.IdempotencyKey = seedIdempotencyKey(t, ctx, f.Idempotency, tid, slug)

	// --- feedback audit (needs FeedbackID) ---
	seedFeedbackAudit(t, ctx, f.FeedbackAudit, tid, td.FeedbackID)

	// --- embedding task (needs FeedbackID) ---
	td.EmbeddingTaskID = seedEmbeddingTask(t, ctx, f.EmbeddingTasks, td.FeedbackID, tid)

	// --- tag assignment (needs FeedbackID + TagID) ---
	seedTagAssignment(t, ctx, f.TagAssign, tid, td.FeedbackID, td.TagID)

	// --- GDPR delete request + export job ---
	td.GDPRDeleteReqID, td.GDPRExportJobID = seedGDPR(t, ctx, f.GDPR, tid, slug)

	// --- guard policy ---
	td.GuardPolicyID = seedGuardPolicy(t, ctx, f.GuardPolicy, tid, slug)

	// --- reply draft (needs a feedback row with enrichment_status='enriched') ---
	td.ReplyDraftFBID = seedReplyDraftFeedback(t, ctx, pool, tid)

	return td
}

func seedFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'iso-user', 'api', 'isolation seed')
		RETURNING id`, tenantID).Scan(&id) // ptrext:allow scan-out-param
	if err != nil {
		t.Fatalf("seed feedback for %s: %v", tenantID, err)
	}
	return id
}

func seedTag(t *testing.T, ctx context.Context, repo *feedbacktag.Repo, tenantID, tagName string) uuid.UUID {
	t.Helper()
	tag, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID:  tenantID,
		Name:      tagName,
		Color:     "#ff0000",
		CreatedBy: "iso-seed",
	})
	if err != nil {
		t.Fatalf("seed tag for %s: %v", tenantID, err)
	}
	return tag.ID
}

func seedWorkflowState(t *testing.T, ctx context.Context, repo *workflowstate.Repo, tenantID string) string {
	t.Helper()
	ws, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID:    tenantID,
		Name:        "iso-state",
		DisplayName: domain.I18nString{"default": "Isolation State"},
		Color:       "#00ff00",
		Category:    "open",
		Position:    0,
		IsDefault:   false,
	})
	if err != nil {
		t.Fatalf("seed workflow state for %s: %v", tenantID, err)
	}
	return ws.ID
}

func seedOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO notify_outbox
		 (feedback_id, tenant_id, destination_type, destination_target, audience,
		  payload, status, attempts, trace_id)
		VALUES ($1, $2, 'raw-webhook', 'https://iso.test/hook', 'pool',
		        '{}'::jsonb, 'pending', 0, 'iso-trace')
		RETURNING id`, feedbackID, tenantID).Scan(&id) // ptrext:allow scan-out-param
	if err != nil {
		t.Fatalf("seed outbox for %s: %v", tenantID, err)
	}
	return id
}

func seedAPIKey(t *testing.T, ctx context.Context, repo *apikey.APIKeyRepo, tenantID, slug string) ([]byte, uuid.UUID) {
	t.Helper()
	h := sha256.Sum256([]byte(fmt.Sprintf("iso-key-%s", slug)))
	hash := h[:]
	prefix := fmt.Sprintf("attn_%s", slug[:4])

	id, err := repo.Insert(ctx, tenantID, hash, prefix, "iso-key")
	if err != nil {
		t.Fatalf("seed API key for %s: %v", tenantID, err)
	}
	return hash, id
}

func seedNotifyTarget(t *testing.T, ctx context.Context, repo *notifytarget.NotifyTargetRepo, tenantID, slug string) uuid.UUID {
	t.Helper()
	id, _, err := repo.Insert(ctx, notifytarget.NotifyTarget{
		TenantID:        tenantID,
		DestinationType: notifytarget.DestRawWebhook,
		Audience:        notifytarget.AudiencePool,
		URL:             fmt.Sprintf("https://%s.test/notify", slug),
		Secret:          "iso-secret-at-least-16ch",
		TimeoutSeconds:  5,
	})
	if err != nil {
		t.Fatalf("seed notify target for %s: %v", tenantID, err)
	}
	return id
}

func seedAuditLog(t *testing.T, ctx context.Context, repo *auditlog.Repo, tenantID string) {
	t.Helper()
	err := repo.Insert(ctx, auditlog.Entry{
		TenantID:   tenantID,
		ActorType:  "admin",
		ActorID:    "iso-actor",
		Action:     "tag.create",
		TargetType: "tag",
		TargetID:   "iso-target",
		Summary:    "isolation seed audit entry",
	})
	if err != nil {
		t.Fatalf("seed audit log for %s: %v", tenantID, err)
	}
}

func seedMCPClient(t *testing.T, ctx context.Context, repo *mcp.ClientsRepo, tenantID, slug string) uuid.UUID {
	t.Helper()
	client, err := repo.Create(ctx, mcp.CreateClientParams{
		TenantID:     tenantID,
		Name:         fmt.Sprintf("iso-client-%s", slug),
		RedirectURIs: []string{fmt.Sprintf("https://%s.test/callback", slug)},
		Scopes:       []string{"tool:read"},
		CreatedBy:    "iso-seed",
	})
	if err != nil {
		t.Fatalf("seed MCP client for %s: %v", tenantID, err)
	}
	return client.ID
}

func seedFeedbackJob(t *testing.T, ctx context.Context, repo *feedbackjob.Repo, tenantID string) string {
	t.Helper()
	job, err := repo.Create(ctx, tenantID, "iso-seed", []byte(`{"type":"test"}`), 1)
	if err != nil {
		t.Fatalf("seed feedback job for %s: %v", tenantID, err)
	}
	return job.ID
}

func seedAuditEvidence(t *testing.T, ctx context.Context, repo *auditevidence.Repo, tenantID string) string {
	t.Helper()
	job, err := repo.CreateJob(ctx, tenantID, "admin", "iso-seed", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("seed audit evidence for %s: %v", tenantID, err)
	}
	return job.ID
}

func seedDigestSubscription(t *testing.T, ctx context.Context, repo *digestsubscription.Repo, tenantID string) uuid.UUID {
	t.Helper()
	sub, err := repo.Upsert(ctx, digestsubscription.Subscription{
		TenantID:       tenantID,
		Enabled:        true,
		Frequency:      "daily",
		SendHour:       9,
		LLMMinFeedback: 6,
	})
	if err != nil {
		t.Fatalf("seed digest subscription for %s: %v", tenantID, err)
	}
	return sub.ID
}

func seedSystemSetting(t *testing.T, ctx context.Context, repo *systemsettings.Repo, tenantID, slug string) {
	t.Helper()
	err := repo.Set(ctx, tenantID, "iso-test-key", "iso-test-value", "iso-seed")
	if err != nil {
		t.Fatalf("seed system setting for %s: %v", tenantID, err)
	}
	uniqueKey := fmt.Sprintf("iso-unique-%s", slug)
	err = repo.Set(ctx, tenantID, uniqueKey, "iso-unique-value", "iso-seed")
	if err != nil {
		t.Fatalf("seed unique system setting for %s: %v", tenantID, err)
	}
}

func seedTenantMember(t *testing.T, ctx context.Context, repo *tenantmember.Repo, tenantID, slug string) string {
	t.Helper()
	m, err := repo.Create(ctx, tenantmember.Member{
		TenantID:   tenantID,
		MemberType: "admin",
		UserID:     fmt.Sprintf("iso-member-%s", slug),
		Role:       domain.RoleAdmin,
		RoleSource: "manual",
	})
	if err != nil {
		t.Fatalf("seed tenant member for %s: %v", tenantID, err)
	}
	return m.ID
}

func seedLLMRoute(t *testing.T, ctx context.Context, repo *llmconfig.Repo, tenantID string) uuid.UUID {
	t.Helper()
	route, err := repo.UpsertRoute(ctx, llmconfig.Route{
		TenantID:     tenantID,
		Purpose:      "iso-test",
		LogicalModel: "gpt-4o-mini",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("seed LLM route for %s: %v", tenantID, err)
	}
	return route.ID
}

func seedIdempotencyKey(t *testing.T, ctx context.Context, repo *idempotency.Repo, tenantID, slug string) string {
	t.Helper()
	key := fmt.Sprintf("iso-idem-%s", slug)
	hash := sha256.Sum256([]byte(key))
	_, _, err := repo.Acquire(ctx, tenantID, key, hash[:], 24*time.Hour)
	if err != nil {
		t.Fatalf("seed idempotency key for %s: %v", tenantID, err)
	}
	return key
}

func seedFeedbackAudit(t *testing.T, ctx context.Context, repo *feedbackaudit.Repo, tenantID string, feedbackID int64) {
	t.Helper()
	err := repo.Write(ctx, feedbackaudit.Entry{
		TenantID:   tenantID,
		FeedbackID: feedbackID,
		EntityType: "feedback",
		FieldName:  "status",
		OldValue:   ptrext.Of("new"),
		NewValue:   ptrext.Of("open"),
		ChangedBy:  "iso-seed",
	})
	if err != nil {
		t.Fatalf("seed feedback audit for %s: %v", tenantID, err)
	}
}

func seedTagAssignment(t *testing.T, ctx context.Context, repo *feedbacktagassignment.Repo, tenantID string, feedbackID int64, tagID uuid.UUID) {
	t.Helper()
	_, err := repo.Add(ctx, tenantID, feedbackID, tagID, "iso-seed")
	if err != nil {
		t.Fatalf("seed tag assignment for %s: %v", tenantID, err)
	}
}

func seedEmbeddingTask(t *testing.T, ctx context.Context, repo *embedding.TaskRepo, feedbackID int64, tenantID string) int64 {
	t.Helper()
	id, err := repo.CreateTask(ctx, feedbackID, tenantID)
	if err != nil {
		t.Fatalf("seed embedding task for %s: %v", tenantID, err)
	}
	return id
}

func seedGDPR(t *testing.T, ctx context.Context, repo *gdpr.Repo, tenantID, _ string) (string, string) {
	t.Helper()
	subjectKey := "iso-user"
	subjectHash := fmt.Sprintf("%x", sha256.Sum256([]byte(subjectKey)))

	delResult, err := repo.CreateDeleteRequest(ctx, tenantID, subjectKey, subjectHash, "admin", "iso-seed", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("seed GDPR delete request for %s: %v", tenantID, err)
	}

	exportJob, err := repo.CreateExportJob(ctx, tenantID, subjectKey, subjectHash, "admin", "iso-seed")
	if err != nil {
		t.Fatalf("seed GDPR export job for %s: %v", tenantID, err)
	}
	return delResult.RequestID, exportJob.ID
}

func seedGuardPolicy(t *testing.T, ctx context.Context, repo *guardpolicy.Repo, tenantID, slug string) string {
	t.Helper()
	p, err := repo.CreateTenantPolicy(ctx, tenantID, "iso-seed", llmguard.Policy{
		Name:     fmt.Sprintf("iso-policy-%s", slug),
		Kind:     llmguard.KindDefault,
		Enabled:  true,
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("seed guard policy for %s: %v", tenantID, err)
	}
	return p.ID
}

func seedReplyDraftFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content, enrichment_status)
		VALUES ($1, 'iso-user', 'api', 'isolation seed for reply draft', 'enriched')
		RETURNING id`, tenantID).Scan(&id) // ptrext:allow scan-out-param
	if err != nil {
		t.Fatalf("seed reply draft feedback for %s: %v", tenantID, err)
	}
	return id
}
