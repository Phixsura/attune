// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test stub fixtures use address-of for interface satisfaction

package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestWorkerProcessNext_NoJobAvailable(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return nil, nil
		},
	}
	w := NewWorker(store, nil, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerProcessNext_ClaimError(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return nil, errors.New("claim failed")
		},
	}
	w := NewWorker(store, nil, nil)
	err := w.ProcessNextForTest(context.Background())
	if err == nil || err.Error() != "claim failed" {
		t.Fatalf("expected claim error, got %v", err)
	}
}

func TestWorkerProcessNext_FetchFails_MarksJobFailed(t *testing.T) {
	var failedErr string
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:       "job-1",
				TenantID: "t1",
				Status:   aerepo.JobRunning,
			}), nil
		},
		failJobFn: func(_ context.Context, _, _, errMsg string) (int64, error) {
			failedErr = errMsg
			return 1, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{}, errors.New("list error")
		},
	}
	w := NewWorker(store, lister, nil)
	err := w.ProcessNextForTest(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if failedErr != "list error" {
		t.Errorf("fail reason = %q, want 'list error'", failedErr)
	}
}

func TestWorkerProcessNext_CompletesSuccessfully(t *testing.T) {
	var completedID string
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:            "job-1",
				TenantID:      "t1",
				CreatedByType: "admin",
				CreatedBy:     "admin-1",
				FilterJSON:    json.RawMessage("{}"),
				Status:        aerepo.JobRunning,
			}), nil
		},
		completeJobFn: func(_ context.Context, jobID, _, _ string, _ []byte, _ int, _ time.Time) (int64, error) {
			completedID = jobID
			return 1, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{
					{ID: 1, TenantID: "t1", Action: "api_key.create", Summary: "test"},
				},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if completedID != "job-1" {
		t.Errorf("completed job = %s, want job-1", completedID)
	}
}

func TestWorkerProcessNext_ReClaimedJobIsNoop(t *testing.T) {
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:            "job-1",
				TenantID:      "t1",
				CreatedByType: "admin",
				CreatedBy:     "admin-1",
				FilterJSON:    json.RawMessage("{}"),
				Status:        aerepo.JobRunning,
			}), nil
		},
		completeJobFn: func(context.Context, string, string, string, []byte, int, time.Time) (int64, error) {
			return 0, nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "test", Summary: "s"}},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	if err := w.ProcessNextForTest(context.Background()); err != nil {
		t.Fatalf("expected nil for re-claimed job, got %v", err)
	}
}

func TestWorkerOptions(t *testing.T) {
	key := make([]byte, 32)
	w := NewWorker(nil, nil, nil,
		WithWorkerExportTTL(48*time.Hour),
		WithWorkerSigningKey(key),
	)
	if w.exportTTL != 48*time.Hour {
		t.Errorf("exportTTL = %v, want 48h", w.exportTTL)
	}
	if len(w.signKey) != 32 {
		t.Errorf("signKey len = %d, want 32", len(w.signKey))
	}
}

func TestWorkerAuditExpiry(t *testing.T) {
	recorder := &stubAuditRecorder{}
	w := NewWorker(nil, nil, recorder)
	w.auditExpiry(context.Background(), 3)
	if len(recorder.events) != 1 {
		t.Fatalf("events = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].Action != "audit_evidence.expire" {
		t.Errorf("action = %s", recorder.events[0].Action)
	}
}

func TestWorkerAuditExpiry_NilRecorder(t *testing.T) {
	w := NewWorker(nil, nil, nil)
	w.auditExpiry(context.Background(), 5)
}

func TestWorkerProcessNext_ContextCanceled_ReturnsNil(t *testing.T) {
	t.Parallel()

	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:       "job-ctx",
				TenantID: "t1",
				Status:   aerepo.JobRunning,
			}), nil
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{}, context.Canceled
		},
	}
	w := NewWorker(store, lister, nil)
	err := w.ProcessNextForTest(context.Background())
	require.NoError(t, err, "context.Canceled from fetchAllEntries should return nil")
}

