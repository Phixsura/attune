// Package system implements Console system administration endpoints.
package system

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	defaultReleaseOwnerTeam  = "Platform"
	defaultReleaseRunbookURL = "https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md"
	defaultReleaseEscalation = "https://github.com/Phixsura/attune/issues/new/choose"
)

// ReleaseHandler serves the current runtime, release, and ownership metadata.
type ReleaseHandler struct {
	cfg       *config.Config
	startedAt time.Time
}

// ReleaseInfo describes the current runtime release context.
type ReleaseInfo struct {
	ServiceVersion     string                      `json:"serviceVersion"`
	Environment        string                      `json:"environment"`
	Profile            string                      `json:"profile"`
	LifecycleState     string                      `json:"lifecycleState"`
	OwnerTeam          string                      `json:"ownerTeam"`
	CompatibilityRules []domain.SemanticDescriptor `json:"compatibilityRules"`
	Glossary           []domain.SemanticDescriptor `json:"glossary"`
	RunbookURL         string                      `json:"runbookUrl"`
	EscalationURL      string                      `json:"escalationUrl,omitempty"`
	StartedAt          string                      `json:"startedAt"`
}

// NewReleaseHandler creates a handler that serves runtime release metadata.
func NewReleaseHandler(cfg *config.Config) *ReleaseHandler {
	return ptrext.Of(ReleaseHandler{
		cfg:       cfg,
		startedAt: time.Now().UTC(),
	})
}

// ServeHTTP returns the current release and ownership metadata as JSON.
func (h *ReleaseHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info := ReleaseInfo{
		OwnerTeam:          defaultReleaseOwnerTeam,
		RunbookURL:         defaultReleaseRunbookURL,
		EscalationURL:      defaultReleaseEscalation,
		CompatibilityRules: domain.CompatibilityRules(),
		Glossary:           domain.PlatformGlossary(),
	}
	if h != nil && h.cfg != nil {
		info.ServiceVersion = h.cfg.Observability.ServiceVersion
		info.Environment = h.cfg.Observability.Environment
		info.Profile = h.cfg.Profile
	}
	semantics := domain.RuntimeSemantics(info.Profile, info.ServiceVersion)
	info.LifecycleState = semantics.LifecycleState.String()
	info.CompatibilityRules = semantics.CompatibilityRules
	info.Glossary = semantics.Glossary
	if h != nil && !h.startedAt.IsZero() {
		info.StartedAt = h.startedAt.Format(time.RFC3339Nano)
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(info)
}
