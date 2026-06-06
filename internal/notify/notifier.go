package notify

import (
	"context"

	"github.com/Phixsura/attune/internal/domain"
)

// Notifier is the side-channel the enricher hands a freshly classified
// row to. Lives in the notify root because that's the convergence point
// for outbound delivery — both consumer (service/enrich) and providers
// (service/outbox MultiNotifier, notify/adapter/lark+raw+github) refer
// to the same canonical name. Push* methods MUST NOT block — they are
// invoked from a goroutine but should still respect the supplied
// context for timeouts.
type Notifier interface {
	PushPool(ctx context.Context, s domain.Snapshot) error
	PushRadar(ctx context.Context, s domain.Snapshot) error
}
