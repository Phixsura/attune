// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBindManagedGitHubIssueLinkTxRebindsLocalTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := ptrext.Of(Repo{})
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	oldRequestID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	connectionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	mappingID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	externalLinkID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{{rows: []fakeRow{{values: []any{
			mappingID,
			[]byte(`{"owner":"Phixsura","repo":"attune"}`),
			"",
		}}}}},
		rows: []fakeRow{
			{values: []any{externalLinkID, oldRequestID.String(), true}},
			{err: pgx.ErrNoRows},
			{values: []any{connectionID, mappingID, "212"}},
		},
	})

	binding, err := repository.BindManagedGitHubIssueLinkTx(ctx, tx, ManagedGitHubIssueLinkInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		Provider:    "github",
		ExternalKey: "Phixsura/attune#212",
		ExternalURL: "https://github.com/Phixsura/attune/issues/212",
	})

	require.NoError(t, err)
	require.NotNil(t, binding)
	require.Equal(t, externalLinkID, binding.ExternalObjectLinkID)
	require.Equal(t, connectionID, binding.ConnectionID)
	require.Equal(t, mappingID, binding.MappingID)
	require.Equal(t, "212", binding.ExternalKey)
	require.Equal(t, 1, tx.queryIdx)
	require.Equal(t, 3, tx.rowIdx)
	require.Equal(t, 1, tx.execIdx)
}

func TestBindManagedGitHubIssueLinkTxRejectsDifferentActiveLocalLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := ptrext.Of(Repo{})
	requestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	mappingID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	externalLinkID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	tx := ptrext.Of(fakeTx{
		queryRows: []fakeRows{{rows: []fakeRow{{values: []any{
			mappingID,
			[]byte(`{"owner":"Phixsura","repo":"attune"}`),
			"",
		}}}}},
		rows: []fakeRow{
			{values: []any{externalLinkID, requestID.String(), true}},
			{values: []any{"213"}},
		},
	})

	binding, err := repository.BindManagedGitHubIssueLinkTx(ctx, tx, ManagedGitHubIssueLinkInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		Provider:    "github",
		ExternalKey: "Phixsura/attune#212",
		ExternalURL: "https://github.com/Phixsura/attune/issues/212",
	})

	require.ErrorIs(t, err, ErrConflict)
	require.Nil(t, binding)
	require.Equal(t, 1, tx.queryIdx)
	require.Equal(t, 2, tx.rowIdx)
	require.Equal(t, 0, tx.execIdx)
}

func TestGitHubRepoTargetFromConfigDerivesBrowserURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cfg               githubExternalConnectionConfig
		connectionBaseURL string
		wantURL           string
	}{
		{
			name:    "repo url",
			cfg:     githubExternalConnectionConfig{RepoURL: "https://github.com/acme/app.git"},
			wantURL: "https://github.com/acme/app/issues/42",
		},
		{
			name:    "api github default",
			cfg:     githubExternalConnectionConfig{Owner: "acme", Repo: "app", APIBaseURL: "https://api.github.com"},
			wantURL: "https://github.com/acme/app/issues/42",
		},
		{
			name:              "enterprise connection base",
			cfg:               githubExternalConnectionConfig{Owner: "acme", Repo: "app"},
			connectionBaseURL: "https://github.example.com/api/v3",
			wantURL:           "https://github.example.com/acme/app/issues/42",
		},
		{
			name:    "enterprise provider base",
			cfg:     githubExternalConnectionConfig{Owner: "acme", Repo: "app", APIBaseURL: "https://github.example.com/api/v3"},
			wantURL: "https://github.example.com/acme/app/issues/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target, ok := githubRepoTargetFromConfig(tt.cfg, tt.connectionBaseURL)
			require.True(t, ok)
			got, err := target.issueURL("42")
			require.NoError(t, err)
			require.Equal(t, tt.wantURL, got)
		})
	}
}

func TestGitHubRepoTargetRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cfg               githubExternalConnectionConfig
		connectionBaseURL string
	}{
		{
			name: "repo url with no path",
			cfg:  githubExternalConnectionConfig{RepoURL: "https://github.com"},
		},
		{
			name: "bad repo url",
			cfg:  githubExternalConnectionConfig{RepoURL: "https://%zz"},
		},
		{
			name: "missing owner",
			cfg:  githubExternalConnectionConfig{Repo: "app", APIBaseURL: "https://api.github.com"},
		},
		{
			name: "missing repo",
			cfg:  githubExternalConnectionConfig{Owner: "acme", APIBaseURL: "https://api.github.com"},
		},
		{
			name:              "bad connection base",
			cfg:               githubExternalConnectionConfig{Owner: "acme", Repo: "app"},
			connectionBaseURL: "://not-a-url",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := githubRepoTargetFromConfig(tt.cfg, tt.connectionBaseURL)
			require.False(t, ok)
		})
	}

	if _, ok := githubRepoTargetFromRaw([]byte(`not-json`), ""); ok {
		t.Fatal("githubRepoTargetFromRaw(invalid JSON) ok = true, want false")
	}
	if _, _, ok := githubBrowserBaseFromAPIBase("", "://bad"); ok {
		t.Fatal("githubBrowserBaseFromAPIBase(invalid URL) ok = true, want false")
	}
}

