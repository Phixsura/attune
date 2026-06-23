package database

import "testing"

func TestChecksum(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantLen int
	}{
		{"empty", []byte{}, 64},
		{"hello", []byte("hello world"), 64},
		{"sql", []byte("CREATE TABLE foo (id INT);"), 64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Checksum(tc.input)
			if len(got) != tc.wantLen {
				t.Errorf("Checksum() length = %d, want %d", len(got), tc.wantLen)
			}
			// Verify it's hex (all chars are 0-9a-f)
			for _, c := range got {
				isDigit := c >= '0' && c <= '9'
				isHexLower := c >= 'a' && c <= 'f'
				if !isDigit && !isHexLower {
					t.Errorf("Checksum() contains non-hex char: %c", c)
				}
			}
		})
	}
}

func TestChecksum_Deterministic(t *testing.T) {
	input := []byte("CREATE TABLE users (id SERIAL PRIMARY KEY);")
	c1 := Checksum(input)
	c2 := Checksum(input)
	if c1 != c2 {
		t.Errorf("Checksum not deterministic: %s != %s", c1, c2)
	}
}

func TestChecksum_Sensitive(t *testing.T) {
	// Even a small change should produce a different checksum
	c1 := Checksum([]byte("hello"))
	c2 := Checksum([]byte("hellO")) // capital O
	if c1 == c2 {
		t.Error("Checksum should be sensitive to changes")
	}
}

func TestChecksumShort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc", "abc"},
		{"123456789012", "123456789012"},
		{"1234567890123", "123456789012..."},
		{"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890", "abcdef123456..."},
	}

	for _, tc := range tests {
		t.Run(tc.input[:min(8, len(tc.input))], func(t *testing.T) {
			got := ChecksumShort(tc.input)
			if got != tc.want {
				t.Errorf("ChecksumShort(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestManifestHash(t *testing.T) {
	// ManifestHash uses embedded migration files, so we test basic properties
	names, err := LoadMigrationNames()
	if err != nil {
		t.Fatalf("LoadMigrationNames: %v", err)
	}
	if len(names) == 0 {
		t.Skip("no embedded migrations")
	}

	t.Run("deterministic", func(t *testing.T) {
		h1, err := ManifestHash(names)
		if err != nil {
			t.Fatalf("ManifestHash: %v", err)
		}
		h2, err := ManifestHash(names)
		if err != nil {
			t.Fatalf("ManifestHash: %v", err)
		}
		if h1 != h2 {
			t.Errorf("ManifestHash not deterministic: %s != %s", h1, h2)
		}
	})

	t.Run("hex_format", func(t *testing.T) {
		h, err := ManifestHash(names)
		if err != nil {
			t.Fatalf("ManifestHash: %v", err)
		}
		if len(h) != 64 {
			t.Errorf("ManifestHash length = %d, want 64", len(h))
		}
		for _, c := range h {
			isDigit := c >= '0' && c <= '9'
			isHexLower := c >= 'a' && c <= 'f'
			if !isDigit && !isHexLower {
				t.Errorf("ManifestHash contains non-hex char: %c", c)
			}
		}
	})

	t.Run("order_sensitive", func(t *testing.T) {
		if len(names) < 2 {
			t.Skip("need at least 2 migrations to test order sensitivity")
		}
		h1, err := ManifestHash(names)
		if err != nil {
			t.Fatalf("ManifestHash: %v", err)
		}
		// Reverse the order
		reversed := make([]string, len(names))
		for i, name := range names {
			reversed[len(names)-1-i] = name
		}
		h2, err := ManifestHash(reversed)
		if err != nil {
			t.Fatalf("ManifestHash reversed: %v", err)
		}
		if h1 == h2 {
			t.Error("ManifestHash should be order-sensitive")
		}
	})

	t.Run("subset_differs", func(t *testing.T) {
		if len(names) < 2 {
			t.Skip("need at least 2 migrations to test subset difference")
		}
		h1, err := ManifestHash(names)
		if err != nil {
			t.Fatalf("ManifestHash: %v", err)
		}
		// Use only first half
		h2, err := ManifestHash(names[:len(names)/2])
		if err != nil {
			t.Fatalf("ManifestHash subset: %v", err)
		}
		if h1 == h2 {
			t.Error("ManifestHash should differ for subsets")
		}
	})
}

func TestManifestHash_EmptyList(t *testing.T) {
	h, err := ManifestHash([]string{})
	if err != nil {
		t.Fatalf("ManifestHash empty: %v", err)
	}
	// SHA-256 of empty input
	if len(h) != 64 {
		t.Errorf("ManifestHash empty length = %d, want 64", len(h))
	}
}

func TestManifestHash_MissingFile(t *testing.T) {
	_, err := ManifestHash([]string{"nonexistent_migration.sql"})
	if err == nil {
		t.Error("ManifestHash should error on missing file")
	}
}
