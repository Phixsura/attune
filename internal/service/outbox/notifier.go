// Package outbox holds the outbox-side business logic: at-least-once
// webhook delivery (OutboxWorker), in-process fan-out (MultiNotifier),
// and the weekly digest job.
//
// The Notifier interface itself lives in internal/notify (the root of
// the outbound subsystem) — both consumer (service/enrich) and providers
// (this MultiNotifier, plus notify/adapter/*) refer to the same name.
package outbox

import (
	"context"
	"errors"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// MultiNotifier fans one Push call out to every wired member. Per-
// member errors are collected via errors.Join but a single failure
// never blocks the others — webhook outages must not cascade.
//
// Today production wiring builds a single LarkWebhook (raw-webhook delivers via outbox, not inline) — MultiNotifier is reserved for future multi-channel fanout. A follow-up adds
// Slack / Discord adapters using the same Notifier shape.
type MultiNotifier struct {
	members []notify.Notifier
}

// NewMultiNotifier returns a notifier that fans out to every supplied
// member in order. Nil entries are silently skipped so callers can pass
// a webhook that is only conditionally configured.
func NewMultiNotifier(members ...notify.Notifier) *MultiNotifier {
	live := make([]notify.Notifier, 0, len(members))
	for _, m := range members {
		if m != nil {
			live = append(live, m)
		}
	}
	return ptrext.Of(MultiNotifier{members: live})
}

// PushPool fans out PushPool to every member. Returns errors.Join of
// per-member failures (nil on full success). Members are called in
// order; one failing does NOT short-circuit the rest.
func (m *MultiNotifier) PushPool(ctx context.Context, s domain.Snapshot) error {
	return m.fanOut(func(n notify.Notifier) error { return n.PushPool(ctx, s) })
}

// PushRadar fans out PushRadar to every member.
func (m *MultiNotifier) PushRadar(ctx context.Context, s domain.Snapshot) error {
	return m.fanOut(func(n notify.Notifier) error { return n.PushRadar(ctx, s) })
}

func (m *MultiNotifier) fanOut(push func(notify.Notifier) error) error {
	if len(m.members) == 0 {
		return nil
	}
	errs := make([]error, 0, len(m.members))
	for _, member := range m.members {
		if err := push(member); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
