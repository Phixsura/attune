// SPDX-License-Identifier: Apache-2.0

// Package logexttest provides test helpers for code that needs to assert
// output written through the logext facade.
package logexttest

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// CaptureText runs fn with slog's default logger redirected to a text buffer
// and returns the captured output.
func CaptureText(t testing.TB, fn func()) string {
	t.Helper()
	buf := ptrext.Of(bytes.Buffer{})
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}
