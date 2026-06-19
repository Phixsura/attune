package ingest

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/idempotency"
)

const testKey = "abcd1234efgh5678"

// memIdem is an in-memory idempotency.Store with the real Acquire semantics
// (new → pending+acquired; same hash → existing; different hash → mismatch).
type memIdem struct {
	mu   sync.Mutex
	keys map[string]*idempotency.Key
}

func newMemIdem() *memIdem { return ptrext.Of(memIdem{keys: map[string]*idempotency.Key{}}) }

func (m *memIdem) Acquire(_ context.Context, tenantID, key string, hash []byte, _ time.Duration) (*idempotency.Key, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := tenantID + "|" + key
	if existing, ok := m.keys[id]; ok {
		if !bytes.Equal(existing.RequestHash, hash) {
			return nil, false, idempotency.ErrHashMismatch
		}
		return existing, false, nil
	}
	rec := ptrext.Of(idempotency.Key{TenantID: tenantID, Key: key, RequestHash: hash, Status: idempotency.StatusPending})
	m.keys[id] = rec
	return rec, true, nil
}

func (m *memIdem) Complete(_ context.Context, tenantID, key string, code int, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.keys[tenantID+"|"+key]; ok {
		rec.Status = idempotency.StatusCompleted
		rec.ResponseCode = code
		rec.ResponseBody = body
	}
	return nil
}

func (m *memIdem) Fail(_ context.Context, tenantID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.keys[tenantID+"|"+key]; ok {
		rec.Status = idempotency.StatusFailed
	}
	return nil
}

func (m *memIdem) Get(context.Context, string, string) (*idempotency.Key, error) {
	return nil, idempotency.ErrNotFound
}
func (m *memIdem) Delete(context.Context, string, string) error  { return nil }
func (m *memIdem) CleanupExpired(context.Context) (int64, error) { return 0, nil }

func newInput(content string) domain.IngestInput {
	return domain.IngestInput{Content: content, Source: "api", IdempotencyKey: testKey}
}

func TestIngestRow_IdempotentReplayReturnsSameRowWithoutReinserting(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 42})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), newMemIdem())

	first, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, newInput("checkout broke"))
	if err != nil {
		t.Fatalf("first IngestRow err = %v", err)
	}
	second, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, newInput("checkout broke"))
	if err != nil {
		t.Fatalf("second IngestRow err = %v", err)
	}
	if first != 42 || second != 42 {
		t.Fatalf("ids = (%d, %d), want both 42", first, second)
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts = %d, want 1 (replay must not re-insert)", repo.inserts)
	}
}

func TestIngestRow_SameKeyDifferentBodyIsConflict(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 1})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), newMemIdem())

	if _, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, newInput("first body")); err != nil {
		t.Fatalf("first IngestRow err = %v", err)
	}
	_, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, newInput("DIFFERENT body"))
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestIngestRow_PendingKeyIsRequestInProgress(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 1})
	store := newMemIdem()
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), store)

	in := newInput("concurrent body")
	// Simulate a concurrent in-flight request holding the key (pending).
	if _, _, err := store.Acquire(context.Background(), "t1", testKey, hashIngest("t1", in), 0); err != nil {
		t.Fatalf("seed acquire err = %v", err)
	}
	_, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, in)
	if !errors.Is(err, ErrRequestInProgress) {
		t.Fatalf("err = %v, want ErrRequestInProgress", err)
	}
	if repo.inserts != 0 {
		t.Fatalf("inserts = %d, want 0 (in-progress must not insert)", repo.inserts)
	}
}

func TestIngestRow_MalformedKeyRejected(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 1})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), newMemIdem())

	in := domain.IngestInput{Content: "x", Source: "api", IdempotencyKey: "short"} // < 8 chars
	_, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, in)
	if !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("err = %v, want ErrInvalidIdempotencyKey", err)
	}
	if repo.inserts != 0 {
		t.Fatalf("inserts = %d, want 0", repo.inserts)
	}
}

func TestIngestRow_NoKeyStillInserts(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 9})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), newMemIdem())

	// No IdempotencyKey → idempotency path skipped, plain insert.
	id, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, domain.IngestInput{Content: "x", Source: "api"})
	if err != nil || id != 9 {
		t.Fatalf("id=%d err=%v, want id=9 err=nil", id, err)
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts = %d, want 1", repo.inserts)
	}
}
