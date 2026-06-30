package feedback

// EnrichmentFailureSnapshot captures the failure-time metadata that powers the
// terminal-failure workbench. It is intentionally small, stable, and string
// typed so callers can persist it without pulling in higher layers.
type EnrichmentFailureSnapshot struct {
	ReasonClass       string
	Model             string
	ChannelID         string
	ChannelName       string
	ConfigFingerprint string
	PromptVersion     string
}
