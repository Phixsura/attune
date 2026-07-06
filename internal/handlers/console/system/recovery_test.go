// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures
package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/restoredrill"
)

type fakeRecoveryReader struct {
	row pgx.Row
}

func (f fakeRecoveryReader) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return f.row
}

type fakeRow struct {
	scanFn func(dest ...any) error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

func TestRecoveryHandler_ReturnsLatestRestoreDrill(t *testing.T) {
	t.Parallel()
	ranAt := time.Now().Add(-2 * 24 * time.Hour).UTC()
	h := NewRecoveryHandler(fakeRecoveryReader{
		row: fakeRow{
			scanFn: func(dest ...any) error {
				require.Len(t, dest, 5)
				*(dest[0].(*time.Time)) = ranAt
				*(dest[1].(*string)) = string(restoredrill.StatusPass)
				*(dest[2].(*string)) = "nightly-backup"
				*(dest[3].(*int64)) = 1234
				*(dest[4].(*json.RawMessage)) = json.RawMessage(`{"ok":true}`)
				return nil
			},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/recovery", nil)
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var info RecoveryInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	require.Equal(t, string(restoredrill.StatusPass), info.Status)
	require.Contains(t, info.Message, "passed")
	require.NotNil(t, info.LastRun)
	require.Equal(t, "nightly-backup", info.LastRun.BackupRef)
	require.NotNil(t, info.AgeSeconds)
	require.Greater(t, *info.AgeSeconds, int64(0))
}

func TestRecoveryHandler_NoRowsWarns(t *testing.T) {
	t.Parallel()
	h := NewRecoveryHandler(fakeRecoveryReader{
		row: fakeRow{
			scanFn: func(dest ...any) error {
				return pgx.ErrNoRows
			},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/recovery", nil)
	h.ServeHTTP(rec, req)

	var info RecoveryInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	require.Equal(t, string(restoredrill.StatusWarn), info.Status)
	require.Contains(t, info.Message, "No restore drill")
	require.Nil(t, info.LastRun)
}

func TestRecoveryHandler_ErrorFallsBackToWarn(t *testing.T) {
	t.Parallel()
	h := NewRecoveryHandler(fakeRecoveryReader{
		row: fakeRow{
			scanFn: func(dest ...any) error {
				return errors.New("db down")
			},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/recovery", nil)
	h.ServeHTTP(rec, req)

	var info RecoveryInfo
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&info))
	require.Equal(t, string(restoredrill.StatusWarn), info.Status)
	require.Contains(t, info.Message, "Recovery history unavailable")
}
