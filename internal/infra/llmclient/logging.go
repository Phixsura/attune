package llmclient

import "strings"

func schemaName(req CompletionRequest) string {
	if req.Schema == nil {
		return ""
	}
	return strings.TrimSpace(req.Schema.Name)
}
