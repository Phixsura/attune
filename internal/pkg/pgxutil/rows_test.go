// ptrext:file-allow test mock rows use &mockRows for interface satisfaction
package pgxutil

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type mockRows struct {
	items   []string
	cursor  int
	closed  bool
	rowsErr error
}

func (m *mockRows) Close()                                       { m.closed = true }
func (m *mockRows) Err() error                                   { return m.rowsErr }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT") }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }
func (m *mockRows) Scan(_ ...any) error                          { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }

func (m *mockRows) Next() bool {
	if m.cursor >= len(m.items) {
		return false
	}
	m.cursor++
	return true
}

func scanString(rows pgx.Rows) (string, error) {
	r := rows.(*mockRows)
	return r.items[r.cursor-1], nil
}

func TestCollectRows_Success(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{"a", "b", "c"}}
	result, err := CollectRows[string](rows, scanString)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, result)
	require.True(t, rows.closed)
}

func TestCollectRows_Empty(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}}
	result, err := CollectRows[string](rows, scanString)
	require.NoError(t, err)
	require.Nil(t, result)
	require.True(t, rows.closed)
}

func TestCollectRows_ScanError(t *testing.T) {
	t.Parallel()
	errScan := errors.New("scan failed")
	rows := &mockRows{items: []string{"a"}}
	_, err := CollectRows[string](rows, func(_ pgx.Rows) (string, error) {
		return "", errScan
	})
	require.ErrorIs(t, err, errScan)
	require.True(t, rows.closed)
}

func TestCollectRows_RowsErr(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}, rowsErr: errors.New("connection lost")}
	_, err := CollectRows[string](rows, scanString)
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection lost")
}

func TestCollectOne_Success(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{"only"}}
	result, err := CollectOne[string](rows, scanString)
	require.NoError(t, err)
	require.Equal(t, "only", result)
	require.True(t, rows.closed)
}

func TestCollectOne_NoRows(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}}
	_, err := CollectOne[string](rows, scanString)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.True(t, rows.closed)
}

func TestCollectOne_NoRowsWithError(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}, rowsErr: errors.New("timeout")}
	_, err := CollectOne[string](rows, scanString)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout")
}

func TestCollectOne_ScanError(t *testing.T) {
	t.Parallel()
	errScan := errors.New("scan failed")
	rows := &mockRows{items: []string{"x"}}
	_, err := CollectOne[string](rows, func(_ pgx.Rows) (string, error) {
		return "", errScan
	})
	require.ErrorIs(t, err, errScan)
}

func TestCollectOne_RowsErrAfterScan(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{"x"}, rowsErr: errors.New("late error")}
	_, err := CollectOne[string](rows, scanString)
	require.Error(t, err)
	require.Contains(t, err.Error(), "late error")
}

func TestForEachRow_Success(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{"a", "b"}}
	var collected []string
	err := ForEachRow(rows, func(r pgx.Rows) error {
		s, _ := scanString(r)
		collected = append(collected, s)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, collected)
	require.True(t, rows.closed)
}

func TestForEachRow_Empty(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}}
	err := ForEachRow(rows, func(_ pgx.Rows) error {
		t.Fatal("should not be called")
		return nil
	})
	require.NoError(t, err)
}

func TestForEachRow_FnError(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{"a"}}
	errFn := errors.New("fn failed")
	err := ForEachRow(rows, func(_ pgx.Rows) error {
		return errFn
	})
	require.ErrorIs(t, err, errFn)
}

func TestForEachRow_RowsErr(t *testing.T) {
	t.Parallel()
	rows := &mockRows{items: []string{}, rowsErr: errors.New("conn error")}
	err := ForEachRow(rows, func(_ pgx.Rows) error { return nil })
	require.Error(t, err)
}
