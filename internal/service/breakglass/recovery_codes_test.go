// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	breakglassrepo "github.com/Phixsura/attune/internal/repo/breakglass"
)

func TestRecoveryCodeServiceGenerateCodesCreatesHashedCodes(t *testing.T) {
	t.Parallel()

	repo := newFakeRecoveryCodeRepo()
	svc := NewRecoveryCodeService(repo)

	result, err := svc.GenerateCodes(context.Background(), GenerateCodesInput{
		TenantID:  "tenant-1",
		Count:     1,
		CreatedBy: "admin@example.com",
	})
	if err != nil {
		t.Fatalf("GenerateCodes() error = %v, want nil", err)
	}
	if len(result.Codes) != 1 || len(result.RawCodes) != 1 {
		t.Fatalf("GenerateCodes() returned %d codes and %d raw codes, want 1/1", len(result.Codes), len(result.RawCodes))
	}
	raw := result.RawCodes[0]
	if !strings.HasPrefix(raw, RecoveryCodePrefix) {
		t.Fatalf("raw code %q does not use prefix %q", raw, RecoveryCodePrefix)
	}
	if result.Codes[0].CodeHash == raw {
		t.Fatal("stored recovery code hash must not equal the raw code")
	}
	if !verifyRecoveryCode(raw, result.Codes[0].CodeHash) {
		t.Fatal("stored recovery code hash does not verify the returned raw code")
	}
	if repo.created[0].Label != "Code 1" || repo.created[0].TenantID != "tenant-1" {
		t.Fatalf("created payload = %+v, want tenant label Code 1", repo.created[0])
	}
}

func TestRecoveryCodeServiceGenerateCodesReturnsCreateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("insert recovery code")
	svc := NewRecoveryCodeService(newFakeRecoveryCodeRepoWithError(wantErr))

	_, err := svc.GenerateCodes(context.Background(), GenerateCodesInput{
		TenantID:  "tenant-1",
		Count:     1,
		CreatedBy: "admin@example.com",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("GenerateCodes() error = %v, want %v", err, wantErr)
	}
}

func TestRecoveryCodeServiceValidateCodeConsumesMatchingCode(t *testing.T) {
	t.Parallel()

	raw, hash := mustRecoveryCodeHash(t)
	repo := newFakeRecoveryCodeRepo()
	repo.valid = []domain.BreakGlassRecoveryCode{
		{ID: "code-1", TenantID: "tenant-1", CodeHash: hash, Label: "Code 1"},
	}
	svc := NewRecoveryCodeService(repo)

	code, err := svc.ValidateCode(context.Background(), "tenant-1", raw, "203.0.113.10")
	if err != nil {
		t.Fatalf("ValidateCode() error = %v, want nil", err)
	}
	if code.ID != "code-1" {
		t.Fatalf("ValidateCode() ID = %q, want code-1", code.ID)
	}
	if repo.markUsedID != "code-1" || repo.markUsedIP != "203.0.113.10" {
		t.Fatalf("MarkUsed called with id=%q ip=%q", repo.markUsedID, repo.markUsedIP)
	}
}

func TestRecoveryCodeServiceValidateCodeRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	repo := newFakeRecoveryCodeRepo()
	svc := NewRecoveryCodeService(repo)

	if _, err := svc.ValidateCode(context.Background(), "tenant-1", "bad-code", "203.0.113.10"); !errors.Is(err, breakglassrepo.ErrCodeNotFound) {
		t.Fatalf("ValidateCode(bad prefix) error = %v, want ErrCodeNotFound", err)
	}
	if repo.listValidCalled {
		t.Fatal("ListValid should not be called for invalid prefixes")
	}

	raw, hash := mustRecoveryCodeHash(t)
	repo.valid = []domain.BreakGlassRecoveryCode{{ID: "code-1", TenantID: "tenant-1", CodeHash: hash}}
	if _, err := svc.ValidateCode(context.Background(), "tenant-1", raw+"x", "203.0.113.10"); !errors.Is(err, breakglassrepo.ErrCodeNotFound) {
		t.Fatalf("ValidateCode(non-matching code) error = %v, want ErrCodeNotFound", err)
	}
}

