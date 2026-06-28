package attune

import "testing"

func TestFilenameFromHeader_PrefersRFC5987FilenameStar(t *testing.T) {
	t.Parallel()

	got := filenameFromHeader(`attachment; filename="fallback.csv"; filename*=UTF-8'en'audit-%E2%82%AC.csv`)
	if got != "audit-€.csv" {
		t.Fatalf("filenameFromHeader = %q, want %q", got, "audit-€.csv")
	}
}

func TestFilenameFromHeader_FallsBackOnBadEscape(t *testing.T) {
	t.Parallel()

	got := filenameFromHeader(`attachment; filename*=UTF-8''bad-%ZZ.csv`)
	if got != "bad-%ZZ.csv" {
		t.Fatalf("filenameFromHeader = %q, want %q", got, "bad-%ZZ.csv")
	}
}

func TestFilenameFromHeader_UsesPlainFilenameWhenFilenameStarIsMalformed(t *testing.T) {
	t.Parallel()

	got := filenameFromHeader(`attachment; filename="fallback.csv"; filename*=UTF-8''bad-%ZZ.csv`)
	if got != "fallback.csv" {
		t.Fatalf("filenameFromHeader = %q, want %q", got, "fallback.csv")
	}
}
