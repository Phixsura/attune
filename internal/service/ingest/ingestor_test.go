package ingest

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
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