func TestRecoveryCodeServiceValidateCodeHandlesRepositoryErrors(t *testing.T) {
	t.Parallel()

	raw, hash := mustRecoveryCodeHash(t)
	listErr := errors.New("list valid")
	repo := newFakeRecoveryCodeRepo()
	repo.listValidErr = listErr
	svc := NewRecoveryCodeService(repo)
	if _, err := svc.ValidateCode(context.Background(), "tenant-1", raw, "203.0.113.10"); !errors.Is(err, listErr) {
		t.Fatalf("ValidateCode(list error) = %v, want %v", err, listErr)
	}

	markErr := errors.New("mark used")
	repo = newFakeRecoveryCodeRepo()
	repo.valid = []domain.BreakGlassRecoveryCode{{ID: "code-1", TenantID: "tenant-1", CodeHash: hash}}
	repo.markUsedErr = markErr
	svc = NewRecoveryCodeService(repo)
	if _, err := svc.ValidateCode(context.Background(), "tenant-1", raw, "203.0.113.10"); !errors.Is(err, markErr) {
		t.Fatalf("ValidateCode(mark error) = %v, want %v", err, markErr)
	}
}

func TestRecoveryCodeServiceValidateCodeContinuesPastAlreadyUsedRace(t *testing.T) {
	t.Parallel()

	raw, hash := mustRecoveryCodeHash(t)
	repo := newFakeRecoveryCodeRepo()
	repo.valid = []domain.BreakGlassRecoveryCode{
		{ID: "raced", TenantID: "tenant-1", CodeHash: hash},
		{ID: "winner", TenantID: "tenant-1", CodeHash: hash},
	}
	repo.markUsedErrs = []error{breakglassrepo.ErrCodeAlreadyUsed, nil}
	svc := NewRecoveryCodeService(repo)

	code, err := svc.ValidateCode(context.Background(), "tenant-1", raw, "203.0.113.10")
	if err != nil {
		t.Fatalf("ValidateCode() error = %v, want nil", err)
	}
	if code.ID != "winner" || repo.markUsedCalls != 2 {
		t.Fatalf("ValidateCode() consumed %q after %d attempts, want winner after 2", code.ID, repo.markUsedCalls)
	}
}

func TestRecoveryCodeServiceDelegatesListCountAndRevoke(t *testing.T) {
	t.Parallel()

	repo := newFakeRecoveryCodeRepo()
	repo.all = []domain.BreakGlassRecoveryCode{{ID: "code-1"}, {ID: "code-2"}}
	repo.countValid = 2
	repo.revokeAllCount = 2
	svc := NewRecoveryCodeService(repo)

	codes, err := svc.List(context.Background(), "tenant-1")
	if err != nil || len(codes) != 2 || repo.listAllLimit != 100 {
		t.Fatalf("List() = %d codes, limit=%d, err=%v; want 2, 100, nil", len(codes), repo.listAllLimit, err)
	}
	count, err := svc.CountValid(context.Background(), "tenant-1")
	if err != nil || count != 2 {
		t.Fatalf("CountValid() = %d, %v; want 2, nil", count, err)
	}
	if err := svc.Revoke(context.Background(), "tenant-1", "code-1", "admin@example.com"); err != nil {
		t.Fatalf("Revoke() error = %v, want nil", err)
	}
	if repo.revokedID != "code-1" || repo.revokedBy != "admin@example.com" {
		t.Fatalf("Revoke() delegated id=%q by=%q", repo.revokedID, repo.revokedBy)
	}
	revoked, err := svc.RevokeAll(context.Background(), "tenant-1", "admin@example.com")
	if err != nil || revoked != 2 {
		t.Fatalf("RevokeAll() = %d, %v; want 2, nil", revoked, err)
	}
}

