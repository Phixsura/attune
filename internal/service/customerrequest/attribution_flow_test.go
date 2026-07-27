// SPDX-License-Identifier: Apache-2.0

package customerrequest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
)

// attrTx is a no-op pgx.Tx whose Begin returns a savepoint that records
// commit/rollback so the per-row isolation contract is assertable.
type attrTx struct {
	beginErr    error
	spCommitErr error
	savepoints  []*attrSavepoint
	commitCalls int
}

type attrSavepoint struct {
	attrTx
	commitErr  error
	committed  bool
	rolledBack bool
}

func (tx *attrTx) Begin(context.Context) (pgx.Tx, error) {
	if tx.beginErr != nil {
		return nil, tx.beginErr
	}
	sp := ptrext.Of(attrSavepoint{commitErr: tx.spCommitErr})
	tx.savepoints = append(tx.savepoints, sp)
	return sp, nil
}

func (sp *attrSavepoint) Commit(context.Context) error {
	if sp.commitErr != nil {
		return sp.commitErr
	}
	sp.committed = true
	return nil
}

func (sp *attrSavepoint) Rollback(context.Context) error {
	sp.rolledBack = true
	return nil
}

func (tx *attrTx) Commit(context.Context) error   { tx.commitCalls++; return nil }
func (tx *attrTx) Rollback(context.Context) error { return nil }
func (tx *attrTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *attrTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *attrTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (tx *attrTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *attrTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *attrTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (tx *attrTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (tx *attrTx) Conn() *pgx.Conn                                  { return nil }

// fakeRequestRepo scripts the requestRepo surface for the promote flow.
type fakeRequestRepo struct {
	requestRepo // panic on anything not overridden

	tx *attrTx

	sourceMetaByID map[int64]struct {
		source string
		meta   map[string]any
	}
	sourceMetaErr map[int64]error

	linkedInputs []repo.CustomerLinkInput
	linkErrOn    string // SubjectKey that fails LinkCustomerTx

	created   *repo.Summary
	createErr error
	linkFbErr error
	detail    *repo.Detail
	detailErr error
}

func (f *fakeRequestRepo) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func (f *fakeRequestRepo) CreateTx(_ context.Context, _ pgx.Tx, in repo.CreateInput) (*repo.Summary, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.created != nil {
		return f.created, nil
	}
	return ptrext.Of(repo.Summary{ID: uuid.MustParse("aaaaaaaa-1000-4000-8000-00000000abcd"), TenantID: in.TenantID, Title: in.Title}), nil
}

func (f *fakeRequestRepo) LinkFeedbackTx(context.Context, pgx.Tx, repo.LinkFeedbackInput) error {
	return f.linkFbErr
}

func (f *fakeRequestRepo) FeedbackSourceMetaTx(_ context.Context, _ pgx.Tx, _ string, feedbackID int64) (repo.FeedbackSourceMeta, error) {
	if err := f.sourceMetaErr[feedbackID]; err != nil {
		return repo.FeedbackSourceMeta{}, err
	}
	entry, ok := f.sourceMetaByID[feedbackID]
	if !ok {
		return repo.FeedbackSourceMeta{}, repo.ErrFeedbackNotFound
	}
	// Row subject identity mirrors ingest: email when present.
	subject, _ := entry.meta["intercom_contact_email"].(string)
	return repo.FeedbackSourceMeta{Source: entry.source, Meta: entry.meta, SubjectKey: subject}, nil
}

func (f *fakeRequestRepo) LinkCustomerTx(_ context.Context, _ pgx.Tx, in repo.CustomerLinkInput) (*repo.CustomerLink, error) {
	if f.linkErrOn != "" && in.SubjectKey == f.linkErrOn {
		return nil, errors.New("link boom")
	}
	f.linkedInputs = append(f.linkedInputs, in)
	return ptrext.Of(repo.CustomerLink{SubjectKey: in.SubjectKey}), nil
}

func (f *fakeRequestRepo) GetDetail(context.Context, string, uuid.UUID, int) (*repo.Detail, error) {
	if f.detailErr != nil {
		return nil, f.detailErr
	}
	if f.detail != nil {
		return f.detail, nil
	}
	return ptrext.Of(repo.Detail{}), nil
}

func intercomMeta(email, name string) map[string]any {
	return map[string]any{
		"intercom_workspace_id":          "ws1",
		"intercom_contact_email":         email,
		"intercom_contact_name":          name,
		"intercom_company_id":            "co-9",
		"intercom_company_name":          "Customer Co",
		"intercom_company_plan":          "Pro",
		"intercom_company_monthly_spend": float64(1200),
	}
}

// TestAutoLinkCustomersTx_FlowAndIsolation drives the real savepoint
// loop: identity dedup, no-identity skip, per-row failure isolation, and
// the derived note text.
func TestAutoLinkCustomersTx_FlowAndIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000301")

	f := ptrext.Of(fakeRequestRepo{
		tx: ptrext.Of(attrTx{}),
		sourceMetaByID: map[int64]struct {
			source string
			meta   map[string]any
		}{
			41: {source: "intercom", meta: intercomMeta("alice@customer.example", "Alice")},
			42: {source: "intercom", meta: intercomMeta("alice@customer.example", "Alice")}, // dup identity
			43: {source: "webhook", meta: map[string]any{"webhook_email": "x@y.z"}},         // non-channel
			44: {source: "intercom", meta: intercomMeta("bob@customer.example", "Bob")},
		},
		sourceMetaErr: map[int64]error{45: errors.New("read boom")},
	})
	s := ptrext.Of(Service{repo: f})

	tx, _ := f.Begin(ctx)
	linked := s.autoLinkCustomersTx(ctx, tx, "tenant-1", requestID, []int64{41, 42, 43, 44, 45, 46}, "actor-1")

	require.Equal(t, 2, linked, "alice once (deduped), bob once; webhook + errors skipped")
	require.Len(t, f.linkedInputs, 2)
	require.Equal(t, "alice@customer.example", f.linkedInputs[0].SubjectKey)
	require.Equal(t, "Auto-attributed from intercom feedback", f.linkedInputs[0].Note)
	require.Equal(t, "bob@customer.example", f.linkedInputs[1].SubjectKey)
	// Revenue context traveled with the account profile.
	require.Equal(t, int64(120000), f.linkedInputs[0].AccountProfile.RevenueCents)
	require.Equal(t, "USD", f.linkedInputs[0].AccountProfile.RevenueCurrency)
	require.Equal(t, "intercom", f.linkedInputs[0].AccountProfile.CRMProvider)

	// Savepoint accounting: 6 rows attempted, 2 committed, 4 rolled back.
	committed, rolledBack := 0, 0
	for _, sp := range f.tx.savepoints {
		if sp.committed {
			committed++
		}
		if sp.rolledBack && !sp.committed {
			rolledBack++
		}
	}
	require.Equal(t, 2, committed)
	require.Equal(t, 4, rolledBack)
}

// TestAutoLinkCustomersTx_LinkFailureDoesNotBlockLater covers the
// "failed row must not block a later row with the same identity" seen-map
// contract, and the savepoint-begin failure leg.
func TestAutoLinkCustomersTx_LinkFailureDoesNotBlockLater(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	requestID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000302")

	f := ptrext.Of(fakeRequestRepo{
		tx: ptrext.Of(attrTx{}),
		sourceMetaByID: map[int64]struct {
			source string
			meta   map[string]any
		}{
			41: {source: "intercom", meta: intercomMeta("carol@customer.example", "Carol")},
			42: {source: "intercom", meta: intercomMeta("carol@customer.example", "Carol")},
		},
	})
	s := ptrext.Of(Service{repo: f})

	// First row's link fails; the second (same identity) must still link.
	f.linkErrOn = "carol@customer.example"
	tx, _ := f.Begin(ctx)
	linked := s.autoLinkCustomersTx(ctx, tx, "tenant-1", requestID, []int64{41}, "actor-1")
	require.Equal(t, 0, linked)

	f.linkErrOn = ""
	linked = s.autoLinkCustomersTx(ctx, tx, "tenant-1", requestID, []int64{42}, "actor-1")
	require.Equal(t, 1, linked, "identity is only marked seen after a successful insert")

	// Savepoint begin failure: warn-and-continue, no link.
	fBeginErr := ptrext.Of(fakeRequestRepo{tx: ptrext.Of(attrTx{beginErr: errors.New("sp boom")})})
	s2 := ptrext.Of(Service{repo: fBeginErr})
	tx2, _ := fBeginErr.Begin(ctx)
	// The outer tx itself begins savepoints; make those fail.
	require.Equal(t, 0, s2.autoLinkCustomersTx(ctx, tx2, "tenant-1", requestID, []int64{41}, "actor-1"))

	// Savepoint commit failure: the row is not counted as linked.
	fCommitErr := ptrext.Of(fakeRequestRepo{
		tx: ptrext.Of(attrTx{spCommitErr: errors.New("sp commit boom")}),
		sourceMetaByID: map[int64]struct {
			source string
			meta   map[string]any
		}{
			41: {source: "intercom", meta: intercomMeta("dave@customer.example", "Dave")},
		},
	})
	s3 := ptrext.Of(Service{repo: fCommitErr})
	tx3, _ := fCommitErr.Begin(ctx)
	require.Equal(t, 0, s3.autoLinkCustomersTx(ctx, tx3, "tenant-1", requestID, []int64{41}, "actor-1"))
}

// TestPromoteInTransaction_AutoAttributionAudit covers the promote
// transaction body incl. the auto_linked_customers audit field.
func TestPromoteInTransaction_AutoAttributionAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := ptrext.Of(fakeRequestRepo{
		tx: ptrext.Of(attrTx{}),
		sourceMetaByID: map[int64]struct {
			source string
			meta   map[string]any
		}{
			42: {source: "intercom", meta: intercomMeta("alice@customer.example", "Alice")},
		},
	})
	s := ptrext.Of(Service{repo: f}) // nil audit → recordAuditTx no-ops

	detail, err := s.promoteInTransaction(ctx, PromoteInput{
		TenantID: "tenant-1", Title: "CSV pagination", FeedbackIDs: []int64{42},
		Actor: testCustomerRequestActor(),
	})
	require.NoError(t, err)
	require.NotNil(t, detail)
	require.Len(t, f.linkedInputs, 1)
	require.Equal(t, 1, f.tx.commitCalls)

	// Error legs: create failure and feedback-link failure abort promote.
	fCreateErr := ptrext.Of(fakeRequestRepo{tx: ptrext.Of(attrTx{}), createErr: errors.New("create boom")})
	_, err = ptrext.Of(Service{repo: fCreateErr}).promoteInTransaction(ctx, PromoteInput{
		TenantID: "tenant-1", Title: "t", FeedbackIDs: []int64{1}, Actor: testCustomerRequestActor(),
	})
	require.Error(t, err)

	fLinkErr := ptrext.Of(fakeRequestRepo{tx: ptrext.Of(attrTx{}), linkFbErr: repo.ErrFeedbackNotFound})
	_, err = ptrext.Of(Service{repo: fLinkErr}).promoteInTransaction(ctx, PromoteInput{
		TenantID: "tenant-1", Title: "t", FeedbackIDs: []int64{1}, Actor: testCustomerRequestActor(),
	})
	require.ErrorIs(t, err, repo.ErrFeedbackNotFound)
}
