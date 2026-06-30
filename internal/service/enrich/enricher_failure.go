package enrich

import (
	"strings"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func (e *Enricher) failureSnapshot(cfg ClassifyConfig, route llmclient.RouteMetadata, err error) feedback.EnrichmentFailureSnapshot {
	policy := resolvePromptPolicy(cfg)
	snapshot := feedback.EnrichmentFailureSnapshot{
		ReasonClass:       failureReasonClass(err),
		Model:             strings.TrimSpace(route.ProviderModel),
		ChannelID:         strings.TrimSpace(route.ChannelID),
		ChannelName:       strings.TrimSpace(route.ChannelName),
		ConfigFingerprint: terminalFailureConfigFingerprint(policy),
		PromptVersion:     policy.PromptVersion,
	}
	if snapshot.Model == "" {
		snapshot.Model = strings.TrimSpace(route.LogicalModel)
	}
	if snapshot.Model == "" {
		snapshot.Model = strings.TrimSpace(e.model)
	}
	return snapshot
}

func failureReasonClass(err error) string {
	if err == nil {
		return "other_err"
	}
	msg := err.Error()
	if len(msg) >= 4 && msg[:4] == "llm:" {
		return "llm_err"
	}
	if len(msg) >= 5 && msg[:5] == "parse" {
		return "parse_err"
	}
	return "other_err"
}

func normalizeFailureReasonClass(class string) string {
	switch strings.TrimSpace(class) {
	case "llm_err", "parse_err", "other_err":
		return class
	default:
		return "other_err"
	}
}
