// SPDX-License-Identifier: Apache-2.0

package logexttest

import (
	"log/slog"
	"strings"
	"testing"
)

func TestCaptureTextCapturesAndRestoresDefaultLogger(t *testing.T) {
	before := slog.Default()
	out := CaptureText(t, func() {
		slog.Info("hello", "tenant_id", "tenant-1")
	})
	if !strings.Contains(out, "hello") || !strings.Contains(out, "tenant_id=tenant-1") {
		t.Fatalf("CaptureText() output = %q", out)
	}
	if slog.Default() != before {
		t.Fatal("CaptureText() did not restore slog default")
	}
}
