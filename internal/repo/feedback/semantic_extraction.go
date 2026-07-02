package feedback

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
)

const (
	SemanticSubjectFeedback = "feedback"
	DefaultDomainPack       = domain.CustomerFeedbackPackName
	DefaultSchemaVersion    = domain.CustomerFeedbackSchemaVersion
)

// SemanticExtractionRun records the evidence/provenance for one semantic
// extraction. user_feedback keeps the fast current-state snapshot; this table
// keeps append-only history for recompute, audit, and evaluation.
type SemanticExtractionRun struct {
	TenantID        string
	SubjectType     string
	SubjectID       int64
	DomainPack      string
	SchemaVersion   string
	PromptVersion   string
	PromptVersionID string
	Model           string
	Source          string
	LogicalModel    string
	ProviderModel   string
	ChannelID       string
	ChannelName     string
	GuardSummary    map[string]any
	Attrs           map[string]any
	Confidence      map[string]any
	Rationale       map[string]any
	DroppedAttrs    map[string]any
}

// InsertSemanticExtractionRunTx writes one extraction run inside the caller's
// transaction. The enricher uses this in the same tx as MarkDone/Outbox so a
// row is "done" iff its semantic evidence was captured too.
func (r *FeedbackRepo) InsertSemanticExtractionRunTx(
	ctx context.Context,
	tx pgx.Tx,
	run SemanticExtractionRun,
) (int64, error) {
	run = normalizeSemanticRun(run)
	guardJSON, err := jsonObject(run.GuardSummary)
	if err != nil {
		return 0, fmt.Errorf("marshal guard_summary: %w", err)
	}
	attrsJSON, err := jsonObject(run.Attrs)
	if err != nil {
		return 0, fmt.Errorf("marshal attrs: %w", err)
	}
	confidenceJSON, err := jsonObject(run.Confidence)
	if err != nil {
		return 0, fmt.Errorf("marshal confidence: %w", err)
	}
	rationaleJSON, err := jsonObject(run.Rationale)
	if err != nil {
		return 0, fmt.Errorf("marshal rationale: %w", err)
	}
	droppedJSON, err := jsonObject(run.DroppedAttrs)
	if err != nil {
		return 0, fmt.Errorf("marshal dropped_attrs: %w", err)
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO semantic_extraction_runs
		 (tenant_id, subject_type, subject_id, domain_pack, schema_version,
		  prompt_version, prompt_version_id, model, guard_summary, attrs, confidence, rationale,
		  dropped_attrs, source, logical_model, provider_model, channel_id, channel_name)
		VALUES
		 ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::uuid, $8, $9::jsonb, $10::jsonb, $11::jsonb,
		  $12::jsonb, $13::jsonb, $14, $15, $16, $17, $18)
		RETURNING id`,
		run.TenantID, run.SubjectType, run.SubjectID, run.DomainPack,
		run.SchemaVersion, run.PromptVersion, run.PromptVersionID, run.Model, guardJSON, attrsJSON,
		confidenceJSON, rationaleJSON, droppedJSON, run.Source, run.LogicalModel, run.ProviderModel,
		run.ChannelID, run.ChannelName,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert semantic extraction run: %w", err)
	}
	return id, nil
}

func normalizeSemanticRun(run SemanticExtractionRun) SemanticExtractionRun {
	if run.SubjectType == "" {
		run.SubjectType = SemanticSubjectFeedback
	}
	if run.DomainPack == "" {
		run.DomainPack = DefaultDomainPack
	}
	if run.SchemaVersion == "" {
		run.SchemaVersion = DefaultSchemaVersion
	}
	if run.PromptVersion == "" {
		run.PromptVersion = "default"
	}
	return run
}

func jsonObject(v map[string]any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte("{}"), nil
	}
	return b, nil
}
