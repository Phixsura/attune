// ptrext:file-allow test fixtures use in-memory fakes.
package outbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

type workerFakeOutboxStore struct {
	rows     []outboxrepo.OutboxRow
	claimErr error

	claimN     int
	claimOwner string

	resetCalls int
	resetCount int64
	resetErr   error

	deliveredIDs      []int64
	markDeliveredZero bool
	markDeliveredErr  error

	failed        []workerFailedRecord
	markFailedErr error

	dead         []workerDeadRecord
	markDeadZero bool
	markDeadErr  error
}

type workerFailedRecord struct {
	id          int64
	owner       string
	errMsg      string
	failureKind string
	httpStatus  int
	nextDelay   time.Duration
}

type workerDeadRecord struct {
	id          int64
	owner       string
	reason      string
	failureKind string
	httpStatus  int
}

func (f *workerFakeOutboxStore) ResetStaleClaims(_ context.Context) (int64, error) {
	f.resetCalls++
	return f.resetCount, f.resetErr
}

func (f *workerFakeOutboxStore) ClaimBatch(
	_ context.Context,
	n int,
	owner string,
) ([]outboxrepo.OutboxRow, error) {
	f.claimN = n
	f.claimOwner = owner
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	return append([]outboxrepo.OutboxRow(nil), f.rows...), nil
}

func (f *workerFakeOutboxStore) RefreshClaims(
	_ context.Context,
	_ []int64,
	_ string,
) (int64, error) {
	return 0, nil
}

func (f *workerFakeOutboxStore) MarkDelivered(_ context.Context, id int64, _ string) (int64, error) {
	if f.markDeliveredErr != nil {
		return 0, f.markDeliveredErr
	}
	f.deliveredIDs = append(f.deliveredIDs, id)
	if f.markDeliveredZero {
		return 0, nil
	}
	return 1, nil
}

func (f *workerFakeOutboxStore) MarkFailed(
	_ context.Context,
	id int64,
	owner string,
	errMsg string,
	failureKind string,
	httpStatus int,
	nextDelay time.Duration,
) (int64, error) {
	f.failed = append(f.failed, workerFailedRecord{
		id:          id,
		owner:       owner,
		errMsg:      errMsg,
		failureKind: failureKind,
		httpStatus:  httpStatus,
		nextDelay:   nextDelay,
	})
	if f.markFailedErr != nil {
		return 0, f.markFailedErr
	}
	return 1, nil
}

func (f *workerFakeOutboxStore) MarkDead(
	_ context.Context,
	id int64,
	owner string,
	reason string,
	failureKind string,
	httpStatus int,
) (int64, error) {
	f.dead = append(f.dead, workerDeadRecord{
		id:          id,
		owner:       owner,
		reason:      reason,
		failureKind: failureKind,
		httpStatus:  httpStatus,
	})
	if f.markDeadErr != nil {
		return 0, f.markDeadErr
	}
	if f.markDeadZero {
		return 0, nil
	}
	return 1, nil
}

type workerFakeTargetStore struct {
	target *notifytarget.NotifyTarget
	err    error

	clearRecords []workerTargetRecord
	clearErr     error

	touchRecords []workerTargetRecord
	touchErr     error
}

type workerTargetRecord struct {
	tenantID string
	destType string
	url      string
	audience string
	errMsg   string
}

func (f *workerFakeTargetStore) GetByTenantAudience(
	_ context.Context,
	tenantID string,
	destType string,
	audience string,
) (*notifytarget.NotifyTarget, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.target == nil {
		return nil, notifytarget.ErrNotifyTargetNotFound
	}
	if f.target.TenantID != "" && f.target.TenantID != tenantID {
		return nil, notifytarget.ErrNotifyTargetNotFound
	}
	if f.target.DestinationType != "" && f.target.DestinationType != destType {
		return nil, notifytarget.ErrNotifyTargetNotFound
	}
	if f.target.Audience != "" && f.target.Audience != audience {
		return nil, notifytarget.ErrNotifyTargetNotFound
	}
	return f.target, nil
}

func (f *workerFakeTargetStore) ClearFailure(
	_ context.Context,
	tenantID string,
	destType string,
	url string,
	audience string,
) error {
	f.clearRecords = append(f.clearRecords, workerTargetRecord{
		tenantID: tenantID,
		destType: destType,
		url:      url,
		audience: audience,
	})
	return f.clearErr
}

func (f *workerFakeTargetStore) TouchFailure(
	_ context.Context,
	tenantID string,
	destType string,
	url string,
	audience string,
	errMsg string,
) error {
	f.touchRecords = append(f.touchRecords, workerTargetRecord{
		tenantID: tenantID,
		destType: destType,
		url:      url,
		audience: audience,
		errMsg:   errMsg,
	})
	return f.touchErr
}