func TestRecoveryCodeHelpersRoundTrip(t *testing.T) {
	t.Parallel()

	raw, err := generateRecoveryCode()
	if err != nil {
		t.Fatalf("generateRecoveryCode() error = %v", err)
	}
	if !strings.HasPrefix(raw, RecoveryCodePrefix) || len(raw) != len("rc_0000-0000-0000") {
		t.Fatalf("generateRecoveryCode() = %q, want rc_XXXX-XXXX-XXXX", raw)
	}
	hash, err := hashRecoveryCode(raw)
	if err != nil {
		t.Fatalf("hashRecoveryCode() error = %v", err)
	}
	if !verifyRecoveryCode(raw, hash) {
		t.Fatal("verifyRecoveryCode() = false, want true")
	}
	if verifyRecoveryCode(raw+"x", hash) {
		t.Fatal("verifyRecoveryCode(mutated) = true, want false")
	}
}

func mustRecoveryCodeHash(t *testing.T) (string, string) {
	t.Helper()
	raw, err := generateRecoveryCode()
	if err != nil {
		t.Fatalf("generateRecoveryCode() error = %v", err)
	}
	hash, err := hashRecoveryCode(raw)
	if err != nil {
		t.Fatalf("hashRecoveryCode() error = %v", err)
	}
	return raw, hash
}

type fakeRecoveryCodeRepo struct {
	created   []breakglassrepo.NewRecoveryCode
	createErr error

	valid           []domain.BreakGlassRecoveryCode
	listValidErr    error
	listValidCalled bool

	all          []domain.BreakGlassRecoveryCode
	listAllLimit int
	listAllErr   error

	markUsedErr   error
	markUsedErrs  []error
	markUsedCalls int
	markUsedID    string
	markUsedIP    string

	revokedID string
	revokedBy string
	revokeErr error

	revokeAllCount int64
	revokeAllErr   error

	countValid    int
	countValidErr error
}

func newFakeRecoveryCodeRepo() *fakeRecoveryCodeRepo {
	return ptrext.Of(fakeRecoveryCodeRepo{})
}

func newFakeRecoveryCodeRepoWithError(err error) *fakeRecoveryCodeRepo {
	repo := newFakeRecoveryCodeRepo()
	repo.createErr = err
	return repo
}

func (f *fakeRecoveryCodeRepo) Create(_ context.Context, n breakglassrepo.NewRecoveryCode) (domain.BreakGlassRecoveryCode, error) {
	if f.createErr != nil {
		return domain.BreakGlassRecoveryCode{}, f.createErr
	}
	f.created = append(f.created, n)
	return domain.BreakGlassRecoveryCode{
		ID:        n.Label,
		TenantID:  n.TenantID,
		CodeHash:  n.CodeHash,
		Label:     n.Label,
		CreatedBy: n.CreatedBy,
	}, nil
}

func (f *fakeRecoveryCodeRepo) ListValid(_ context.Context, _ string) ([]domain.BreakGlassRecoveryCode, error) {
	f.listValidCalled = true
	return f.valid, f.listValidErr
}

func (f *fakeRecoveryCodeRepo) ListAll(_ context.Context, _ string, limit int) ([]domain.BreakGlassRecoveryCode, error) {
	f.listAllLimit = limit
	return f.all, f.listAllErr
}

func (f *fakeRecoveryCodeRepo) MarkUsed(_ context.Context, _, id, clientIP string) error {
	f.markUsedCalls++
	f.markUsedID = id
	f.markUsedIP = clientIP
	if len(f.markUsedErrs) >= f.markUsedCalls {
		return f.markUsedErrs[f.markUsedCalls-1]
	}
	return f.markUsedErr
}

func (f *fakeRecoveryCodeRepo) Revoke(_ context.Context, _, id, revokedBy string) error {
	f.revokedID = id
	f.revokedBy = revokedBy
	return f.revokeErr
}

func (f *fakeRecoveryCodeRepo) RevokeAll(_ context.Context, _, revokedBy string) (int64, error) {
	f.revokedBy = revokedBy
	return f.revokeAllCount, f.revokeAllErr
}

func (f *fakeRecoveryCodeRepo) CountValid(context.Context, string) (int, error) {
	return f.countValid, f.countValidErr
}
