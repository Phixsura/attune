package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
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

type fakeFeedbackRepo struct {
	insertID int64
	inserts  int
}

func (f *fakeFeedbackRepo) Insert(
	context.Context,
	string,
	string,
	string,
	string,
	string,
	domain.IngestInput,
) (int64, error) {
	f.inserts++
	return f.insertID, nil
}

type fakeSubmitter struct {
	jobs []enrich.Job
	err  error
}

func (f *fakeSubmitter) Submit(_ context.Context, job enrich.Job) error {
	f.jobs = append(f.jobs, job)
	return f.err
}
