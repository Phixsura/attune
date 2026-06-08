// SPDX-License-Identifier: Apache-2.0

package inbound

import "context"

// Logger — logext facade subset, ctx-first. Adapters call this so they
// never import log/slog directly (CLAUDE.md §7).
type Logger interface {
	Infof(ctx context.Context, format string, args ...any)
	Warnf(ctx context.Context, format string, args ...any)
	Errorf(ctx context.Context, format string, args ...any)
}
