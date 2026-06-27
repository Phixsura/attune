package errorcode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()
	e := New(BadRequest, "invalid field")
	require.Equal(t, BadRequest, e.Code)
	require.Equal(t, "invalid field", e.Message)
	require.Nil(t, e.Details)
}

func TestWithDetails(t *testing.T) {
	t.Parallel()
	e := New(ValidationFailed, "two fields bad").WithDetails(map[string]string{"field": "email"})
	require.Equal(t, ValidationFailed, e.Code)
	require.NotNil(t, e.Details)
	m, ok := e.Details.(map[string]string)
	require.True(t, ok)
	require.Equal(t, "email", m["field"])
}
