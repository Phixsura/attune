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
