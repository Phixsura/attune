// ptrext:file-allow test fixtures use &pgconn.PgError literals
package pgxutil

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("some error"), false},
		{"unique violation", &pgconn.PgError{Code: "23505"}, true},
		{"foreign key violation", &pgconn.PgError{Code: "23503"}, false},
		{"check violation", &pgconn.PgError{Code: "23514"}, false},
		{"other pg error", &pgconn.PgError{Code: "42P01"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsUniqueViolation(tt.err); got != tt.want {
				t.Errorf("IsUniqueViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsForeignKeyViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("some error"), false},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"foreign key violation", &pgconn.PgError{Code: "23503"}, true},
		{"check violation", &pgconn.PgError{Code: "23514"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsForeignKeyViolation(tt.err); got != tt.want {
				t.Errorf("IsForeignKeyViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCheckViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("some error"), false},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"foreign key violation", &pgconn.PgError{Code: "23503"}, false},
		{"check violation", &pgconn.PgError{Code: "23514"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCheckViolation(tt.err); got != tt.want {
				t.Errorf("IsCheckViolation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 10, ""},
		{"shorter than limit", "hello", 10, "hello"},
		{"equal to limit", "hello", 5, "hello"},
		{"longer than limit", "hello world", 5, "hello"},
		{"zero limit", "hello", 0, ""},
		{"unicode string", "你好世界", 6, "你好"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Truncate(tt.s, tt.n); got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestWrappedPgError(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{Code: "23505"}
	wrapped := errors.Join(errors.New("context"), pgErr)

	if !IsUniqueViolation(wrapped) {
		t.Error("IsUniqueViolation should detect wrapped PgError")
	}
}
