package llmguard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestClient_RedactsPromptBeforeBackend(t *testing.T) {
	backend := ptrext.Of(recordingLLM{resp: `{"title":"ok","rationale":"ok"}`})
	client := NewClient(backend, StaticResolver{Plan: Plan{Rules: []Rule{{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionRedact,
	}}}})
	_, err := client.Complete(context.Background(), llmclient.CompletionRequest{
		Prompt: "contact alice@example.com",
		Guard:  llmclient.GuardMetadata{TenantID: "t1", Purpose: "enrich"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(backend.prompt, "alice@example.com") {
		t.Fatalf("backend saw raw email: %q", backend.prompt)
	}
	if !strings.Contains(backend.prompt, "<PII:email>") {
		t.Fatalf("backend prompt was not redacted: %q", backend.prompt)
	}
}

func TestClient_AuditDoesNotMutatePrompt(t *testing.T) {
	backend := ptrext.Of(recordingLLM{resp: `{"title":"ok","rationale":"ok"}`})
	client := NewClient(backend, StaticResolver{Plan: Plan{Rules: []Rule{{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"email"}, Action: ActionAudit,
	}}}})
	_, err := client.Complete(context.Background(), llmclient.CompletionRequest{Prompt: "alice@example.com"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if backend.prompt != "alice@example.com" {
		t.Fatalf("audit should not mutate prompt, got %q", backend.prompt)
	}
}

func TestClient_BlockSkipsBackend(t *testing.T) {
	backend := ptrext.Of(recordingLLM{resp: `{"title":"ok","rationale":"ok"}`})
	client := NewClient(backend, StaticResolver{Plan: Plan{Rules: []Rule{{
		Guard: "pii", Stage: StageLLMInput, Entities: []string{"credit_card"}, Action: ActionBlock,
	}}}})
	_, err := client.Complete(context.Background(), llmclient.CompletionRequest{Prompt: "card 4111 1111 1111 1111"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err: got %v want ErrBlocked", err)
	}
	if backend.calls != 0 {
		t.Fatalf("backend called despite block: %d", backend.calls)
	}
}

type recordingLLM struct {
	prompt string
	resp   string
	calls  int
}

func (r *recordingLLM) Complete(_ context.Context, req llmclient.CompletionRequest) (llmclient.CompletionResponse, error) {
	r.calls++
	r.prompt = req.Prompt
	return llmclient.CompletionResponse{Text: r.resp}, nil
}

func (r *recordingLLM) Close() error { return nil }
