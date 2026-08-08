// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func TestNPSCampaignRunEvidenceExportFreezesArtifactAndTracksDownload(t *testing.T) {
	t.Parallel()

	campaignID := uuid.New()
	runID := uuid.New()
	generatedAt := time.Date(2026, 8, 8, 1, 2, 3, 0, time.UTC)
	store := ptrext.Of(npsEvidenceExportTestRepo{
		npsPreflightRepo: ptrext.Of(npsPreflightRepo{
			fakeRepo: ptrext.Of(fakeRepo{analytics: repo.Analytics{
				InvitationCount: 8,
				CompletedCount:  4,
				NPS:             25,
				NPSAvailable:    true,
				ScoreDistribution: []repo.ScoreBucket{
					{Score: 0, Count: 1},
					{Score: 7, Count: 1},
					{Score: 9, Count: 2},
				},
			}}),
			npsRunPage: repo.NPSCampaignRunPage{Runs: []repo.NPSCampaignRun{{
				ID:             runID,
				CampaignID:     campaignID,
				Sequence:       7,
				Status:         repo.NPSRunClosed,
				ScheduledAt:    generatedAt,
				MeasurementKey: "nps:v4:opaque",
				NPSAvailable:   true,
			}}},
		}),
	})
	service := ptrext.Of(Service{
		repo: store,
		now: func() time.Time {
			return generatedAt
		},
	})

	created, err := service.CreateNPSCampaignRunEvidenceExport(
		context.Background(), "tenant-1", campaignID, runID, "", "operator-1",
	)
	if err != nil {
		t.Fatalf("CreateNPSCampaignRunEvidenceExport() error = %v", err)
	}
	want, err := BuildNPSCampaignRunEvidenceCSV(
		NPSCampaignRunEvidence{
			Run:       store.npsRunPage.Runs[0],
			Analytics: store.analytics,
		},
		generatedAt,
	)
	if err != nil {
		t.Fatalf("BuildNPSCampaignRunEvidenceCSV() error = %v", err)
	}
	if !bytes.Equal(created.Artifact, want) {
		t.Fatalf("created artifact changed during persistence")
	}
	digest := sha256.Sum256(want)
	if created.ArtifactSHA256 != "sha256:"+fmt.Sprintf("%x", digest) {
		t.Fatalf("artifact hash = %q, want sha256:%x", created.ArtifactSHA256, digest)
	}
	if created.CreatedByType != "admin" || created.CreatedBy != "operator-1" {
		t.Fatalf("creator = %q/%q, want admin/operator-1", created.CreatedByType, created.CreatedBy)
	}

	store.summaries = []repo.NPSCampaignRunEvidenceExportSummary{{
		ID:             created.ID,
		CampaignID:     campaignID,
		RunID:          runID,
		ReportVersion:  created.ReportVersion,
		GeneratedAt:    created.GeneratedAt,
		ArtifactSHA256: created.ArtifactSHA256,
	}}
	history, err := service.ListNPSCampaignRunEvidenceExports(context.Background(), "tenant-1", campaignID, runID, 20)
	if err != nil || len(history) != 1 || history[0].ID != created.ID {
		t.Fatalf("ListNPSCampaignRunEvidenceExports() = %#v, %v", history, err)
	}

	downloaded, err := service.DownloadNPSCampaignRunEvidenceExport(
		context.Background(), "tenant-1", campaignID, runID, created.ID,
	)
	if err != nil {
		t.Fatalf("DownloadNPSCampaignRunEvidenceExport() error = %v", err)
	}
	if !bytes.Equal(downloaded.Artifact, created.Artifact) || downloaded.DownloadedAt == nil {
		t.Fatalf("downloaded artifact = %#v, want exact persisted bytes and timestamp", downloaded)
	}
}

func TestNPSCampaignRunEvidenceExportPurgeForwardsTenantCounts(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(npsEvidenceExportTestRepo{
		npsPreflightRepo: ptrext.Of(npsPreflightRepo{fakeRepo: ptrext.Of(fakeRepo{})}),
		purgeCounts:      map[string]int64{"tenant-1": 2},
	})
	service := ptrext.Of(Service{repo: store})

	counts, err := service.PurgeExpiredNPSCampaignRunEvidenceExports(
		context.Background(), time.Time{}, 10,
	)
	if err != nil {
		t.Fatalf("PurgeExpiredNPSCampaignRunEvidenceExports() error = %v", err)
	}
	if counts["tenant-1"] != 2 || store.purgeLimit != 10 {
		t.Fatalf("purge result = %#v, limit = %d", counts, store.purgeLimit)
	}
}

type npsEvidenceExportTestRepo struct {
	*npsPreflightRepo
	created     repo.NPSCampaignRunEvidenceExport
	summaries   []repo.NPSCampaignRunEvidenceExportSummary
	purgeCounts map[string]int64
	purgeLimit  int
}

func (r *npsEvidenceExportTestRepo) PurgeExpiredNPSCampaignRunEvidenceExports(
	_ context.Context,
	_ time.Time,
	limit int,
) (map[string]int64, error) {
	r.purgeLimit = limit
	return r.purgeCounts, nil
}

func (r *npsEvidenceExportTestRepo) CreateNPSCampaignRunEvidenceExport(
	_ context.Context,
	item repo.NPSCampaignRunEvidenceExport,
) (repo.NPSCampaignRunEvidenceExport, error) {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = item.GeneratedAt
	}
	r.created = item
	return item, nil
}

func (r *npsEvidenceExportTestRepo) FindNPSCampaignRunEvidenceExportByRequestKey(
	_ context.Context,
	_ string,
	_, _, requestKey uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, error) {
	if r.created.ClientRequestKey != requestKey {
		return repo.NPSCampaignRunEvidenceExport{}, repo.ErrNotFound
	}
	return r.created, nil
}

func (r *npsEvidenceExportTestRepo) GetNPSCampaignRunEvidenceExport(
	_ context.Context,
	_ string,
	_, _, exportID uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, error) {
	if r.created.ID != exportID {
		return repo.NPSCampaignRunEvidenceExport{}, repo.ErrNotFound
	}
	return r.created, nil
}

func (r *npsEvidenceExportTestRepo) ListNPSCampaignRunEvidenceExports(
	_ context.Context,
	_ string,
	_, _ uuid.UUID,
	_ int,
) ([]repo.NPSCampaignRunEvidenceExportSummary, error) {
	return r.summaries, nil
}

func (r *npsEvidenceExportTestRepo) MarkNPSCampaignRunEvidenceExportDownloaded(
	_ context.Context,
	_ string,
	_, _, exportID uuid.UUID,
) (repo.NPSCampaignRunEvidenceExport, error) {
	if r.created.ID != exportID {
		return repo.NPSCampaignRunEvidenceExport{}, repo.ErrNotFound
	}
	downloadedAt := r.created.GeneratedAt.Add(time.Minute)
	r.created.DownloadedAt = ptrext.Of(downloadedAt)
	return r.created, nil
}
