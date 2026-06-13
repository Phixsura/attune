// SPDX-License-Identifier: Apache-2.0

// Package replydraft generates operator-facing empathetic reply drafts for
// enriched feedback. A draft is a suggestion only — it is never auto-sent;
// the operator reviews, edits, and sends from Console.
package replydraft

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

const (
	// draftPurpose tags the LLM call so the audit wrapper records cost under
	// this purpose in llm_audit.
	draftPurpose    = "reply_draft"
	draftSystemUser = "system-reply-drafter"
	draftMaxTokens  = 256
	draftTemp       = 0.4 // warmer than classification (0.0) for a natural tone
	draftMaxRunes   = 2000
)

// ReplyDrafter produces and stores a reply draft for one feedback row. It is
// shared by the async worker and the synchronous Regenerate endpoint.
type ReplyDrafter struct {
	repo *replydraftrepo.DraftTaskRepo
	llm  llmclient.LLMClient
}

func NewReplyDrafter(repo *replydraftrepo.DraftTaskRepo, llm llmclient.LLMClient) *ReplyDrafter {
	return ptrext.Of(ReplyDrafter{repo: repo, llm: llm})
}

// Generate loads the enriched row, prompts the LLM, and overwrites
// user_feedback.reply_draft. The LLM call goes through the audit-wrapping
// client (Guard.Purpose='reply_draft'), so token/cost are recorded
// automatically — no explicit audit write here.
func (d *ReplyDrafter) Generate(ctx context.Context, feedbackID int64, tenantID string) (string, time.Time, error) {
	const where = "service.replydraft.Generate"
	in, err := d.repo.LoadForDraft(ctx, feedbackID, tenantID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("load: %w", err)
	}
	req := llmclient.CompletionRequest{
		Prompt:      renderDraftPrompt(ptrext.Indirect(in)),
		Temperature: draftTemp,
		MaxTokens:   draftMaxTokens,
		UserID:      draftSystemUser,
		Guard: llmclient.GuardMetadata{
			TenantID:   in.TenantID,
			FeedbackID: feedbackID,
			Purpose:    draftPurpose,
		},
	}
	resp, err := d.llm.Complete(ctx, req)
	if err != nil {
		logext.Errorf(ctx, "[%s] llm.Complete failed,feedback_id:%d,err:%+v", where, feedbackID, err.Error())
		return "", time.Time{}, fmt.Errorf("llm: %w", err)
	}
	draft := cleanDraft(resp.Text)
	generatedAt, err := d.repo.UpdateReplyDraft(ctx, feedbackID, tenantID, draft)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("persist: %w", err)
	}
	return draft, generatedAt, nil
}

// Precheck returns the enrichment status, the tenant's reply-draft opt-in flag,
// and whether the row is owned by the tenant — the Regenerate endpoint's guard
// inputs. found=false means the row does not exist or is not this tenant's, so
// the handler never has to import the repo's sentinel error.
func (d *ReplyDrafter) Precheck(ctx context.Context, feedbackID int64, tenantID string) (status string, enabled, found bool, err error) {
	status, enabled, err = d.repo.DraftPrecheck(ctx, feedbackID, tenantID)
	if errors.Is(err, replydraftrepo.ErrNotFound) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return status, enabled, true, nil
}

// renderDraftPrompt builds the prompt from the enriched row. sentiment and
// the other classification dimensions live in enriched_attrs (a configurable
// dimension map), not fixed columns — they are read by name and skipped when
// absent. A per-tenant template, when set, takes over via {placeholder}
// substitution.
func renderDraftPrompt(in replydraftrepo.DraftInput) string {
	if tmpl := strings.TrimSpace(in.PromptTemplate); tmpl != "" {
		return applyTemplate(tmpl, in)
	}
	lang := in.Language
	if lang == "" {
		lang = "the customer's language"
	}
	var b strings.Builder
	b.WriteString("You are a customer support operator. Draft a brief, empathetic reply ")
	b.WriteString("to the feedback below. Write 2-3 sentences in ")
	b.WriteString(lang)
	b.WriteString(". Acknowledge the issue, show you understand, and set one clear next-step ")
	b.WriteString("expectation. Do not promise timelines or fixes you cannot guarantee. ")
	b.WriteString("Output only the reply text.\n\nFeedback: ")
	b.WriteString(in.Content)
	b.WriteString("\n")
	if line := contextLine(in); line != "" {
		b.WriteString(line)
	}
	b.WriteString("\nReply draft:")
	return b.String()
}

func contextLine(in replydraftrepo.DraftInput) string {
	var parts []string
	if in.EnrichedTitle != "" {
		parts = append(parts, "title="+in.EnrichedTitle)
	}
	for _, k := range []string{"kind", "severity", "modules", "sentiment"} {
		if v := attrToString(in.Attrs[k]); v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Context: " + strings.Join(parts, ", ") + "\n"
}

func applyTemplate(tmpl string, in replydraftrepo.DraftInput) string {
	return strings.NewReplacer(
		"{content}", in.Content,
		"{language}", in.Language,
		"{title}", in.EnrichedTitle,
		"{kind}", attrToString(in.Attrs["kind"]),
		"{severity}", attrToString(in.Attrs["severity"]),
		"{modules}", attrToString(in.Attrs["modules"]),
		"{sentiment}", attrToString(in.Attrs["sentiment"]),
	).Replace(tmpl)
}

// attrToString flattens an enriched_attrs value. After JSON decode a
// single-kind dim is a string and a multi-kind dim is a []any of strings.
func attrToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []any:
		ss := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				ss = append(ss, s)
			}
		}
		return strings.Join(ss, "/")
	default:
		return fmt.Sprintf("%v", x)
	}
}

// preambleLine matches a whole first line that is purely a label like
// "Here's a draft:", "Reply draft:", or "Draft:" — anchored start-to-end so a
// real sentence that merely contains "here"/"reply" and ends in ':' is NOT
// stripped.
var preambleLine = regexp.MustCompile(`(?i)^\s*(here'?s\s+|here\s+is\s+)?(a\s+|the\s+|your\s+)?(reply\s+)?draft(\s+reply)?\s*:\s*$|^\s*reply\s*:\s*$`)

// cleanDraft trims the model output, drops a leading pure-preamble line, and
// caps the length by rune count (never splitting a multi-byte character).
func cleanDraft(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		// Only strip a short, anchored preamble line — not a real first
		// sentence that happens to end in ':' and contain "reply"/"here".
		if first := strings.TrimSpace(s[:idx]); len(first) < 40 && preambleLine.MatchString(first) {
			s = strings.TrimSpace(s[idx+1:])
		}
	}
	if r := []rune(s); len(r) > draftMaxRunes {
		s = strings.TrimSpace(string(r[:draftMaxRunes]))
	}
	return s
}