func TestGitHubConnectionMatchesIssueRefEnterpriseHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		raw               []byte
		connectionBaseURL string
		ref               githubIssueRef
		want              bool
	}{
		{
			name: "provider api base matches enterprise browser host",
			raw:  []byte(`{"owner":"acme","repo":"app","api_base_url":"https://github.example.com/api/v3"}`),
			ref: githubIssueRef{
				host:        "github.example.com",
				owner:       "acme",
				repo:        "app",
				issueNumber: "42",
			},
			want: true,
		},
		{
			name: "enterprise repo rejects same repository on public github",
			raw:  []byte(`{"owner":"acme","repo":"app","api_base_url":"https://github.example.com/api/v3"}`),
			ref: githubIssueRef{
				host:        "github.com",
				owner:       "acme",
				repo:        "app",
				issueNumber: "42",
			},
			want: false,
		},
		{
			name:              "connection base host with port matches browser hostname",
			raw:               []byte(`{"owner":"acme","repo":"app"}`),
			connectionBaseURL: "https://github.enterprise.test:8443/api/v3",
			ref: githubIssueRef{
				host:        "github.enterprise.test",
				owner:       "acme",
				repo:        "app",
				issueNumber: "42",
			},
			want: true,
		},
		{
			name: "repo url host matches",
			raw:  []byte(`{"repo_url":"https://github.example.com/acme/app.git"}`),
			ref: githubIssueRef{
				host:        "github.example.com",
				owner:       "acme",
				repo:        "app",
				issueNumber: "42",
			},
			want: true,
		},
		{
			name: "owner mismatch rejects",
			raw:  []byte(`{"repo_url":"https://github.example.com/acme/app.git"}`),
			ref: githubIssueRef{
				host:        "github.example.com",
				owner:       "other",
				repo:        "app",
				issueNumber: "42",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := githubConnectionMatchesIssueRef(tt.raw, tt.connectionBaseURL, tt.ref)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGitHubIssueReferenceParsers(t *testing.T) {
	t.Parallel()

	ref, ok := parseGitHubIssueKey(" Phixsura/attune.git#00228 ")
	require.True(t, ok)
	require.Equal(t, githubIssueRef{owner: "Phixsura", repo: "attune", issueNumber: "228"}, ref)

	ref, ok = parseGitHubIssueRef(" github ", "", "Phixsura/attune#228")
	require.True(t, ok)
	require.Equal(t, "228", ref.issueNumber)

	ref, ok = parseGitHubIssueRef("github", "https://github.com/Phixsura/attune/issues/00229", "Phixsura/attune#228")
	require.True(t, ok)
	require.Equal(t, githubIssueRef{host: "github.com", owner: "Phixsura", repo: "attune", issueNumber: "229"}, ref)

	if ref, ok := parseGitHubIssueRef("jira", "https://github.com/Phixsura/attune/issues/228", "Phixsura/attune#228"); ok {
		t.Fatalf("parseGitHubIssueRef(non-github) = %+v, true; want false", ref)
	}
	for _, raw := range []string{
		"",
		"https://github.com/Phixsura/attune/pull/228",
		"https://github.com/Phixsura/attune/issues/not-a-number",
		"https://github.com/Phixsura/issues/228",
	} {
		if ref, ok := parseGitHubIssueURL(raw); ok {
			t.Fatalf("parseGitHubIssueURL(%q) = %+v, true; want false", raw, ref)
		}
	}
	for _, raw := range []string{"", "Phixsura/attune", "Phixsura/attune#", "Phixsura/attune#0", "too/many/parts#1"} {
		if ref, ok := parseGitHubIssueKey(raw); ok {
			t.Fatalf("parseGitHubIssueKey(%q) = %+v, true; want false", raw, ref)
		}
	}

	got, ok := normalizeGitHubIssueNumber(" 00228 ")
	require.True(t, ok)
	require.Equal(t, "228", got)
	for _, raw := range []string{"", "0", "-1", "not-a-number"} {
		if got, ok := normalizeGitHubIssueNumber(raw); ok {
			t.Fatalf("normalizeGitHubIssueNumber(%q) = %q, true; want false", raw, got)
		}
	}
}

func TestResolveGitHubIssueLinkTargetValidationAndQueryError(t *testing.T) {
	t.Parallel()

	repository := newCanceledPoolRepo(t)
	connectionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	if target, err := repository.ResolveGitHubIssueLinkTarget(context.Background(), GitHubIssueLinkTargetInput{
		TenantID:     "tenant-a",
		ConnectionID: connectionID,
		IssueNumber:  "0",
	}); !errors.Is(err, ErrInvalidInput) || target != nil {
		t.Fatalf("target=%+v err=%v, want invalid input", target, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target, err := repository.ResolveGitHubIssueLinkTarget(ctx, GitHubIssueLinkTargetInput{
		TenantID:     "tenant-a",
		ConnectionID: connectionID,
		IssueNumber:  "228",
	})
	if err == nil || target != nil {
		t.Fatalf("target=%+v err=%v, want query error", target, err)
	}
}

func TestFindMatchingGitHubIssueMappingBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mappingID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	otherMappingID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ref := githubIssueRef{
		host:        "github.com",
		owner:       "Phixsura",
		repo:        "attune",
		issueNumber: "228",
	}

	t.Run("returns the only matching mapping", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{queryRows: []fakeRows{{rows: []fakeRow{
			{values: []any{otherMappingID, []byte(`{"owner":"other","repo":"attune"}`), ""}},
			{values: []any{mappingID, []byte(`{"owner":"Phixsura","repo":"attune"}`), ""}},
		}}}})
		got, ok, err := findMatchingGitHubIssueMapping(ctx, tx, "tenant-a", ref, ptrext.Of(mappingID))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, mappingID, got)
		require.Equal(t, 1, tx.queryIdx)
	})

	t.Run("returns no match when no connection targets the issue repo", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{queryRows: []fakeRows{{rows: []fakeRow{{
			values: []any{otherMappingID, []byte(`{"owner":"other","repo":"attune"}`), ""},
		}}}}})
		got, ok, err := findMatchingGitHubIssueMapping(ctx, tx, "tenant-a", ref, nil)
		require.NoError(t, err)
		require.False(t, ok)
		require.Equal(t, uuid.Nil, got)
	})

	t.Run("rejects ambiguous matching mappings", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{queryRows: []fakeRows{{rows: []fakeRow{
			{values: []any{mappingID, []byte(`{"owner":"Phixsura","repo":"attune"}`), ""}},
			{values: []any{otherMappingID, []byte(`{"repo_url":"https://github.com/Phixsura/attune"}`), ""}},
		}}}})
		got, ok, err := findMatchingGitHubIssueMapping(ctx, tx, "tenant-a", ref, nil)
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, ok)
		require.Equal(t, uuid.Nil, got)
	})

	t.Run("wraps query and rows errors", func(t *testing.T) {
		t.Parallel()

		errBoom := errors.New("boom")
		got, ok, err := findMatchingGitHubIssueMapping(ctx, ptrext.Of(fakeTx{
			queryErrs: []error{errBoom},
		}), "tenant-a", ref, nil)
		require.ErrorContains(t, err, "find github issue sync mapping")
		require.False(t, ok)
		require.Equal(t, uuid.Nil, got)

		got, ok, err = findMatchingGitHubIssueMapping(ctx, ptrext.Of(fakeTx{
			queryRows: []fakeRows{{err: errBoom}},
		}), "tenant-a", ref, nil)
		require.ErrorContains(t, err, "find github issue sync mapping rows")
		require.False(t, ok)
		require.Equal(t, uuid.Nil, got)
	})
}

func TestEnsureGitHubExternalIssueLinkBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	otherRequestID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	mappingID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	linkID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	input := ManagedGitHubIssueLinkInput{
		TenantID:    "tenant-a",
		RequestID:   requestID,
		ExternalURL: "https://github.com/Phixsura/attune/issues/228",
	}
	ref := githubIssueRef{owner: "Phixsura", repo: "attune", issueNumber: "228"}

	t.Run("refreshes an existing active link for the same request", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{{values: []any{linkID, requestID.String(), false}}}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, linkID, gotID)
		require.Equal(t, 1, tx.rowIdx)
		require.Equal(t, 1, tx.execIdx)
	})

	t.Run("rejects an active link owned by another request", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{{values: []any{linkID, otherRequestID.String(), false}}}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, ok)
		require.Equal(t, uuid.Nil, gotID)
		require.Equal(t, 0, tx.execIdx)
	})

	t.Run("refreshes a local tombstone when no active link conflicts", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{linkID, requestID.String(), true}},
			{err: pgx.ErrNoRows},
		}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, linkID, gotID)
		require.Equal(t, 2, tx.rowIdx)
		require.Equal(t, 1, tx.execIdx)
	})

	t.Run("rejects a local tombstone when another active external key exists", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{values: []any{linkID, requestID.String(), true}},
			{values: []any{"229"}},
		}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, ok)
		require.Equal(t, uuid.Nil, gotID)
	})

	t.Run("refreshes an existing local link with the same issue key", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{err: pgx.ErrNoRows},
			{values: []any{linkID, "228"}},
		}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, linkID, gotID)
		require.Equal(t, 2, tx.rowIdx)
		require.Equal(t, 1, tx.execIdx)
	})

	t.Run("rejects an existing local link with a different issue key", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{err: pgx.ErrNoRows},
			{values: []any{linkID, "229"}},
		}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.ErrorIs(t, err, ErrConflict)
		require.False(t, ok)
		require.Equal(t, uuid.Nil, gotID)
	})

	t.Run("inserts a new external issue link when no existing link matches", func(t *testing.T) {
		t.Parallel()

		tx := ptrext.Of(fakeTx{rows: []fakeRow{
			{err: pgx.ErrNoRows},
			{err: pgx.ErrNoRows},
			{values: []any{linkID}},
		}})
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, tx, input, mappingID, ref)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, linkID, gotID)
		require.Equal(t, 3, tx.rowIdx)
	})

	t.Run("wraps lookup errors", func(t *testing.T) {
		t.Parallel()

		errBoom := errors.New("boom")
		gotID, ok, err := ensureGitHubExternalIssueLink(ctx, ptrext.Of(fakeTx{
			rows: []fakeRow{{err: errBoom}},
		}), input, mappingID, ref)
		require.ErrorContains(t, err, "find existing github external issue link")
		require.False(t, ok)
		require.Equal(t, uuid.Nil, gotID)
	})
}

