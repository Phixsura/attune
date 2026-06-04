// Package service holds listen's business logic. It sits between
// handlers (HTTP) and repo (DB), depending on domain for types and
// optionally on llmclient + notify for I/O collaborators (律 8).
//
// This file declares the Notifier seam so the enricher can fan out
// without importing notify directly — keeps the dependency arrow
// service → notify one-way and lets tests inject a fake.
package service

import (
	"context"
	"errors"

	"github.com/Phixsura/listen/internal/domain"
)

// Notifier is the side-channel the enricher hands a freshly classified
// row to. Implementations live in internal/notify (Lark group webhook
// + raw HTTPS webhook in Wave 1.2). Push* methods MUST NOT block —
// they are invoked from a goroutine but should still respect the
// supplied context for timeouts.
type Notifier interface {
	PushPool(ctx context.Context, s domain.Snapshot) error
	PushRadar(ctx context.Context, s domain.Snapshot) error
}

// MultiNotifier fans one Push call out to every wired member. Per-
// member errors are collected via errors.Join but a single failure
// never blocks the others — webhook outages must not cascade.
//
// Wave 1.2 members: LarkWebhook + RawWebhookRouter. Wave 3 adds
// Slack / Discord adapters using the same Notifier shape.
type MultiNotifier struct {
	members []Notifier
}

// NewMultiNotifier returns a notifier that fans out to every supplied
// member in order. Nil entries are silently skipped so callers can pass
// a webhook that is only conditionally configured.
func NewMultiNotifier(members ...Notifier) *MultiNotifier {
	live := make([]Notifier, 0, len(members))
	for _, m := range members {
		if m != nil {
			live = append(live, m)
		}
	}
	return &MultiNotifier{members: live}
}

// PushPool fans out PushPool to every member. Returns errors.Join of
// per-member failures (nil on full success). Members are called in
// order; one failing does NOT short-circuit the rest.
func (m *MultiNotifier) PushPool(ctx context.Context, s domain.Snapshot) error {
	return m.fanOut(func(n Notifier) error { return n.PushPool(ctx, s) })
}

// PushRadar fans out PushRadar to every member.
func (m *MultiNotifier) PushRadar(ctx context.Context, s domain.Snapshot) error {
	return m.fanOut(func(n Notifier) error { return n.PushRadar(ctx, s) })
}

func (m *MultiNotifier) fanOut(push func(Notifier) error) error {
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
