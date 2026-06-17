package enrichmentruntime

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
)

type fakeAuditRecorder struct {
	calls int
}

func (f *fakeAuditRecorder) Record(context.Context, auditlogsvc.Event) error {
	f.calls++
	return nil
}

func TestRequireStepUpRejectsMissingOrExpiredStepUp(t *testing.T) {
	t.Parallel()

	h := NewHandler(nil, 15*time.Minute)

	require.Error(t, h.requireStepUp(nil))
	require.Error(t, h.requireStepUp(ptrext.Of(session.AuthCtx{})))
	require.Error(t, h.requireStepUp(ptrext.Of(session.AuthCtx{
		StepUpAt: ptrext.Of(time.Now().Add(-16 * time.Minute)),
	})))
	require.NoError(t, h.requireStepUp(ptrext.Of(session.AuthCtx{
		StepUpAt: ptrext.Of(time.Now().Add(-5 * time.Minute)),
	})))
}

func TestToProtoReadModelIncludesHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 15, 4, 5, 0, time.UTC)
	readModel := enrichruntimesvc.ReadModel{
		BootstrapDefault: enrichrepo.Spec{
			QueueLen:            64,
			Workers:             8,
			BatchSize:           16,
			BatchWindow:         2 * time.Second,
			SweepInterval:       5 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           3.5,
			LLMBurst:            7,
		},
		DesiredSpec: enrichrepo.Spec{
			QueueLen:            128,
			Workers:             16,
			BatchSize:           32,
			BatchWindow:         time.Second,
			SweepInterval:       4 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           4.2,
			LLMBurst:            9,
		},
		DesiredRevision: enrichrepo.Policy{
			Version:                  9,
			UpdatedAt:                now,
			UpdatedBy:                "admin-1",
			UpdateReason:             "raise workers",
			BootstrapSnapshotVersion: "cfg-v3",
			SpecVersion:              1,
			LastKnownGoodVersion:     8,
		},
		Summary: enrichruntimesvc.Summary{
			DesiredVersion:        9,
			LiveInstances:         1,
			FullyAppliedInstances: 1,
			FullyConverged:        true,
		},
		History: []enrichrepo.HistoryEntry{{
			Policy: enrichrepo.Policy{
				Version:                  9,
				UpdatedAt:                now,
				UpdatedBy:                "admin-1",
				UpdateReason:             "raise workers",
				BootstrapSnapshotVersion: "cfg-v3",
				SpecVersion:              1,
				LastKnownGoodVersion:     8,
			},
			OperationType:   "update",
			RiskLevel:       "normal",
			SourceVersion:   8,
			TargetVersion:   9,
			RollbackLineage: "",
		}},
	}

	got := toProtoReadModel(readModel)

	require.NotNil(t, got)
	require.Len(t, got.GetHistory(), 1)
	require.Equal(t, uint64(9), got.GetHistory()[0].GetRevision().GetVersion())
	require.Equal(t, "update", got.GetHistory()[0].GetOperationType())
	require.Equal(t, "normal", got.GetHistory()[0].GetRiskLevel())
	require.Equal(t, uint64(8), got.GetHistory()[0].GetSourceVersion())
	require.Equal(t, uint64(9), got.GetHistory()[0].GetTargetVersion())
	require.Equal(t, "raise workers", got.GetDesiredRevision().GetUpdateReason())
	require.Equal(t, int32(128), got.GetDesiredSpec().GetQueueLen())
}

func TestRecordAuditSkipsWhenTenantScopeMissing(t *testing.T) {
	t.Parallel()

	audit := ptrext.Of(fakeAuditRecorder{})
	h := NewHandler(nil, 15*time.Minute)
	h.SetAuditLogger(audit)

	reqCtx := ptrext.Of(dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    ptrext.Of(session.AuthCtx{UserID: "admin-1"}),
	})

	err := h.recordAudit(reqCtx, "enrichment_runtime.update", "summary", enrichruntimesvc.ReadModel{}, enrichruntimesvc.ReadModel{})
	require.NoError(t, err)
	require.Equal(t, 0, audit.calls)
}
