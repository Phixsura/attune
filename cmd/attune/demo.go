package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	feedbackauditrepo "github.com/Phixsura/attune/internal/repo/feedbackaudit"
	tenantrepo "github.com/Phixsura/attune/internal/repo/tenant"
	workflowstaterepo "github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

const (
	defaultDemoTenantSlug = "attune-demo"
	defaultDemoTenantName = "Attune Demo"
	demoSeedActor         = "demo-seed"
)

type demoFeedbackSeed struct {
	Key        string
	Source     string
	UserID     string
	Content    string
	PageURL    string
	Title      string
	Attrs      map[string]any
	Confidence float64
	AgeHours   int
	Embedding  bool
}

type demoSeedResult struct {
	TenantID       string
	TenantSlug     string
	FeedbackRows   int
	SearchRuns     int
	QualityActions int
}

func runDemo(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: attune demo seed [--tenant <slug>] [--name <name>]")
	}
	switch args[0] {
	case "seed":
		return runDemoSeed(args[1:])
	default:
		return fmt.Errorf("unknown demo subcommand %q (have: seed)", args[0])
	}
}

func runDemoSeed(args []string) error {
	fs := flag.NewFlagSet("demo seed", flag.ContinueOnError)
	tenantSlug := fs.String("tenant", defaultDemoTenantSlug, "tenant slug to create or refresh")
	tenantName := fs.String("name", defaultDemoTenantName, "tenant display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(ptrext.Indirect(tenantSlug)) == "" {
		return fmt.Errorf("--tenant is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	result, err := seedDemoWorkspace(ctx, pool, strings.TrimSpace(ptrext.Indirect(tenantSlug)), ptrext.Indirect(tenantName))
	if err != nil {
		return err
	}
	fmt.Printf(`Demo workspace ready:

 tenant: %s
 tenant_id: %s
 feedback_rows: %d
 search_runs: %d
 quality_actions: %d

Open the Console and visit /control-tower after signing in.
`,
		result.TenantSlug, result.TenantID, result.FeedbackRows,
		result.SearchRuns, result.QualityActions)
	return nil
}

func seedDemoWorkspace(ctx context.Context, pool *pgxpool.Pool, slug, name string) (demoSeedResult, error) {
	tenantID, err := upsertDemoTenant(ctx, pool, slug, name)
	if err != nil {
		return demoSeedResult{}, err
	}
	if err := workflowsvc.NewService(
		workflowstaterepo.New(pool),
		feedbackauditrepo.New(pool),
		pool,
	).SeedDefaults(ctx, tenantID); err != nil {
		return demoSeedResult{}, fmt.Errorf("seed workflow defaults: %w", err)
	}
	feedbackRepo := feedbackrepo.NewFeedback(pool)
	if err := clearDemoTelemetry(ctx, pool, tenantID); err != nil {
		return demoSeedResult{}, err
	}
	ids, err := seedDemoFeedback(ctx, pool, tenantID, time.Now().UTC())
	if err != nil {
		return demoSeedResult{}, err
	}
	if err := seedDemoSearchTelemetry(ctx, feedbackRepo, tenantID, ids); err != nil {
		return demoSeedResult{}, err
	}
	if err := seedDemoQualityActions(ctx, feedbackRepo, tenantID); err != nil {
		return demoSeedResult{}, err
	}
	return demoSeedResult{
		TenantID:       tenantID,
		TenantSlug:     slug,
		FeedbackRows:   len(ids),
		SearchRuns:     12,
		QualityActions: 1,
	}, nil
}

func upsertDemoTenant(ctx context.Context, pool *pgxpool.Pool, slug, name string) (string, error) {
	tenants := tenantrepo.NewTenant(pool)
	id, err := tenants.ResolveSlug(ctx, slug)
	if err == nil {
		if _, err := pool.Exec(ctx, `UPDATE tenants SET name = $2, is_active = TRUE, updated_at = NOW() WHERE id = $1`, id, name); err != nil {
			return "", fmt.Errorf("refresh demo tenant: %w", err)
		}
		return id, nil
	}
	if !errors.Is(err, tenantrepo.ErrTenantNotFound) {
		return "", fmt.Errorf("resolve demo tenant: %w", err)
	}
	id, err = tenants.Create(ctx, slug, name)
	if err != nil {
		return "", fmt.Errorf("create demo tenant: %w", err)
	}
	return id, nil
}

func clearDemoTelemetry(ctx context.Context, pool *pgxpool.Pool, tenantID string) error {
	if _, err := pool.Exec(ctx, `
		DELETE FROM semantic_extraction_runs
		 WHERE tenant_id = $1
		   AND prompt_version = 'demo-seed.v1'`, tenantID); err != nil {
		return fmt.Errorf("clear demo semantic runs: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		DELETE FROM feedback_search_runs
		 WHERE tenant_id = $1
		   AND actor_user_id = $2`, tenantID, demoSeedActor); err != nil {
		return fmt.Errorf("clear demo search runs: %w", err)
	}
	return nil
}

func seedDemoFeedback(ctx context.Context, pool *pgxpool.Pool, tenantID string, now time.Time) ([]int64, error) {
	seeds := demoFeedbackSeeds()
	ids := make([]int64, 0, len(seeds))
	for i, seed := range seeds {
		id, err := upsertDemoFeedback(ctx, pool, tenantID, seed, i, now)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if err := insertDemoSemanticRun(ctx, pool, tenantID, id, seed, now); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func upsertDemoFeedback(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	seed demoFeedbackSeed,
	index int,
	now time.Time,
) (int64, error) {
	attrsJSON, err := json.Marshal(seed.Attrs)
	if err != nil {
		return 0, fmt.Errorf("marshal demo attrs: %w", err)
	}
	metaJSON, err := json.Marshal(map[string]any{
		"demo_seed": true,
		"account":   demoAccount(index),
		"plan":      demoPlan(index),
	})
	if err != nil {
		return 0, fmt.Errorf("marshal demo source meta: %w", err)
	}
	idemKey := "demo-seed-" + seed.Key
	hash := sha256.Sum256([]byte(tenantID + ":" + idemKey))
	embedding := ""
	if seed.Embedding {
		embedding = demoEmbedding(index)
	}
	createdAt := now.Add(-time.Duration(seed.AgeHours) * time.Hour)
	var id int64
	err = pool.QueryRow(ctx, `
		INSERT INTO user_feedback
		 (tenant_id, user_id, subject_key, subject_display, subject_hash, source,
		  source_meta, type, content, page_url, attachments, enrichment_status,
		  enriched_title, enriched_display_title, enriched_display_rationale,
		  enriched_attrs, is_urgent, classification_confidence, language,
		  enriched_at, created_at, updated_at, idempotency_key, idempotency_hash,
		  embedding, embedding_model, embedding_dims, embedded_at, workflow_state_id)
		VALUES
		 ($1, $2, $2, $2, '', $3, $4::jsonb, 'other', $5, $6, '[]'::jsonb, 'done',
		  $7, $7, 'Demo classification produced by attune demo seed.',
		  $8::jsonb, $9, $10, 'en', $11, $12, $11, $13, $14,
		  NULLIF($15, '')::vector,
		  CASE WHEN $15 = '' THEN '' ELSE 'text-embedding-3-small' END,
		  CASE WHEN $15 = '' THEN NULL ELSE 256 END,
		  CASE WHEN $15 = '' THEN NULL ELSE $11 END,
		  (SELECT id FROM tenant_workflow_states
		    WHERE tenant_id = $1 AND is_default AND archived_at IS NULL
		    ORDER BY position LIMIT 1))
		ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
		DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    source = EXCLUDED.source,
		    source_meta = EXCLUDED.source_meta,
		    content = EXCLUDED.content,
		    page_url = EXCLUDED.page_url,
		    enrichment_status = EXCLUDED.enrichment_status,
		    enriched_title = EXCLUDED.enriched_title,
		    enriched_display_title = EXCLUDED.enriched_display_title,
		    enriched_display_rationale = EXCLUDED.enriched_display_rationale,
		    enriched_attrs = EXCLUDED.enriched_attrs,
		    is_urgent = EXCLUDED.is_urgent,
		    classification_confidence = EXCLUDED.classification_confidence,
		    language = EXCLUDED.language,
		    enriched_at = EXCLUDED.enriched_at,
		    created_at = EXCLUDED.created_at,
		    updated_at = EXCLUDED.updated_at,
		    embedding = EXCLUDED.embedding,
		    embedding_model = EXCLUDED.embedding_model,
		    embedding_dims = EXCLUDED.embedding_dims,
		    embedded_at = EXCLUDED.embedded_at,
		    deleted_at = NULL
		RETURNING id`,
		tenantID, seed.UserID, seed.Source, metaJSON, seed.Content, seed.PageURL,
		seed.Title, attrsJSON, isDemoUrgent(seed.Attrs), seed.Confidence, now,
		createdAt, idemKey, hash[:], embedding,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		return 0, fmt.Errorf("upsert demo feedback %q: %w", seed.Key, err)
	}
	return id, nil
}

func insertDemoSemanticRun(ctx context.Context, pool *pgxpool.Pool, tenantID string, feedbackID int64, seed demoFeedbackSeed, now time.Time) error {
	attrsJSON, err := json.Marshal(seed.Attrs)
	if err != nil {
		return fmt.Errorf("marshal semantic attrs: %w", err)
	}
	confidenceJSON, err := json.Marshal(map[string]any{
		"overall": seed.Confidence,
	})
	if err != nil {
		return fmt.Errorf("marshal semantic confidence: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO semantic_extraction_runs
		 (tenant_id, subject_type, subject_id, domain_pack, schema_version,
		  prompt_version, model, attrs, confidence, rationale, dropped_attrs,
		  source, logical_model, provider_model, channel_id, channel_name, created_at)
		VALUES
		 ($1, $2, $3, $4, $5, 'demo-seed.v1', 'demo-classifier',
		  $6::jsonb, $7::jsonb, '{}'::jsonb, '{}'::jsonb,
		  $8, 'demo-classifier', 'demo-classifier-v1', 'demo', 'Demo', $9)`,
		tenantID, feedbackrepo.SemanticSubjectFeedback, feedbackID,
		feedbackrepo.DefaultDomainPack, feedbackrepo.DefaultSchemaVersion,
		attrsJSON, confidenceJSON, seed.Source, now.Add(-time.Duration(seed.AgeHours)*time.Hour),
	)
	if err != nil {
		return fmt.Errorf("insert demo semantic run: %w", err)
	}
	return nil
}

func seedDemoSearchTelemetry(ctx context.Context, repo *feedbackrepo.FeedbackRepo, tenantID string, feedbackIDs []int64) error {
	for i := 0; i < 12; i++ {
		runID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("attune-demo-search-%02d", i))).String()
		resultCount := 6
		if i < 3 {
			resultCount = 0
		}
		fallback := i == 3 || i == 4
		if err := repo.RecordSearchRun(ctx, feedbackrepo.SearchRunInsert{
			TenantID:            tenantID,
			RunID:               runID,
			QueryHash:           demoHash(fmt.Sprintf("query-%02d", i)),
			QueryPreview:        demoSearchQuery(i),
			FilterHash:          demoHash("default-filter"),
			RankingVersion:      semanticsearch.RankingVersion,
			EmbeddingModel:      "text-embedding-3-small",
			ResultCount:         resultCount,
			UsedKeywordFallback: fallback,
			FallbackReason:      demoFallbackReason(fallback),
			LatencyMS:           demoLatency(i),
			TotalLiveFeedback:   len(feedbackIDs),
			TotalWithEmbeddings: 8,
			CoverageRatio:       8.0 / float64(len(feedbackIDs)),
			ActorUserID:         demoSeedActor,
		}); err != nil {
			return fmt.Errorf("record demo search run: %w", err)
		}
		if resultCount > 0 && i%2 == 0 {
			if err := repo.RecordSearchResultEvent(ctx, feedbackrepo.SearchResultEventInsert{
				TenantID:    tenantID,
				RunID:       runID,
				FeedbackID:  feedbackIDs[i%len(feedbackIDs)],
				Action:      "open",
				Rank:        1,
				MatchType:   "hybrid",
				ActorUserID: demoSeedActor,
			}); err != nil {
				return fmt.Errorf("record demo search event: %w", err)
			}
		}
	}
	return nil
}

func seedDemoQualityActions(ctx context.Context, repo *feedbackrepo.FeedbackRepo, tenantID string) error {
	_, err := repo.UpsertQualityActionStatus(ctx, feedbackrepo.QualityActionUpsert{
		TenantID:          tenantID,
		ActionKey:         "control_tower.zero_result",
		Signal:            "zero-result",
		Status:            feedbackrepo.QualityActionStatusAcknowledged,
		Severity:          feedbackrepo.QualityActionSeverityAlert,
		TargetPath:        "/analytics/search-quality",
		MetricLabel:       "Zero-result search",
		MetricValue:       "25%",
		RecommendationKey: "control_tower.action.zero_result",
		EvidenceJSON:      `{"query":"enterprise export webhook","source":"demo seed"}`,
		ActorUserID:       demoSeedActor,
	})
	if err != nil {
		return fmt.Errorf("seed demo quality action: %w", err)
	}
	return nil
}

func demoFeedbackSeeds() []demoFeedbackSeed {
	return []demoFeedbackSeed{
		{Key: "export-timeout", Source: "api", UserID: "maya@northstar.example", Content: "CSV export times out on workspaces with more than 80k rows. Our renewal review depends on this report.", PageURL: "https://app.example.com/reports/export", Title: "Large CSV export times out", Attrs: map[string]any{"type": "bug", "severity": "critical", "sentiment": "frustrated", "labels": []string{"export", "enterprise"}}, Confidence: 0.91, AgeHours: 6, Embedding: true},
		{Key: "audit-log-filter", Source: "webhook", UserID: "samir@aurora.example", Content: "The audit log needs filtering by actor before we can hand it to compliance.", PageURL: "https://app.example.com/audit", Title: "Audit log actor filter request", Attrs: map[string]any{"type": "feature", "severity": "major", "sentiment": "neutral", "labels": []string{"compliance"}}, Confidence: 0.88, AgeHours: 10, Embedding: true},
		{Key: "snowflake-connector", Source: "email", UserID: "nora@atlas.example", Content: "Can Attune ingest from Snowflake or a warehouse table? We do not want to copy feedback exports by hand.", PageURL: "https://docs.example.com/imports", Title: "Warehouse connector request", Attrs: map[string]any{"type": "integration", "severity": "major", "sentiment": "neutral", "labels": []string{"warehouse"}}, Confidence: 0.52, AgeHours: 14, Embedding: true},
		{Key: "sso-loop", Source: "api", UserID: "eli@meridian.example", Content: "SSO returns to the login page after Okta approval. It happens for two admins on Safari.", PageURL: "https://app.example.com/login", Title: "SSO approval loop", Attrs: map[string]any{"type": "bug", "severity": "urgent", "sentiment": "negative", "labels": []string{"sso"}}, Confidence: 0.49, AgeHours: 20, Embedding: true},
		{Key: "reply-draft-tone", Source: "webhook", UserID: "ines@kepler.example", Content: "The reply draft is useful, but the tone is too formal for our support team.", PageURL: "https://app.example.com/feedback/42", Title: "Reply draft tone too formal", Attrs: map[string]any{"type": "feature", "severity": "minor", "sentiment": "neutral", "labels": []string{"reply-draft"}}, Confidence: 0.79, AgeHours: 28, Embedding: true},
		{Key: "billing-webhook", Source: "api", UserID: "owen@harbor.example", Content: "Webhook retries doubled our billing issue tickets because duplicate events show up as separate feedback.", PageURL: "https://app.example.com/integrations/webhook", Title: "Duplicate webhook feedback", Attrs: map[string]any{"type": "bug", "severity": "major", "sentiment": "frustrated", "labels": []string{"webhook", "billing"}}, Confidence: 0.83, AgeHours: 32, Embedding: true},
		{Key: "mobile-copy", Source: "web", UserID: "lea@copper.example", Content: "The mobile feedback detail view truncates the copy button label and the button jumps when loading.", PageURL: "https://app.example.com/mobile", Title: "Mobile detail action layout shifts", Attrs: map[string]any{"type": "bug", "severity": "minor", "sentiment": "negative", "labels": []string{"mobile"}}, Confidence: 0.76, AgeHours: 40, Embedding: true},
		{Key: "cluster-name", Source: "email", UserID: "pavel@cedar.example", Content: "Cluster names are good, but I need to rename a cluster before sending the digest to product.", PageURL: "https://app.example.com/clusters", Title: "Editable cluster label request", Attrs: map[string]any{"type": "feature", "severity": "minor", "sentiment": "neutral", "labels": []string{"clusters"}}, Confidence: 0.81, AgeHours: 48, Embedding: true},
		{Key: "api-docs", Source: "api", UserID: "mina@lumen.example", Content: "The API docs do not show a Python example for idempotent ingest.", PageURL: "https://docs.example.com/api", Title: "Python idempotency example missing", Attrs: map[string]any{"type": "question", "severity": "minor", "sentiment": "neutral", "labels": []string{"docs"}}, Confidence: 0.62, AgeHours: 54, Embedding: false},
		{Key: "search-empty", Source: "webhook", UserID: "tomas@vector.example", Content: "Searching for enterprise export webhook returns nothing even though the exact phrase exists in old support notes.", PageURL: "https://app.example.com/search", Title: "Search misses enterprise export webhook", Attrs: map[string]any{"type": "bug", "severity": "major", "sentiment": "frustrated", "labels": []string{"search"}}, Confidence: 0.58, AgeHours: 60, Embedding: false},
	}
}

func demoSearchQuery(i int) string {
	queries := []string{
		"enterprise export webhook",
		"sso audit trail owner",
		"billing connector snowflake",
		"reply draft tone",
		"duplicate webhook feedback",
		"mobile detail action",
	}
	return queries[i%len(queries)]
}

func demoFallbackReason(fallback bool) string {
	if fallback {
		return "embedding_unavailable"
	}
	return ""
}

func demoLatency(i int) int {
	if i >= 10 {
		return 3600
	}
	return 420 + i*70
}

func demoHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return fmt.Sprintf("%x", sum)
}

func demoEmbedding(seed int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 256; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		value := float64(((seed+1)*(i+3))%97) / 100
		b.WriteString(strconv.FormatFloat(value, 'f', 4, 64))
	}
	b.WriteByte(']')
	return b.String()
}

func demoAccount(index int) string {
	accounts := []string{"Northstar Bank", "Aurora Labs", "AtlasWorks", "Meridian Health"}
	return accounts[index%len(accounts)]
}

func demoPlan(index int) string {
	if index%3 == 0 {
		return "enterprise"
	}
	return "growth"
}

func isDemoUrgent(attrs map[string]any) bool {
	return attrs["severity"] == "critical" || attrs["severity"] == "urgent"
}