func TestWorkerProcessNext_BuildArchiveError_MarksJobFailed(t *testing.T) {
	t.Parallel()

	var failedReason string
	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:            "job-bad-archive",
				TenantID:      "t1",
				CreatedByType: "admin",
				CreatedBy:     "admin-1",
				FilterJSON:    json.RawMessage("{}"),
				Status:        aerepo.JobRunning,
			}), nil
		},
		failJobFn: func(_ context.Context, _, _, errMsg string) (int64, error) {
			failedReason = errMsg
			return 1, nil
		},
	}
	// An entry whose AfterJSON is syntactically invalid causes marshalEntry
	// (called by buildChain inside buildArchive) to fail.
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{
					{
						ID:        1,
						TenantID:  "t1",
						Action:    "test",
						Summary:   "s",
						AfterJSON: json.RawMessage("{invalid"),
					},
				},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	err := w.ProcessNextForTest(context.Background())
	require.Error(t, err)
	require.NotEmpty(t, failedReason, "FailJob should have been called")
}

func TestWorkerProcessNext_CompleteJobError(t *testing.T) {
	t.Parallel()

	store := &stubJobStore{
		claimNextJobFn: func(context.Context, string) (*aerepo.ExportJob, error) {
			return ptrext.Of(aerepo.ExportJob{
				ID:            "job-cje",
				TenantID:      "t1",
				CreatedByType: "admin",
				CreatedBy:     "admin-1",
				FilterJSON:    json.RawMessage("{}"),
				Status:        aerepo.JobRunning,
			}), nil
		},
		completeJobFn: func(context.Context, string, string, string, []byte, int, time.Time) (int64, error) {
			return 0, errors.New("complete failed")
		},
	}
	lister := &stubAuditLister{
		listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
			return auditlogrepo.ListResult{
				Items: []auditlogrepo.Entry{
					{ID: 1, TenantID: "t1", Action: "test", Summary: "s"},
				},
			}, nil
		},
	}
	w := NewWorker(store, lister, nil)
	err := w.ProcessNextForTest(context.Background())
	require.Error(t, err)
	require.Equal(t, "complete failed", err.Error())
}

