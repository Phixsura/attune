// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"time"
)

const (
	// defaultLoopInterval is the fallback when no source row is enabled
	// or no source has reported a poll interval yet. We don't tight-loop
	// on an empty registry.
	defaultLoopInterval = 60 * time.Second

	// minLoopInterval is the floor — a misconfigured source asking to
	// poll every second cannot drag attune into a hot loop against the
	// IMAP server (the per-source 30s budget also bounds this).
	minLoopInterval = 10 * time.Second
)

// pollLoop iterates enabled email sources, polling each within its own
// 30-second budget. The list-then-poll pattern means newly-enabled
// sources are picked up on the next tick without restarting the loop.
func (a *adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()

	for {
		sources, err := a.deps.Sources.List(ctx, channelName)
		if err != nil {
			a.deps.Logger.Warnf(ctx, "[inbound.email.pollLoop] list sources failed,err:%+v", err.Error())
		}
		for _, src := range sources {
			if ctx.Err() != nil {
				return
			}
			if !src.Enabled {
				// Defensive — Repo.List is expected to filter on
				// enabled=TRUE at the SQL layer, but a stale read
				// would otherwise tight-loop polling.
				continue
			}
			srcCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			a.pollSource(srcCtx, src)
			cancel()
		}

		interval := a.nextInterval()
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}
	}
}

// nextInterval picks the smallest per-source poll interval (clamped to
// minLoopInterval) and falls back to defaultLoopInterval when no source
// has reported one yet. Each pollSource records its source's interval
// into pollIntervals on its first successful decrypt; later rounds use
// the cached value.
//
// Spec §Email IMAP adapter promised the per-source
// `poll_interval_seconds` knob would take effect; the original loop
// hard-coded 60s, ignoring every source's setting (review M3, #66).
func (a *adapter) nextInterval() time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	best := defaultLoopInterval
	seen := false
	for _, secs := range a.pollIntervals {
		if secs <= 0 {
			continue
		}
		d := time.Duration(secs) * time.Second
		if d < minLoopInterval {
			d = minLoopInterval
		}
		if !seen || d < best {
			best = d
			seen = true
		}
	}
	return best
}

// recordPollInterval stamps the configured interval seconds for a given
// source slug under the adapter mutex. Called from pollSource right
// after a successful config decrypt.
func (a *adapter) recordPollInterval(slug string, secs int) {
	a.mu.Lock()
	if a.pollIntervals == nil {
		a.pollIntervals = map[string]int{}
	}
	a.pollIntervals[slug] = secs
	a.mu.Unlock()
}
