package enrich

import (
	"context"

	"github.com/Phixsura/attune/internal/domain"
)

// Notifier is the side-channel the enricher hands a freshly classified
// row to. Defined here on the consumer side (Go idiom: the package that
// USES an abstraction owns its interface, not the package that provides
// the implementation). Implementations live in internal/service/outbox
// (MultiNotifier) and internal/notify (Lark group webhook + raw HTTPS
// webhook). Push* methods MUST NOT block — they are invoked from a
// goroutine but should still respect the supplied context for timeouts.
type Notifier interface {
	PushPool(ctx context.Context, s domain.Snapshot) error
	PushRadar(ctx context.Context, s domain.Snapshot) error
}
