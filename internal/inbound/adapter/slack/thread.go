// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"sort"
	"strings"
	"time"
)

const (
	slackThreadCacheTTL        = 30 * 24 * time.Hour
	slackThreadRefreshInterval = 10 * time.Minute
	slackThreadCacheMaxEntries = 500
	slackThreadHydrationBatch  = 1
)

type slackThreadCacheEntry struct {
	RootTS               string `json:"root_ts"`
	LatestReplyTS        string `json:"latest_reply_ts,omitempty"`
	ReplyCount           int    `json:"reply_count"`
	LastHydratedAtMicros int64  `json:"last_hydrated_at_micros,omitempty"`
	LastSeenAtMicros     int64  `json:"last_seen_at_micros,omitempty"`
}

type slackThreadCache map[string]slackThreadCacheEntry

func newSlackThreadCache(entries []slackThreadCacheEntry) slackThreadCache {
	cache := slackThreadCache{}
	for _, entry := range entries {
		entry.RootTS = strings.TrimSpace(entry.RootTS)
		if entry.RootTS == "" {
			continue
		}
		entry.LatestReplyTS = strings.TrimSpace(entry.LatestReplyTS)
		cache[entry.RootTS] = entry
	}
	return cache
}

func (c slackThreadCache) recordHistory(msg slackMessage, nowMicros int64) bool {
	rootTS := messageThreadTS(msg)
	if rootTS == "" {
		rootTS = strings.TrimSpace(msg.Ts)
	}
	if rootTS == "" {
		return false
	}
	entry, ok := c[rootTS]
	if !ok {
		entry = slackThreadCacheEntry{RootTS: rootTS}
	}
	changed := !ok
	if entry.RootTS != rootTS {
		entry.RootTS = rootTS
		changed = true
	}
	if entry.LastSeenAtMicros == 0 || nowMicros-entry.LastSeenAtMicros >= int64(slackThreadRefreshInterval/time.Microsecond) {
		changed = true
	}
	entry.LastSeenAtMicros = nowMicros
	if entry.ReplyCount != msg.ReplyCount {
		entry.ReplyCount = msg.ReplyCount
		changed = true
	}
	c[rootTS] = entry
	return c.compact(nowMicros) || changed
}

func (c slackThreadCache) refreshCandidates(seen map[string]struct{}, nowMicros int64, limit int) []string {
	if limit <= 0 || len(c) == 0 {
		return nil
	}
	type candidate struct {
		rootTS               string
		lastHydratedAtMicros int64
		lastSeenAtMicros     int64
	}
	cands := make([]candidate, 0, len(c))
	for rootTS, entry := range c {
		if _, ok := seen[rootTS]; ok {
			continue
		}
		if !entry.shouldRefresh(nowMicros) {
			continue
		}
		cands = append(cands, candidate{
			rootTS:               rootTS,
			lastHydratedAtMicros: entry.LastHydratedAtMicros,
			lastSeenAtMicros:     entry.LastSeenAtMicros,
		})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].lastHydratedAtMicros == cands[j].lastHydratedAtMicros {
			if cands[i].lastSeenAtMicros == cands[j].lastSeenAtMicros {
				return cands[i].rootTS < cands[j].rootTS
			}
			return cands[i].lastSeenAtMicros < cands[j].lastSeenAtMicros
		}
		if cands[i].lastHydratedAtMicros == 0 {
			return true
		}
		if cands[j].lastHydratedAtMicros == 0 {
			return false
		}
		return cands[i].lastHydratedAtMicros < cands[j].lastHydratedAtMicros
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]string, 0, len(cands))
	for _, cand := range cands {
		out = append(out, cand.rootTS)
	}
	return out
}

func (c slackThreadCache) markHydrated(rootTS string, replyCount int, latestReplyTS string, nowMicros int64) bool {
	rootTS = strings.TrimSpace(rootTS)
	if rootTS == "" {
		return false
	}
	entry, ok := c[rootTS]
	if !ok {
		entry = slackThreadCacheEntry{RootTS: rootTS}
	}
	changed := !ok
	if entry.RootTS != rootTS {
		entry.RootTS = rootTS
		changed = true
	}
	if entry.ReplyCount != replyCount {
		entry.ReplyCount = replyCount
		changed = true
	}
	if latest := strings.TrimSpace(latestReplyTS); latest != "" && entry.LatestReplyTS != latest {
		entry.LatestReplyTS = latest
		changed = true
	}
	if entry.LastHydratedAtMicros != nowMicros {
		entry.LastHydratedAtMicros = nowMicros
		changed = true
	}
	if entry.LastSeenAtMicros != nowMicros {
		entry.LastSeenAtMicros = nowMicros
		changed = true
	}
	c[rootTS] = entry
	return c.compact(nowMicros) || changed
}

func (e slackThreadCacheEntry) shouldRefresh(nowMicros int64) bool {
	if e.RootTS == "" {
		return false
	}
	if nowMicros-e.LastSeenAtMicros > int64(slackThreadCacheTTL/time.Microsecond) {
		return false
	}
	if e.LastHydratedAtMicros == 0 {
		return true
	}
	return nowMicros-e.LastHydratedAtMicros >= int64(slackThreadRefreshInterval/time.Microsecond)
}

func (c slackThreadCache) compact(nowMicros int64) bool {
	changed := false
	if len(c) == 0 {
		return false
	}
	ttlMicros := int64(slackThreadCacheTTL / time.Microsecond)
	for rootTS, entry := range c {
		if entry.LastSeenAtMicros > 0 && nowMicros-entry.LastSeenAtMicros > ttlMicros {
			delete(c, rootTS)
			changed = true
		}
	}
	if len(c) <= slackThreadCacheMaxEntries {
		return changed
	}
	type entryRef struct {
		rootTS         string
		lastSeenMicros int64
	}
	refs := make([]entryRef, 0, len(c))
	for rootTS, entry := range c {
		refs = append(refs, entryRef{rootTS: rootTS, lastSeenMicros: entry.LastSeenAtMicros})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].lastSeenMicros == refs[j].lastSeenMicros {
			return refs[i].rootTS < refs[j].rootTS
		}
		return refs[i].lastSeenMicros < refs[j].lastSeenMicros
	})
	for len(c) > slackThreadCacheMaxEntries && len(refs) > 0 {
		victim := refs[0]
		delete(c, victim.rootTS)
		refs = refs[1:]
		changed = true
	}
	return changed
}

func (c slackThreadCache) snapshot(nowMicros int64) []slackThreadCacheEntry {
	c.compact(nowMicros)
	if len(c) == 0 {
		return nil
	}
	out := make([]slackThreadCacheEntry, 0, len(c))
	for _, entry := range c {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeenAtMicros == out[j].LastSeenAtMicros {
			return out[i].RootTS < out[j].RootTS
		}
		return out[i].LastSeenAtMicros > out[j].LastSeenAtMicros
	})
	return out
}
