package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrChecksumDrift_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrChecksumDrift
		contains []string
	}{
		{
			name: "single drift",
			err: ErrChecksumDrift{
				Drifted: []ChecksumDrift{
					{Version: 5, Filename: "005_add_col.sql", Stored: "abc123...", Computed: "def456..."},
				},
			},
			contains: []string{
				"migration checksum drift detected",
				"005",
				"005_add_col.sql",
				"stored:",
				"abc123...",
				"computed:",
				"def456...",
				"Possible causes:",
				"Recovery options:",
			},
		},
		{
			name: "multiple drifts",
			err: ErrChecksumDrift{
				Drifted: []ChecksumDrift{
					{Version: 1, Filename: "001_init.sql", Stored: "aaa...", Computed: "bbb..."},
					{Version: 3, Filename: "003_users.sql", Stored: "ccc...", Computed: "ddd..."},
				},
			},
			contains: []string{
				"001",
				"001_init.sql",
				"003",
				"003_users.sql",
				"aaa...",
				"bbb...",
				"ccc...",
				"ddd...",
			},
		},
		{
			name: "empty drifted list",
			err:  ErrChecksumDrift{Drifted: []ChecksumDrift{}},
			contains: []string{
				"migration checksum drift detected",
				"Possible causes:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if msg == "" {
				t.Fatal("error message should not be empty")
			}
			for _, substr := range tc.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message missing %q\ngot: %s", substr, msg)
				}
			}
		})
	}
}

func TestErrMissingFile_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrMissingFile
		contains []string
	}{
		{
			name: "standard missing file",
			err:  ErrMissingFile{Version: 10, Filename: "010_drop_legacy.sql"},
			contains: []string{
				"migration 010",
				"010_drop_legacy.sql",
				"was applied but file is missing",
				"Possible causes:",
				"deleted from source",
				"Recovery:",
			},
		},
		{
			name: "version 1",
			err:  ErrMissingFile{Version: 1, Filename: "001_init.sql"},
			contains: []string{
				"migration 001",
				"001_init.sql",
			},
		},
		{
			name: "large version number",
			err:  ErrMissingFile{Version: 150, Filename: "150_big.sql"},
			contains: []string{
				"migration 150",
				"150_big.sql",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if msg == "" {
				t.Fatal("error message should not be empty")
			}
			for _, substr := range tc.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message missing %q\ngot: %s", substr, msg)
				}
			}
		})
	}
}

func TestErrManifestReorder_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      ErrManifestReorder
		contains []string
	}{
		{
			name: "standard reorder",
			err: ErrManifestReorder{
				Stored:   "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				Computed: "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
			},
			contains: []string{
				"migration reordering detected",
				"stored manifest:",
				"abcdef123456...",
				"computed manifest:",
				"fedcba098765...",
				"This happens when migrations are reordered",
				"Recovery options:",
			},
		},
		{
			name: "short hashes",
			err: ErrManifestReorder{
				Stored:   "short",
				Computed: "tiny",
			},
			contains: []string{
				"stored manifest:",
				"short",
				"computed manifest:",
				"tiny",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if msg == "" {
				t.Fatal("error message should not be empty")
			}
			for _, substr := range tc.contains {
				if !strings.Contains(msg, substr) {
					t.Errorf("error message missing %q\ngot: %s", substr, msg)
				}
			}
		})
	}
}

func TestChecksumShort_EdgeCases(t *testing.T) {
	// Additional edge cases not covered in checksum_test.go
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single char",
			input:    "a",
			expected: "a",
		},
		{
			name:     "11 chars (boundary -1)",
			input:    "12345678901",
			expected: "12345678901",
		},
		{
			name:     "unicode chars count correctly",
			input:    "abcdefghijkl",
			expected: "abcdefghijkl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ChecksumShort(tc.input)
			if got != tc.expected {
				t.Errorf("ChecksumShort(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestChecksumDrift_Fields(t *testing.T) {
	// Verify struct fields are accessible and properly typed
	d := ChecksumDrift{
		Version:  42,
		Filename: "042_test.sql",
		Stored:   "abc123...",
		Computed: "def456...",
	}
	if d.Version != 42 {
		t.Errorf("Version = %d, want 42", d.Version)
	}
	if d.Filename != "042_test.sql" {
		t.Errorf("Filename = %s, want 042_test.sql", d.Filename)
	}
	if d.Stored != "abc123..." {
		t.Errorf("Stored = %s, want abc123...", d.Stored)
	}
	if d.Computed != "def456..." {
		t.Errorf("Computed = %s, want def456...", d.Computed)
	}
}

func TestChecksumStatus_Fields(t *testing.T) {
	t.Parallel()

	status := ChecksumStatus{
		Verified: 70,
		Total:    71,
		Drifted: []ChecksumDrift{
			{Version: 50, Filename: "050_test.sql", Stored: "aaa", Computed: "bbb"},
		},
	}

	require.Equal(t, 70, status.Verified)
	require.Equal(t, 71, status.Total)
	require.Len(t, status.Drifted, 1)
}

func TestErrMissingFile_Fields(t *testing.T) {
	t.Parallel()

	err := ErrMissingFile{
		Version:  42,
		Filename: "042_test.sql",
	}

	require.Equal(t, 42, err.Version)
	require.Equal(t, "042_test.sql", err.Filename)
}

func TestErrManifestReorder_Fields(t *testing.T) {
	t.Parallel()

	err := ErrManifestReorder{
		Stored:   "abc123",
		Computed: "def456",
	}

	require.Equal(t, "abc123", err.Stored)
	require.Equal(t, "def456", err.Computed)
}

func TestErrChecksumDrift_SingleDrift(t *testing.T) {
	t.Parallel()

	err := ErrChecksumDrift{
		Drifted: []ChecksumDrift{
			{Version: 1, Filename: "001_init.sql", Stored: "aaa", Computed: "bbb"},
		},
	}

	msg := err.Error()
	require.Contains(t, msg, "001")
	require.Contains(t, msg, "001_init.sql")
	require.Contains(t, msg, "stored:")
	require.Contains(t, msg, "computed:")
	require.Contains(t, msg, "Possible causes")
}

func TestErrChecksumDrift_ManyDrifts(t *testing.T) {
	t.Parallel()

	drifted := make([]ChecksumDrift, 5)
	for i := 0; i < 5; i++ {
		drifted[i] = ChecksumDrift{
			Version:  i + 1,
			Filename: strings.Repeat("x", 10) + ".sql",
			Stored:   "stored",
			Computed: "computed",
		}
	}

	err := ErrChecksumDrift{Drifted: drifted}
	msg := err.Error()

	// Should contain all versions
	for i := 1; i <= 5; i++ {
		require.Contains(t, msg, "stored")
	}
}

func TestErrMissingFile_ErrorFormat(t *testing.T) {
	t.Parallel()

	err := ErrMissingFile{Version: 99, Filename: "099_missing.sql"}
	msg := err.Error()

	require.Contains(t, msg, "migration 099")
	require.Contains(t, msg, "099_missing.sql")
	require.Contains(t, msg, "was applied but file is missing")
}

func TestErrManifestReorder_TruncatesLongHashes(t *testing.T) {
	t.Parallel()

	longHash := strings.Repeat("a", 64)
	err := ErrManifestReorder{
		Stored:   longHash,
		Computed: longHash,
	}

	msg := err.Error()
	// Should use ChecksumShort to truncate
	require.Contains(t, msg, "aaaaaaaaaaaa...")
}