func TestFetchAllEntries(t *testing.T) {
	t.Parallel()

	t.Run("empty_filter_json", func(t *testing.T) {
		t.Parallel()

		lister := &stubAuditLister{
			listFn: func(_ context.Context, f auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
				require.Equal(t, "t1", f.TenantID)
				require.Empty(t, f.Actions)
				require.Empty(t, f.ActorType)
				return auditlogrepo.ListResult{
					Items: []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "a"}},
				}, nil
			},
		}
		w := NewWorker(nil, lister, nil)
		entries, err := w.fetchAllEntries(context.Background(), "t1", nil)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	})

	t.Run("invalid_filter_json", func(t *testing.T) {
		t.Parallel()

		w := NewWorker(nil, nil, nil)
		_, err := w.fetchAllEntries(context.Background(), "t1", json.RawMessage("{bad"))
		require.Error(t, err)
	})

	t.Run("populated_filter_fields", func(t *testing.T) {
		t.Parallel()

		from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		filter := ExportFilter{
			Actions:    []string{"api_key.create", "api_key.delete"},
			ActorType:  "admin",
			ActorID:    "actor-1",
			TargetType: "api_key",
			TargetID:   "key-1",
			From:       ptrext.Of(from),
			To:         ptrext.Of(to),
		}
		filterJSON, err := json.Marshal(filter)
		require.NoError(t, err)

		lister := &stubAuditLister{
			listFn: func(_ context.Context, f auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
				require.Equal(t, []string{"api_key.create", "api_key.delete"}, f.Actions)
				require.Equal(t, "admin", f.ActorType)
				require.Equal(t, "actor-1", f.ActorID)
				require.Equal(t, "api_key", f.TargetType)
				require.Equal(t, "key-1", f.TargetID)
				require.NotNil(t, f.From)
				require.Equal(t, from, *f.From) // ptrext:allow test-assertion
				require.NotNil(t, f.To)
				require.Equal(t, to, *f.To) // ptrext:allow test-assertion
				return auditlogrepo.ListResult{
					Items: []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "api_key.create"}},
				}, nil
			},
		}
		w := NewWorker(nil, lister, nil)
		entries, err := w.fetchAllEntries(context.Background(), "t1", filterJSON)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	})

	t.Run("pagination_follows_cursor", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		lister := &stubAuditLister{
			listFn: func(_ context.Context, f auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
				callCount++
				if callCount == 1 {
					require.Empty(t, f.Cursor, "first call should have no cursor")
					return auditlogrepo.ListResult{
						Items:      []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "a"}},
						NextCursor: "cursor-2",
					}, nil
				}
				require.Equal(t, "cursor-2", f.Cursor)
				return auditlogrepo.ListResult{
					Items: []auditlogrepo.Entry{{ID: 2, TenantID: "t1", Action: "b"}},
				}, nil
			},
		}
		w := NewWorker(nil, lister, nil)
		entries, err := w.fetchAllEntries(context.Background(), "t1", nil)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		require.Equal(t, 2, callCount)
	})

	t.Run("max_events_limit_stops_pagination", func(t *testing.T) {
		t.Parallel()

		batch := make([]auditlogrepo.Entry, DefaultMaxEventsLimit)
		for i := range batch {
			batch[i] = auditlogrepo.Entry{ID: int64(i + 1), TenantID: "t1", Action: "a"}
		}
		callCount := 0
		lister := &stubAuditLister{
			listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
				callCount++
				return auditlogrepo.ListResult{
					Items:      batch,
					NextCursor: "should-not-follow",
				}, nil
			},
		}
		w := NewWorker(nil, lister, nil)
		entries, err := w.fetchAllEntries(context.Background(), "t1", nil)
		require.NoError(t, err)
		require.Len(t, entries, DefaultMaxEventsLimit)
		require.Equal(t, 1, callCount, "should not follow cursor after hitting limit")
	})

	t.Run("context_canceled_mid_pagination", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		callCount := 0
		lister := &stubAuditLister{
			listFn: func(context.Context, auditlogsvc.ListFilter) (auditlogrepo.ListResult, error) {
				callCount++
				// After the first page returns, cancel the context so the
				// loop's ctx.Err() check fires before the second List call.
				cancel()
				return auditlogrepo.ListResult{
					Items:      []auditlogrepo.Entry{{ID: 1, TenantID: "t1", Action: "a"}},
					NextCursor: "next",
				}, nil
			},
		}
		w := NewWorker(nil, lister, nil)
		_, err := w.fetchAllEntries(ctx, "t1", nil)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, callCount, "should stop after context cancel")
	})
}

func TestWithWorkerExportTTL_ZeroAndNegative(t *testing.T) {
	t.Parallel()

	t.Run("zero_keeps_default", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, WithWorkerExportTTL(0))
		require.Equal(t, DefaultExportTTL, w.exportTTL)
	})

	t.Run("negative_keeps_default", func(t *testing.T) {
		t.Parallel()
		w := NewWorker(nil, nil, nil, WithWorkerExportTTL(-1*time.Hour))
		require.Equal(t, DefaultExportTTL, w.exportTTL)
	})
}

func TestWithWorkerSigningKey_Empty(t *testing.T) {
	t.Parallel()

	w := NewWorker(nil, nil, nil, WithWorkerSigningKey(nil))
	require.Nil(t, w.signKey)

	w2 := NewWorker(nil, nil, nil, WithWorkerSigningKey([]byte{}))
	require.Empty(t, w2.signKey)
}
