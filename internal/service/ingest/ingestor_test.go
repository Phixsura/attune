package ingest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/trace"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/enrich"
)

func TestScrubUntrustedSourceMeta_StripsReservedInboundSourceKeys(t *testing.T) {
	keyID := uuid.New()
	in := domain.IngestInput{
		SourceMeta: map[string]any{
			domain.SourceMetaInboundSourceID:   "source-1",
			domain.SourceMetaInboundSourceName: "Support",
			"safe":                             "kept",
		},
	}
	got := scrubUntrustedSourceMeta(keyID, in)
	if _, ok := got.SourceMeta[domain.SourceMetaInboundSourceID]; ok {
		t.Fatalf("reserved source id survived: %+v", got.SourceMeta)
	}
	if _, ok := got.SourceMeta[domain.SourceMetaInboundSourceName]; ok {
		t.Fatalf("reserved source name survived: %+v", got.SourceMeta)
	}
	if got.SourceMeta["safe"] != "kept" {
		t.Fatalf("non-reserved meta changed: %+v", got.SourceMeta)
	}
	if _, ok := in.SourceMeta[domain.SourceMetaInboundSourceID]; !ok {
		t.Fatalf("input meta was mutated: %+v", in.SourceMeta)
	}
}

func TestScrubUntrustedSourceMeta_PreservesAdapterSourceKeys(t *testing.T) {
	in := domain.IngestInput{
		SourceMeta: map[string]any{
			domain.SourceMetaInboundSourceID: "source-1",
		},
	}
	got := scrubUntrustedSourceMeta(uuid.Nil, in)
	if got.SourceMeta[domain.SourceMetaInboundSourceID] != "source-1" {
		t.Fatalf("adapter source id was stripped: %+v", got.SourceMeta)
	}
}

func TestIngestRowSubmitsEnrichmentJob(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 7})
	submitter := ptrext.Of(fakeSubmitter{})
	ingestor := NewIngestor(repo, submitter, nil)

	id, err := ingestor.IngestRow(context.Background(), "tenant-1", uuid.Nil, domain.IngestInput{
		Content: "checkout is broken",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("IngestRow err = %v", err)
	}
	if id != 7 {
		t.Fatalf("id = %d, want 7", id)
	}
	if len(submitter.jobs) != 1 {
		t.Fatalf("submitted jobs = %d, want 1", len(submitter.jobs))
	}
	if submitter.jobs[0].ID != 7 {
		t.Fatalf("submitted job id = %d, want 7", submitter.jobs[0].ID)
	}
	if submitter.jobs[0].TraceID == "" {
		t.Fatal("submitted job trace id must not be empty")
	}
	if repo.lastInsert.SignalTraceID != submitter.jobs[0].TraceID {
		t.Fatalf("persisted signal trace %q != submitted trace %q", repo.lastInsert.SignalTraceID, submitter.jobs[0].TraceID)
	}
}

func TestIngestRowUsesSourceSignalTraceForPersistenceAndJob(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 9})
	submitter := ptrext.Of(fakeSubmitter{})
	ingestor := NewIngestor(repo, submitter, nil)
	ctx := trace.WithID(context.Background(), "request-trace-1")

	id, err := ingestor.IngestRow(ctx, "tenant-1", uuid.Nil, domain.IngestInput{
		Content:    "checkout is broken",
		Source:     "api",
		SourceMeta: map[string]any{"source_event_id": "zendesk-ticket-42"},
	})
	if err != nil {
		t.Fatalf("IngestRow err = %v", err)
	}
	if id != 9 {
		t.Fatalf("id = %d, want 9", id)
	}
	if repo.lastInsert.SignalTraceID != "zendesk-ticket-42" {
		t.Fatalf("signal trace = %q, want source event", repo.lastInsert.SignalTraceID)
	}
	if len(submitter.jobs) != 1 || submitter.jobs[0].TraceID != "zendesk-ticket-42" {
		t.Fatalf("job trace = %+v, want source event trace", submitter.jobs)
	}
}

func TestIngestRowQueueSubmitFailureDoesNotFailRequest(t *testing.T) {
	repo := ptrext.Of(fakeFeedbackRepo{insertID: 11})
	ingestor := NewIngestor(repo, ptrext.Of(fakeSubmitter{err: errors.New("queue full")}), nil)

	id, err := ingestor.IngestRow(context.Background(), "tenant-1", uuid.Nil, domain.IngestInput{
		Content: "search is slow",
		Source:  "api",
	})
	if err != nil {
		t.Fatalf("IngestRow err = %v", err)
	}
	if id != 11 {
		t.Fatalf("id = %d, want 11", id)
	}
}

// fakeFeedbackRepo simulates the partial-unique-index dedup of the real
// InsertIdempotent: first key+hash inserts; a replay with the same hash dedups;
// the same key with a different hash conflicts.
type fakeFeedbackRepo struct {
	insertID   int64
	inserts    int
	seen       map[string][]byte
	ids        map[string]int64
	lastInsert domain.IngestInput
}

func (f *fakeFeedbackRepo) Insert(
	_ context.Context,
	_, _, _, _, _ string,
	in domain.IngestInput,
) (int64, error) {
	f.inserts++
	f.lastInsert = in
	return f.insertID, nil
}

func (f *fakeFeedbackRepo) InsertIdempotent(
	_ context.Context,
	_, _, _, _, _ string,
	in domain.IngestInput,
	idemHash []byte,
) (int64, bool, error) {
	if f.seen == nil {
		f.seen = map[string][]byte{}
		f.ids = map[string]int64{}
	}
	f.lastInsert = in
	if h, ok := f.seen[in.IdempotencyKey]; ok {
		if !bytes.Equal(h, idemHash) {
			return 0, false, feedbackrepo.ErrIdempotencyConflict
		}
		return f.ids[in.IdempotencyKey], true, nil
	}
	f.inserts++
	f.seen[in.IdempotencyKey] = idemHash
	f.ids[in.IdempotencyKey] = f.insertID
	return f.insertID, false, nil
}

type fakeSubmitter struct {
	jobs []enrich.Job
	err  error
}

func (f *fakeSubmitter) Submit(_ context.Context, job enrich.Job) error {
	f.jobs = append(f.jobs, job)
	return f.err
}
