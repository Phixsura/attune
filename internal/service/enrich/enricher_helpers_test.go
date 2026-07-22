// SPDX-License-Identifier: Apache-2.0

package enrich

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

func TestEnricherMarkFailedRecordsTerminalFailure(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackEnrichRepo{
		markFailedTerminal: true,
		markFailedTenant:   "tenant-a",
	})
	enricher := ptrext.Of(Enricher{repo: repo})
	snapshot := feedback.EnrichmentFailureSnapshot{ReasonClass: "parse_err"}

	enricher.markFailed(context.Background(), 42, "bad json", snapshot)

	require.Equal(t, 1, repo.markFailedCalls)
	require.Equal(t, int64(42), repo.markFailedID)
	require.Equal(t, "bad json", repo.markFailedMessage)
	require.Equal(t, snapshot, repo.markFailedSnapshot)
}

func TestEnricherPersistIgnoredMarksDoneWithSentinel(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackEnrichRepo{})
	enricher := ptrext.Of(Enricher{repo: repo})
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "tenant-a",
		Content:       "Billing export is noisy",
		DisplayLocale: "en-US",
	})

	err := enricher.persistIgnored(context.Background(), 43, row, "noise")

	require.NoError(t, err)
	require.Equal(t, 1, repo.markDoneCalls)
	require.Equal(t, int64(43), repo.markDoneID)
	require.Equal(t, "[triage-ignored]", repo.markDoneEnriched.Title)
	require.Equal(t, "triage v0: noise", repo.markDoneEnriched.Rationale)
	require.Equal(t, LanguageEnglish, repo.markDoneMeta.Language)
	require.Equal(t, "en-US", repo.markDoneMeta.DisplayLocale)
}

func TestEnricherPersistIgnoredMarksFailedWhenPersistFails(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackEnrichRepo{markDoneErr: errors.New("write failed")})
	enricher := ptrext.Of(Enricher{repo: repo})
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "tenant-a",
		Content:       "Billing export is noisy",
		DisplayLocale: "en-US",
	})

	err := enricher.persistIgnored(context.Background(), 44, row, "noise")

	require.ErrorContains(t, err, "mark ignored row done")
	require.Equal(t, 1, repo.markFailedCalls)
	require.Equal(t, int64(44), repo.markFailedID)
	require.NotEmpty(t, repo.markFailedSnapshot.ReasonClass)
}

func TestEnricherPersistFromTriageMarksDone(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackEnrichRepo{})
	enricher := ptrext.Of(Enricher{repo: repo})
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "tenant-a",
		Source:        "api",
		Content:       "Please export invoices",
		DisplayLocale: "en-US",
	})

	err := enricher.persistFromTriage(context.Background(), 45, row, domain.Enriched{
		Title:     "Export invoices",
		Rationale: "Matched deterministic triage rule",
		Attrs:     map[string]any{"severity": "low"},
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.markDoneCalls)
	require.Equal(t, int64(45), repo.markDoneID)
	require.Equal(t, "Export invoices", repo.markDoneEnriched.Title)
	require.Equal(t, "Export invoices", repo.markDoneEnriched.DisplayTitle)
	require.Equal(t, LanguageEnglish, repo.markDoneMeta.Language)
	require.Equal(t, "en-US", repo.markDoneMeta.DisplayLocale)
}

func TestEnricherPersistFromTriageMarksFailedWhenPersistFails(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(fakeFeedbackEnrichRepo{markDoneErr: errors.New("write failed")})
	enricher := ptrext.Of(Enricher{repo: repo})
	row := ptrext.Of(feedback.EnrichInput{
		TenantID:      "tenant-a",
		Source:        "api",
		Content:       "Please export invoices",
		DisplayLocale: "en-US",
	})

	err := enricher.persistFromTriage(context.Background(), 46, row, domain.Enriched{
		Title:     "Export invoices",
		Rationale: "Matched deterministic triage rule",
	})

	require.ErrorContains(t, err, "write failed")
	require.Equal(t, 1, repo.markFailedCalls)
	require.Equal(t, int64(46), repo.markFailedID)
	require.NotEmpty(t, repo.markFailedSnapshot.ReasonClass)
}

