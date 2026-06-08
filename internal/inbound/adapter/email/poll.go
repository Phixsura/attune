// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"time"
)

// defaultLoopInterval — fallback when no source row is enabled. We
// don't tight-loop on an empty registry.
const defaultLoopInterval = 60 * time.Second

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
				continue
			}
			srcCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			a.pollSource(srcCtx, src)
			cancel()
		}

		interval := defaultLoopInterval
		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}
	}
}