func TestTruncateGitHubIssueLinkText(t *testing.T) {
	t.Parallel()

	require.Equal(t, "", truncateGitHubIssueLinkText("value", 0))
	require.Equal(t, "short", truncateGitHubIssueLinkText("short", 10))
	require.Equal(t, "abc", truncateGitHubIssueLinkText("abcdef", 3))
	require.Equal(t, "世界", truncateGitHubIssueLinkText("世界hello", 2))
}

func TestTombstoneLocalIssueExternalLinkTx(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := ptrext.Of(Repo{})
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	externalObjectLinkID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tx := ptrext.Of(fakeTx{})
	err := repository.TombstoneLocalIssueExternalLinkTx(ctx, tx, "tenant-a", requestID, externalObjectLinkID)
	require.NoError(t, err)
	require.Equal(t, 1, tx.execIdx)
	require.Equal(t, []any{"tenant-a", requestID, externalObjectLinkID}, tx.execArgs[0])

	errBoom := errors.New("boom")
	err = repository.TombstoneLocalIssueExternalLinkTx(ctx, ptrext.Of(fakeTx{
		execErrs: []error{errBoom},
	}), "tenant-a", requestID, externalObjectLinkID)
	require.ErrorContains(t, err, "tombstone local external issue link")
}

func TestManagedIssueSyncTargetTxBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	externalObjectLinkID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	connectionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	mappingID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	target, err := managedIssueSyncTargetTx(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{
		values: []any{connectionID, mappingID, "Phixsura/attune#228"},
	}}}), "tenant-a", requestID, externalObjectLinkID)
	require.NoError(t, err)
	require.NotNil(t, target)
	require.Equal(t, connectionID, target.ConnectionID)
	require.Equal(t, mappingID, target.MappingID)
	require.Equal(t, "Phixsura/attune#228", target.ExternalKey)

	target, err = managedIssueSyncTargetTx(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{
		err: pgx.ErrNoRows,
	}}}), "tenant-a", requestID, externalObjectLinkID)
	require.NoError(t, err)
	require.Nil(t, target)

	errBoom := errors.New("boom")
	target, err = managedIssueSyncTargetTx(ctx, ptrext.Of(fakeTx{rows: []fakeRow{{
		err: errBoom,
	}}}), "tenant-a", requestID, externalObjectLinkID)
	require.Nil(t, target)
	require.ErrorContains(t, err, "load managed issue sync target")
}

func TestMapGitHubIssueLinkWriteError(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, mapGitHubIssueLinkWriteError(ptrext.Of(pgconn.PgError{
		Code: "23514",
	}), "insert"), ErrInvalidInput)
	require.ErrorIs(t, mapGitHubIssueLinkWriteError(ptrext.Of(pgconn.PgError{
		Code: "23505",
	}), "insert"), ErrConflict)
	require.ErrorIs(t, mapGitHubIssueLinkWriteError(ptrext.Of(pgconn.PgError{
		Code: "23503",
	}), "insert"), ErrLocalObjectNotFound)
	require.ErrorContains(t, mapGitHubIssueLinkWriteError(errors.New("boom"), "insert"), "insert")
}
