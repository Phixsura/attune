package main

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeFileReportsStringLiteralCode(t *testing.T) {
	t.Parallel()

	path := writeLintFixture(t, `
package fixture

var _ = attunev1.ErrorResponse{Code: "VALIDATION"}
`)

	got, err := analyzeFile(token.NewFileSet(), path)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, ruleCodeLiteral, got[0].Rule)
}

func TestAnalyzeFileAllowsEnumString(t *testing.T) {
	t.Parallel()

	path := writeLintFixture(t, `
package fixture

var _ = attunev1.ErrorResponse{Code: attunev1.ErrorCode_VALIDATION.String()}
`)

	got, err := analyzeFile(token.NewFileSet(), path)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAnalyzeFileAllowsLineDirective(t *testing.T) {
	t.Parallel()

	path := writeLintFixture(t, `
package fixture

var _ = attunev1.ErrorResponse{Code: "VALIDATION"} // errorcode:allow fixture
`)

	got, err := analyzeFile(token.NewFileSet(), path)
	require.NoError(t, err)
	require.Empty(t, got)
}

func writeLintFixture(t *testing.T, src string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.go")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
	return path
}