func TestOutboxWorkerProcessOnceDeliversClaimedRows(t *testing.T) {
	t.Parallel()
	chID := "test-process-ok-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	outboxStore := ptrext.Of(workerFakeOutboxStore{
		rows: []outboxrepo.OutboxRow{
			testWorkerOutboxRow(101, chID, srv.URL, 0),
		},
	})
	targetStore := ptrext.Of(workerFakeTargetStore{
		target: testWorkerNotifyTarget(chID, srv.URL, false),
	})
	w := testWorker(outboxStore, targetStore, notify.NewTransport(srv.Client(), notify.NoRetry()))

	w.ProcessOnce(context.Background())

	require.Equal(t, 2, outboxStore.claimN)
	require.Equal(t, "test-owner", outboxStore.claimOwner)
	require.Equal(t, []int64{101}, outboxStore.deliveredIDs)
	require.Empty(t, outboxStore.failed)
	require.Empty(t, outboxStore.dead)
	require.Equal(t, int32(1), requests.Load())
	require.Len(t, targetStore.clearRecords, 1)
	require.Empty(t, targetStore.touchRecords)
}

func TestOutboxWorkerProcessOnceHandlesClaimErrorAndEmptyBatch(t *testing.T) {
	t.Parallel()
	claimErrStore := ptrext.Of(workerFakeOutboxStore{claimErr: errors.New("claim unavailable")})
	w := testWorker(claimErrStore, ptrext.Of(workerFakeTargetStore{}), nil)

	w.ProcessOnce(context.Background())

	require.Equal(t, 2, claimErrStore.claimN)
	require.Empty(t, claimErrStore.deliveredIDs)
	require.Empty(t, claimErrStore.failed)
	require.Empty(t, claimErrStore.dead)

	emptyStore := ptrext.Of(workerFakeOutboxStore{})
	w = testWorker(emptyStore, ptrext.Of(workerFakeTargetStore{}), nil)
	w.ProcessOnce(context.Background())

	require.Equal(t, 2, emptyStore.claimN)
	require.Empty(t, emptyStore.deliveredIDs)
	require.Empty(t, emptyStore.failed)
	require.Empty(t, emptyStore.dead)
}

func TestOutboxWorkerRunResetsStaleClaimsBeforeCancellation(t *testing.T) {
	t.Parallel()
	outboxStore := ptrext.Of(workerFakeOutboxStore{resetCount: 2})
	w := testWorker(outboxStore, ptrext.Of(workerFakeTargetStore{}), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w.Run(ctx)

	require.Equal(t, 1, outboxStore.resetCalls)
}

func TestOutboxWorkerProcessRowMarksDeadForUnsupportedDestination(t *testing.T) {
	t.Parallel()
	outboxStore := ptrext.Of(workerFakeOutboxStore{})
	targetStore := ptrext.Of(workerFakeTargetStore{})
	w := testWorker(outboxStore, targetStore, nil)
	row := testWorkerOutboxRow(201, "test-process-unknown-"+uuid.NewString()[:8], "https://example.test/hook", 0)

	w.processRow(context.Background(), row)

	require.Len(t, outboxStore.dead, 1)
	require.Contains(t, outboxStore.dead[0].reason, "unsupported destination_type")
	require.Equal(t, string(notify.KindTerminal), outboxStore.dead[0].failureKind)
	require.Len(t, targetStore.touchRecords, 1)
	require.Empty(t, outboxStore.failed)
}

func TestOutboxWorkerProcessRowMarksDeadWhenTargetUnavailable(t *testing.T) {
	chID := "test-process-target-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})

	tests := []struct {
		name       string
		target     *notifytarget.NotifyTarget
		targetErr  error
		wantReason string
	}{
		{
			name:       "missing",
			targetErr:  notifytarget.ErrNotifyTargetNotFound,
			wantReason: "destination not found",
		},
		{
			name:       "disabled",
			target:     testWorkerNotifyTarget(chID, "https://example.test/hook", true),
			wantReason: "destination disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outboxStore := ptrext.Of(workerFakeOutboxStore{})
			targetStore := ptrext.Of(workerFakeTargetStore{target: tt.target, err: tt.targetErr})
			w := testWorker(outboxStore, targetStore, nil)

			w.processRow(
				context.Background(),
				testWorkerOutboxRow(211, chID, "https://example.test/hook", 0),
			)

			require.Len(t, outboxStore.dead, 1)
			require.Contains(t, outboxStore.dead[0].reason, tt.wantReason)
			require.Equal(t, string(notify.KindTerminal), outboxStore.dead[0].failureKind)
			require.Len(t, targetStore.touchRecords, 1)
			require.Empty(t, outboxStore.failed)
		})
	}
}