func TestEnrichOneRejectsMissingLLMBeforeRepoUse(t *testing.T) {
	t.Parallel()

	err := ptrext.Of(Enricher{}).EnrichOne(context.Background(), 47)

	require.ErrorContains(t, err, "llm client not configured")
}

func TestEnrichOneClaimAndLoadBranches(t *testing.T) {
	t.Parallel()

	t.Run("claim error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(fakeFeedbackEnrichRepo{
			tryClaimConfigured: true,
			tryClaimErr:        errors.New("claim failed"),
		})
		enricher := ptrext.Of(Enricher{
			repo: repo,
			llm:  ptrext.Of(capturingEnrichLLM{text: `{"title":"Export","rationale":"Need CSV"}`}),
		})

		err := enricher.EnrichOne(context.Background(), 48)

		require.ErrorContains(t, err, "claim failed")
		require.Equal(t, 0, repo.markDoneCalls)
	})

	t.Run("claim contention", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(fakeFeedbackEnrichRepo{tryClaimConfigured: true})
		enricher := ptrext.Of(Enricher{
			repo: repo,
			llm:  ptrext.Of(capturingEnrichLLM{text: `{"title":"Export","rationale":"Need CSV"}`}),
		})

		err := enricher.EnrichOne(context.Background(), 49)

		require.NoError(t, err)
		require.Equal(t, 0, repo.markDoneCalls)
	})

	t.Run("load error", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(fakeFeedbackEnrichRepo{
			tryClaimConfigured: true,
			tryClaim:           true,
			loadConfigured:     true,
			loadErr:            errors.New("load failed"),
		})
		enricher := ptrext.Of(Enricher{
			repo: repo,
			llm:  ptrext.Of(capturingEnrichLLM{text: `{"title":"Export","rationale":"Need CSV"}`}),
		})

		err := enricher.EnrichOne(context.Background(), 50)

		require.ErrorContains(t, err, "load failed")
		require.Equal(t, 0, repo.markDoneCalls)
	})
}

func TestEnrichOneTriageAndLLMFailureBranches(t *testing.T) {
	t.Parallel()

	t.Run("triage ignore", func(t *testing.T) {
		t.Parallel()
		repo := ptrext.Of(fakeFeedbackEnrichRepo{
			tryClaimConfigured: true,
			tryClaim:           true,
			loadConfigured:     true,
			loadRow: ptrext.Of(feedback.EnrichInput{
				TenantID:      "tenant-a",
				Content:       "!!!!!!!!",
				DisplayLocale: "en-US",
			}),
		})
		llm := ptrext.Of(capturingEnrichLLM{text: `{"title":"Export","rationale":"Need CSV"}`})
		enricher := ptrext.Of(Enricher{repo: repo, llm: llm})

		err := enricher.EnrichOne(context.Background(), 51)

		require.NoError(t, err)
		require.Equal(t, 1, repo.markDoneCalls)
		require.Equal(t, "[triage-ignored]", repo.markDoneEnriched.Title)
		require.Empty(t, llm.lastReq.Prompt)
	})

	t.Run("llm transport error marks failed", func(t *testing.T) {
		t.Parallel()
		repo := claimedEnrichRepo("Please export invoices")
		enricher := ptrext.Of(Enricher{
			repo: repo,
			llm:  ptrext.Of(capturingEnrichLLM{err: errors.New("provider down")}),
		})

		err := enricher.EnrichOne(context.Background(), 52)

		require.ErrorContains(t, err, "llm")
		require.Equal(t, 1, repo.markFailedCalls)
		require.Contains(t, repo.markFailedMessage, "provider down")
	})

	t.Run("parse error marks failed", func(t *testing.T) {
		t.Parallel()
		repo := claimedEnrichRepo("Please export invoices")
		enricher := ptrext.Of(Enricher{
			repo: repo,
			llm:  ptrext.Of(capturingEnrichLLM{text: "not json"}),
		})

		err := enricher.EnrichOne(context.Background(), 53)

		require.ErrorContains(t, err, "parse")
		require.Equal(t, 1, repo.markFailedCalls)
		require.Contains(t, repo.markFailedSnapshot.ReasonClass, "parse")
	})
}

