// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"time"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// loopInterval is the fixed between-rounds wait per spec §Email IMAP
// adapter pollLoop pseudocode. The batch-loop topology (one tick polls
// every enabled source serially) does not honour a per-source interval;
// the proto field that previously declared this knob was removed before
// release (#66 review H2). A per-source-goroutine refactor is a future
// follow-up.
const loopInterval = 60 * time.Second

// pollLoop iterates enabled email sources, polling each within its own
// 30-second budget. The list-then-poll pattern means newly-enabled
// sources are picked up on the next tick without restarting the loop.
func (a *adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()

	for {
		sources, err := a.deps.Sources.List(ctx, channelName)
		if err != nil {
			logext.Warnf(ctx, "[inbound.email.pollLoop] list sources failed,err:%+v", err.Error())
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

		select {
		case <-time.After(loopInterval):
		case <-ctx.Done():
			return
		}
	}
}