func TestOutboxWorkerProcessRowRetriesThenDiesOnLookupError(t *testing.T) {
	chID := "test-process-lookup-" + uuid.NewString()[:8]
	registerTestChannel(t, &testStubChannel{id: chID})
	lookupErr := errors.New("target database unavailable")

	tests := []struct {
		name        string
		attempts    int
		wantFailed  bool
		wantDead    bool
		wantBackoff time.Duration
	}{
		{name: "retry", attempts: 0, wantFailed: true, wantBackoff: 30 * time.Second},
		{name: "dead", attempts: 2, wantDead: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outboxStore := ptrext.Of(workerFakeOutboxStore{})
			targetStore := ptrext.Of(workerFakeTargetStore{err: lookupErr})
			w := testWorker(outboxStore, targetStore, nil)

			w.processRow(
				context.Background(),
				testWorkerOutboxRow(221, chID, "https://example.test/hook", tt.attempts),
			)

			if tt.wantFailed {
				require.Len(t, outboxStore.failed, 1)
				require.Contains(t, outboxStore.failed[0].errMsg, "lookup destination")
				require.Equal(t, string(notify.KindOther), outboxStore.failed[0].failureKind)
				require.Equal(t, tt.wantBackoff, outboxStore.failed[0].nextDelay)
				require.Empty(t, outboxStore.dead)
			}
			if tt.wantDead {
				require.Len(t, outboxStore.dead, 1)
				require.Contains(t, outboxStore.dead[0].reason, "exceeded 3 attempts")
				require.Equal(t, string(notify.KindOther), outboxStore.dead[0].failureKind)
				require.Len(t, targetStore.touchRecords, 1)
				require.Empty(t, outboxStore.failed)
			}
		})
	}
}

func TestOutboxWorkerProcessRowClassifiesSendFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		wantDead       bool
		wantFailure    bool
		wantKind       notify.FailureKind
		wantHTTPStatus int
	}{
		{
			name:           "terminal 4xx",
			status:         http.StatusForbidden,
			wantDead:       true,
			wantKind:       notify.KindHTTP4xx,
			wantHTTPStatus: http.StatusForbidden,
		},
		{
			name:           "retryable 5xx",
			status:         http.StatusInternalServerError,
			wantFailure:    true,
			wantKind:       notify.KindHTTP5xx,
			wantHTTPStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chID := "test-process-http-" + uuid.NewString()[:8]
			registerTestChannel(t, &testStubChannel{id: chID})

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)

			outboxStore := ptrext.Of(workerFakeOutboxStore{})
			targetStore := ptrext.Of(workerFakeTargetStore{
				target: testWorkerNotifyTarget(chID, srv.URL, false),
			})
			w := testWorker(outboxStore, targetStore, notify.NewTransport(srv.Client(), notify.NoRetry()))

			w.processRow(context.Background(), testWorkerOutboxRow(231, chID, srv.URL, 0))

			if tt.wantDead {
				require.Len(t, outboxStore.dead, 1)
				require.Equal(t, string(tt.wantKind), outboxStore.dead[0].failureKind)
				require.Equal(t, tt.wantHTTPStatus, outboxStore.dead[0].httpStatus)
				require.Len(t, targetStore.touchRecords, 1)
				require.Empty(t, outboxStore.failed)
			}
			if tt.wantFailure {
				require.Len(t, outboxStore.failed, 1)
				require.Equal(t, string(tt.wantKind), outboxStore.failed[0].failureKind)
				require.Equal(t, tt.wantHTTPStatus, outboxStore.failed[0].httpStatus)
				require.Equal(t, 30*time.Second, outboxStore.failed[0].nextDelay)
				require.Empty(t, outboxStore.dead)
				require.Empty(t, targetStore.touchRecords)
			}
		})
	}
}

func testWorker(
	outboxStore *workerFakeOutboxStore,
	targetStore *workerFakeTargetStore,
	transport *notify.Transport,
) *OutboxWorker {
	if transport == nil {
		transport = notify.NewTransport(http.DefaultClient, notify.NoRetry())
	}
	w := NewOutboxWorker(nil, nil, transport)
	w.outbox = outboxStore
	w.targets = targetStore
	w.owner = "test-owner"
	w.pollInterval = time.Hour
	w.batchSize = 2
	w.maxAttempts = 3
	return w
}

func testWorkerOutboxRow(id int64, destType string, url string, attempts int) outboxrepo.OutboxRow {
	return outboxrepo.OutboxRow{
		ID:                id,
		FeedbackID:        501,
		TenantID:          "tenant-1",
		DestinationType:   destType,
		DestinationTarget: url,
		Audience:          "ops",
		Payload:           validPayload(),
		Attempts:          attempts,
		TraceID:           "trace-test",
	}
}

func testWorkerNotifyTarget(destType string, url string, disabled bool) *notifytarget.NotifyTarget {
	return ptrext.Of(notifytarget.NotifyTarget{
		ID:              uuid.New(),
		TenantID:        "tenant-1",
		DestinationType: destType,
		Audience:        "ops",
		URL:             url,
		Secret:          strings.Repeat("s", 16),
		Disabled:        disabled,
	})
}