func TestEnrichPendingListAndLoopBranches(t *testing.T) {
	t.Parallel()

	errRepo := ptrext.Of(fakeFeedbackEnrichRepo{
		listPendingConfigured: true,
		listPendingErr:        errors.New("list failed"),
	})
	ptrext.Of(Enricher{repo: errRepo, llm: ptrext.Of(capturingEnrichLLM{})}).EnrichPending(context.Background(), 2)

	loopRepo := ptrext.Of(fakeFeedbackEnrichRepo{
		listPendingConfigured: true,
		pendingIDs:            []int64{54},
		tryClaimConfigured:    true,
		tryClaim:              true,
		loadConfigured:        true,
		loadErr:               errors.New("load failed"),
	})
	ptrext.Of(Enricher{
		repo: loopRepo,
		llm:  ptrext.Of(capturingEnrichLLM{text: `{"title":"Export","rationale":"Need CSV"}`}),
	}).EnrichPending(context.Background(), 1)

	require.Equal(t, 0, loopRepo.markDoneCalls)
}

func claimedEnrichRepo(content string) *fakeFeedbackEnrichRepo {
	return ptrext.Of(fakeFeedbackEnrichRepo{
		tryClaimConfigured: true,
		tryClaim:           true,
		loadConfigured:     true,
		loadRow: ptrext.Of(feedback.EnrichInput{
			TenantID:      "tenant-a",
			Source:        "api",
			Content:       content,
			DisplayLocale: "en-US",
		}),
	})
}

type fakeFeedbackEnrichRepo struct {
	markDoneErr error

	tryClaimConfigured    bool
	tryClaim              bool
	tryClaimErr           error
	loadConfigured        bool
	loadRow               *feedback.EnrichInput
	loadErr               error
	listPendingConfigured bool
	pendingIDs            []int64
	listPendingErr        error

	markDoneCalls    int
	markDoneID       int64
	markDoneEnriched domain.Enriched
	markDoneMeta     feedback.EnrichmentMetadata

	markFailedCalls    int
	markFailedID       int64
	markFailedMessage  string
	markFailedSnapshot feedback.EnrichmentFailureSnapshot
	markFailedTerminal bool
	markFailedTenant   string
}

func (r *fakeFeedbackEnrichRepo) TryClaim(context.Context, int64) (bool, error) {
	if !r.tryClaimConfigured {
		return false, errors.New("unexpected TryClaim call")
	}
	return r.tryClaim, r.tryClaimErr
}

func (r *fakeFeedbackEnrichRepo) LoadForEnrich(context.Context, int64) (*feedback.EnrichInput, error) {
	if !r.loadConfigured {
		return nil, errors.New("unexpected LoadForEnrich call")
	}
	return r.loadRow, r.loadErr
}

func (r *fakeFeedbackEnrichRepo) MarkFailed(
	_ context.Context,
	id int64,
	message string,
	snapshots ...feedback.EnrichmentFailureSnapshot,
) (bool, string) {
	r.markFailedCalls++
	r.markFailedID = id
	r.markFailedMessage = message
	if len(snapshots) > 0 {
		r.markFailedSnapshot = snapshots[0]
	}
	return r.markFailedTerminal, r.markFailedTenant
}

func (r *fakeFeedbackEnrichRepo) MarkDone(
	_ context.Context,
	id int64,
	enriched domain.Enriched,
	metas ...feedback.EnrichmentMetadata,
) error {
	r.markDoneCalls++
	r.markDoneID = id
	r.markDoneEnriched = enriched
	if len(metas) > 0 {
		r.markDoneMeta = metas[0]
	}
	return r.markDoneErr
}

func (r *fakeFeedbackEnrichRepo) BeginTx(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected BeginTx call")
}

func (r *fakeFeedbackEnrichRepo) MarkDoneTx(
	context.Context,
	pgx.Tx,
	int64,
	domain.Enriched,
	...feedback.EnrichmentMetadata,
) error {
	return errors.New("unexpected MarkDoneTx call")
}

func (r *fakeFeedbackEnrichRepo) InsertSemanticExtractionRunTx(
	context.Context,
	pgx.Tx,
	feedback.SemanticExtractionRun,
) (int64, error) {
	return 0, errors.New("unexpected InsertSemanticExtractionRunTx call")
}

func (r *fakeFeedbackEnrichRepo) ListPending(context.Context, int) ([]int64, error) {
	if !r.listPendingConfigured {
		return nil, errors.New("unexpected ListPending call")
	}
	return append([]int64(nil), r.pendingIDs...), r.listPendingErr
}
