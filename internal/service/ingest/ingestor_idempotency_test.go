package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const testKey = "abcd1234efgh5678"

func keyedInput(content string) domain.IngestInput {
	return domain.IngestInput{Content: content, Source: "api", IdempotencyKey: testKey}
}

func TestIngestRow_IdempotentReplayReturnsSameRowWithoutReinserting(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 42})
	submitter := ptrext.Of(fakeSubmitter{})
	ingestor := NewIngestor(repo, submitter, nil)

	first, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, keyedInput("checkout broke"))
	if err != nil {
		t.Fatalf("first IngestRow err = %v", err)
	}
	second, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, keyedInput("checkout broke"))
	if err != nil {
		t.Fatalf("second IngestRow err = %v", err)
	}
	if first != 42 || second != 42 {
		t.Fatalf("ids = (%d, %d), want both 42", first, second)
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts = %d, want 1 (replay must not re-insert)", repo.inserts)
	}
	// A deduped replay must NOT re-enqueue enrichment.
	if len(submitter.jobs) != 1 {
		t.Fatalf("enrichment submits = %d, want 1 (replay must not re-submit)", len(submitter.jobs))
	}
}

func TestIngestRow_SameKeyDifferentBodyIsConflict(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 1})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), nil)

	if _, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, keyedInput("first body")); err != nil {
		t.Fatalf("first IngestRow err = %v", err)
	}
	_, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, keyedInput("DIFFERENT body"))
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestIngestRow_MalformedKeyRejected(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 1})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), nil)

	in := domain.IngestInput{Content: "x", Source: "api", IdempotencyKey: "short"} // < 8 chars
	_, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, in)
	if !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("err = %v, want ErrInvalidIdempotencyKey", err)
	}
	if repo.inserts != 0 {
		t.Fatalf("inserts = %d, want 0", repo.inserts)
	}
}

func TestIngestRow_NoKeyUsesPlainInsert(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 9})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{}), nil)

	id, err := ingestor.IngestRow(context.Background(), "t1", uuid.Nil, domain.IngestInput{Content: "x", Source: "api"})
	if err != nil || id != 9 {
		t.Fatalf("id=%d err=%v, want id=9 err=nil", id, err)
	}
	if repo.inserts != 1 {
		t.Fatalf("inserts = %d, want 1", repo.inserts)
	}
}
