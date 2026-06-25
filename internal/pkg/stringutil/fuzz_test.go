// SPDX-License-Identifier: Apache-2.0

package stringutil

import (
	"testing"
	"unicode/utf8"
)

func FuzzTruncate(f *testing.F) {
	f.Add("hello world", 5, "...")
	f.Add("", 10, "...")
	f.Add("short", 100, "...")
	f.Add("unicode: 你好世界", 10, "...")

	f.Fuzz(func(t *testing.T, s string, maxLen int, suffix string) {
		if maxLen < 0 {
			maxLen = 0
		}
		if maxLen > 10000 {
			maxLen = 10000
		}

		result := Truncate(s, maxLen, suffix)

		// Result should not exceed maxLen
		if len(result) > maxLen && maxLen > 0 {
			t.Errorf("Truncate(%q, %d, %q) = %q, len %d > maxLen", s, maxLen, suffix, result, len(result))
		}

		// Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("Truncate(%q, %d, %q) = %q is not valid UTF-8", s, maxLen, suffix, result)
		}
	})
}

func FuzzToSnakeCase(f *testing.F) {
	f.Add("HelloWorld")
	f.Add("myVariable")
	f.Add("already_snake")
	f.Add("XMLParser")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		result := ToSnakeCase(s)

		// Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("ToSnakeCase(%q) = %q is not valid UTF-8", s, result)
		}

		// Result should not contain uppercase
		for _, r := range result {
			if r >= 'A' && r <= 'Z' {
				t.Errorf("ToSnakeCase(%q) = %q contains uppercase", s, result)
				break
			}
		}
	})
}

func FuzzToCamelCase(f *testing.F) {
	f.Add("hello_world")
	f.Add("my_variable")
	f.Add("already")
	f.Add("")

	f.Fuzz(func(t *testing.T, s string) {
		result := ToCamelCase(s)

		// Result should be valid UTF-8
		if !utf8.ValidString(result) {
			t.Errorf("ToCamelCase(%q) = %q is not valid UTF-8", s, result)
		}

		// Result should not contain underscores
		for _, r := range result {
			if r == '_' {
				t.Errorf("ToCamelCase(%q) = %q contains underscore", s, result)
				break
			}
		}
	})
}
