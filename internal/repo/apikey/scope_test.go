package apikey

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/domain"
)

func TestScopesToStrings(t *testing.T) {
	scopes := []domain.Scope{domain.ScopeFeedbackRead, domain.ScopeIngestWrite}
	strs := scopesToStrings(scopes)
	assert.Equal(t, []string{"feedback:read", "ingest:write"}, strs)
}

func TestStringsToScopes(t *testing.T) {
	strs := []string{"feedback:read", "ingest:write"}
	scopes := stringsToScopes(strs)
	assert.Equal(t, []domain.Scope{domain.ScopeFeedbackRead, domain.ScopeIngestWrite}, scopes)
}
