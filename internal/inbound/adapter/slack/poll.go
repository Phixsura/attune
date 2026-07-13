// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	maxHistoryBatch    = 15
	pollSourceTimeout  = 30 * time.Second
	syncLookbackMicros = int64(syncLookback / time.Microsecond)
)

// nowFn is overrideable in tests; production uses time.Now.
var nowFn = time.Now

type slackPollState struct {
	src        inbound.Source
	cache      slackThreadCache
	seenRoots  map[string]struct{}
	scheduled  map[string]struct{}
	nowMicros  int64
	cacheDirty bool
	pollError  string
}

type slackThreadHydrationState struct {
	latestReplyTS string
	replyCount    int
	replyIngested int
}

type slackConfigUpdater interface {
	UpdateConfig(ctx context.Context, id string, config []byte) error
}

var newPollTicker = func(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

func (a *adapter) pollLoop(ctx context.Context) {
	defer a.wg.Done()

	tickC, stop := newPollTicker(defaultPollInterval)
	defer stop()

	for {
		a.pollAllSources(ctx)
		select {
		case <-tickC:
		case <-ctx.Done():
			return
		}
	}
}

func (a *adapter) pollAllSources(ctx context.Context) {
	sources, err := a.deps.Sources.List(ctx, channelName)
	if err != nil {
		logext.Warnf(ctx, "[inbound.slack.pollLoop] list sources failed,err:%+v", err.Error())
		return
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		if !src.Enabled {
			continue
		}
		srcCtx, cancel := context.WithTimeout(ctx, pollSourceTimeout)
		a.pollSource(srcCtx, src)
		cancel()
	}
}

func (a *adapter) pollSource(ctx context.Context, src inbound.Source) {
	const where = "inbound.slack.pollSource"
	start := nowFn()
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, a.pollLagSeconds(src.Slug, start))
	defer func() {
		a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(start).Seconds())
	}()

	cfg, tokenBytes, err := parseConfig(src.Config, a.deps.Secrets)
	if err != nil {
		logext.Warnf(ctx, "[%s] decrypt config failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: src.State.LastEventAt,
			LastUID:     src.State.LastUID,
			LastError:   "decrypt config",
		})
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return
	}
	defer wipeBytes(tokenBytes)

	var client apiClient
	if a.newClient != nil {
		client = a.newClient(string(tokenBytes))
	}
	if client == nil {
		client = newAPIClient(string(tokenBytes))
	}

	src = a.seedFirstPollCursor(ctx, src)
	oldest := src.State.LastUID - syncLookbackMicros
	if oldest < 0 {
		oldest = 0
	}

	messages, err := client.History(ctx, cfg.ChannelID, oldest, maxHistoryBatch)
	if err != nil {
		a.handleSlackHistoryFailure(ctx, src, where, err)
		return
	}

	state := a.collectSlackPollState(ctx, src, cfg, client, messages, where)
	if state.cacheDirty {
		cfg.ThreadCache = state.cache.snapshot(state.nowMicros)
		if err := a.persistSlackConfig(ctx, state.src.ID, tokenBytes, cfg); err != nil {
			logext.Warnf(ctx, "[%s] persist config failed,source_id:%s,err:%+v", where, state.src.ID, err.Error())
		}
	}

	a.persistPollResult(ctx, state.src, state.src.State.LastUID, nowFn(), state.pollError)
	a.markPollSuccess(state.src.Slug, nowFn())
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, 0)
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
}

func (a *adapter) handleSlackHistoryFailure(ctx context.Context, src inbound.Source, where string, err error) {
	if isPermanentSlackError(err) {
		logext.Warnf(ctx, "[%s] auth failed,disabling source,source_id:%s,err:%s", where, src.ID, err.Error())
		_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "slack authentication failed")
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
		return
	}
	logext.Warnf(ctx, "[%s] history failed,source_id:%s,err:%+v", where, src.ID, err.Error())
	a.transientError(ctx, src, "history: transient")
}

func (a *adapter) collectSlackPollState(ctx context.Context, src inbound.Source, cfg Config, client apiClient, messages []slackMessage, where string) slackPollState {
	sort.Slice(messages, func(i, j int) bool {
		return messageMicros(messages[i]) < messageMicros(messages[j])
	})
	state := slackPollState{
		src:       src,
		cache:     newSlackThreadCache(cfg.ThreadCache),
		seenRoots: map[string]struct{}{},
		scheduled: map[string]struct{}{},
		nowMicros: nowFn().UnixMicro(),
	}
	for _, msg := range messages {
		if ctx.Err() != nil {
			return state
		}
		state = a.applySlackHistoryMessage(ctx, cfg, msg, where, state)
	}
	state = a.scheduleSlackThreadRefreshes(state)
	state = a.hydrateSlackScheduledThreads(ctx, client, cfg, where, state)
	return state
}

func (a *adapter) applySlackHistoryMessage(ctx context.Context, cfg Config, msg slackMessage, where string, state slackPollState) slackPollState {
	if !isIngestibleSlackMessage(msg) {
		return state
	}
	tsMicros, tsErr := slackTimestampMicros(msg.Ts)
	if tsErr != nil {
		logext.Warnf(ctx, "[%s] bad timestamp,source_id:%s,ts:%q,err:%+v", where, state.src.ID, msg.Ts, tsErr.Error())
		a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "validate_err")
		return state
	}
	threadTS := messageThreadTS(msg)
	if messageKind(msg) == "root" {
		state.seenRoots[threadTS] = struct{}{}
		if state.cache.recordHistory(msg, state.nowMicros) {
			state.cacheDirty = true
		}
		if shouldHydrateSlackThread(msg, state.cache, state.nowMicros) && len(state.scheduled) < slackThreadHydrationBatch {
			if _, ok := state.scheduled[threadTS]; !ok {
				state.scheduled[threadTS] = struct{}{}
			}
		}
	}
	content := normalizeMessageText(msg.Text)
	if content == "" {
		a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "validate_err")
		state.cacheDirty = true
		return state
	}
	authorID, authorKind := messageAuthor(msg)
	permalink := messagePermalink(cfg.WorkspaceURL, cfg.ChannelID, threadTS, msg.Ts)
	in := domain.IngestInput{
		Source:     channelName,
		Content:    content,
		SourceUser: authorID,
		PageURL:    permalink,
		SourceMeta: buildSlackSourceMeta(state.src.ID, state.src.Name, slackAuthInfo{
			TeamID:       cfg.TeamID,
			TeamName:     cfg.TeamName,
			WorkspaceURL: cfg.WorkspaceURL,
		}, slackChannel{
			ID:         cfg.ChannelID,
			Name:       cfg.ChannelName,
			IsPrivate:  false,
			IsArchived: false,
			IsShared:   false,
		}, msg.Ts, threadTS, permalink, authorID, authorKind, messageKind(msg), msg.ReplyCount, msg.LatestReply),
		IdempotencyKey: slackIdempotencyKey(cfg.TeamID, cfg.ChannelID, msg.Ts),
	}
	if _, err := a.deps.Ingest.Ingest(ctx, state.src.TenantID, uuid.Nil, in); err != nil {
		if isSlackDuplicateError(err) {
			logext.Warnf(ctx, "[%s] ingest conflict,source_id:%s,ts:%s,err:%s", where, state.src.ID, msg.Ts, err.Error())
			a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "validate_err")
		} else {
			logext.Warnf(ctx, "[%s] ingest failed,source_id:%s,ts:%s,err:%+v", where, state.src.ID, msg.Ts, err.Error())
			a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "internal_err")
		}
		state.cacheDirty = true
		state.src.State.LastUID = maxInt64(state.src.State.LastUID, tsMicros)
		return state
	}
	state.src.State.LastUID = maxInt64(state.src.State.LastUID, tsMicros)
	a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "ok")
	return state
}

func (a *adapter) scheduleSlackThreadRefreshes(state slackPollState) slackPollState {
	if remaining := slackThreadHydrationBatch - len(state.scheduled); remaining > 0 {
		for _, rootTS := range state.cache.refreshCandidates(state.seenRoots, state.nowMicros, remaining) {
			state.scheduled[rootTS] = struct{}{}
		}
	}
	return state
}

func (a *adapter) hydrateSlackScheduledThreads(ctx context.Context, client apiClient, cfg Config, where string, state slackPollState) slackPollState {
	for rootTS := range state.scheduled {
		changed, hydrateErr := a.hydrateSlackThread(ctx, client, state.src, cfg, state.cache, rootTS, state.nowMicros)
		if hydrateErr != nil {
			if isPermanentSlackError(hydrateErr) {
				logext.Warnf(ctx, "[%s] thread hydrate auth failed,disabling source,source_id:%s,root_ts:%s,err:%s", where, state.src.ID, rootTS, hydrateErr.Error())
				_ = a.deps.Sources.SetEnabled(ctx, state.src.ID, false, "slack authentication failed")
				a.deps.Metrics.Total(channelName, state.src.TenantID, state.src.Slug, "auth_err")
				a.deps.Metrics.SetSourceState(channelName, state.src.TenantID, state.src.Slug, "enabled", false)
				return state
			}
			logext.Warnf(ctx, "[%s] thread hydrate failed,source_id:%s,root_ts:%s,err:%+v", where, state.src.ID, rootTS, hydrateErr.Error())
			a.transientError(ctx, state.src, "thread hydrate: transient")
			if state.pollError == "" {
				state.pollError = "thread hydrate: transient"
			}
			continue
		}
		if changed {
			state.cacheDirty = true
		}
	}
	return state
}

func (a *adapter) hydrateSlackThread(ctx context.Context, client apiClient, src inbound.Source, cfg Config, cache slackThreadCache, rootTS string, nowMicros int64) (bool, error) {
	entry, ok := cache[rootTS]
	if !ok {
		entry = slackThreadCacheEntry{RootTS: rootTS}
	}
	oldestMicros := slackTimestampMicrosOrZero(entry.LatestReplyTS)
	if oldestMicros == 0 {
		oldestMicros = slackTimestampMicrosOrZero(rootTS)
	}
	replies, err := client.Replies(ctx, cfg.ChannelID, rootTS, oldestMicros, maxHistoryBatch)
	if err != nil {
		if isSlackThreadNotFoundError(err) {
			return cache.markHydrated(rootTS, entry.ReplyCount, entry.LatestReplyTS, nowMicros), nil
		}
		return false, err
	}

	sort.Slice(replies, func(i, j int) bool {
		return messageMicros(replies[i]) < messageMicros(replies[j])
	})

	state := slackThreadHydrationState{
		latestReplyTS: entry.LatestReplyTS,
		replyCount:    entry.ReplyCount,
	}
	threadChanged := false
	for _, msg := range replies {
		var changed bool
		state, changed = a.applySlackThreadReply(ctx, src, cfg, rootTS, msg, state)
		if changed {
			threadChanged = true
		}
	}

	var changed bool
	state, changed = finalizeSlackThreadHydration(replies, rootTS, state)
	if changed {
		threadChanged = true
	}

	if cache.markHydrated(rootTS, state.replyCount, state.latestReplyTS, nowMicros) {
		threadChanged = true
	}
	return threadChanged, nil
}

func (a *adapter) applySlackThreadReply(ctx context.Context, src inbound.Source, cfg Config, rootTS string, msg slackMessage, state slackThreadHydrationState) (slackThreadHydrationState, bool) {
	ts := strings.TrimSpace(msg.Ts)
	if ts == "" || ts == strings.TrimSpace(rootTS) {
		return state, false
	}
	if !isIngestibleSlackMessage(msg) {
		return state, false
	}
	tsMicros, tsErr := slackTimestampMicros(msg.Ts)
	if tsErr != nil {
		logext.Warnf(ctx, "[inbound.slack.hydrateThread] bad timestamp,source_id:%s,root_ts:%s,ts:%q,err:%+v", src.ID, rootTS, msg.Ts, tsErr.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		return state, false
	}
	content := normalizeMessageText(msg.Text)
	if content == "" {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		return state, false
	}
	authorID, authorKind := messageAuthor(msg)
	threadTS := messageThreadTS(msg)
	permalink := messagePermalink(cfg.WorkspaceURL, cfg.ChannelID, threadTS, msg.Ts)
	threadReplyCount := state.replyCount
	if threadReplyCount < state.replyIngested+1 {
		threadReplyCount = state.replyIngested + 1
	}
	in := domain.IngestInput{
		Source:     channelName,
		Content:    content,
		SourceUser: authorID,
		PageURL:    permalink,
		SourceMeta: buildSlackSourceMeta(src.ID, src.Name, slackAuthInfo{
			TeamID:       cfg.TeamID,
			TeamName:     cfg.TeamName,
			WorkspaceURL: cfg.WorkspaceURL,
		}, slackChannel{
			ID:         cfg.ChannelID,
			Name:       cfg.ChannelName,
			IsPrivate:  false,
			IsArchived: false,
			IsShared:   false,
		}, msg.Ts, threadTS, permalink, authorID, authorKind, messageKind(msg), threadReplyCount, state.latestReplyTS),
		IdempotencyKey: slackIdempotencyKey(cfg.TeamID, cfg.ChannelID, msg.Ts),
	}
	if _, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in); err != nil {
		if isSlackDuplicateError(err) {
			logext.Warnf(ctx, "[inbound.slack.hydrateThread] ingest conflict,source_id:%s,root_ts:%s,ts:%s,err:%s", src.ID, rootTS, msg.Ts, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		} else {
			logext.Warnf(ctx, "[inbound.slack.hydrateThread] ingest failed,source_id:%s,root_ts:%s,ts:%s,err:%+v", src.ID, rootTS, msg.Ts, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		}
		return state, false
	}
	state.replyIngested++
	if tsMicros > 0 {
		state.latestReplyTS = msg.Ts
	}
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	return state, true
}

func finalizeSlackThreadHydration(replies []slackMessage, rootTS string, state slackThreadHydrationState) (slackThreadHydrationState, bool) {
	changed := false
	if len(replies) > 0 {
		lastReplyTS := latestReplyTSFromBatch(replies, rootTS)
		if lastReplyTS != "" && lastReplyTS != state.latestReplyTS {
			state.latestReplyTS = lastReplyTS
			changed = true
		}
	}
	if state.replyCount < state.replyIngested {
		state.replyCount = state.replyIngested
		changed = true
	}
	return state, changed
}

func (a *adapter) persistSlackConfig(ctx context.Context, sourceID string, tokenBytes []byte, cfg Config) error {
	updater, ok := a.deps.Sources.(slackConfigUpdater)
	if !ok {
		return nil
	}
	encToken, err := a.deps.Secrets.Encrypt(tokenBytes)
	if err != nil {
		return fmt.Errorf("encrypt slack token: %w", err)
	}
	cfg.TokenEncrypted = encToken
	raw, err := jsonMarshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal slack config: %w", err)
	}
	encrypted, err := a.deps.Secrets.Encrypt(raw)
	if err != nil {
		return fmt.Errorf("encrypt slack config: %w", err)
	}
	return updater.UpdateConfig(ctx, sourceID, encrypted)
}

func shouldHydrateSlackThread(msg slackMessage, cache slackThreadCache, nowMicros int64) bool {
	rootTS := messageThreadTS(msg)
	if rootTS == "" {
		rootTS = strings.TrimSpace(msg.Ts)
	}
	entry, ok := cache[rootTS]
	if !ok {
		return msg.ReplyCount > 0
	}
	if entry.LastHydratedAtMicros == 0 {
		return msg.ReplyCount > 0
	}
	if msg.ReplyCount > entry.ReplyCount {
		return true
	}
	if latest := strings.TrimSpace(msg.LatestReply); latest != "" && latest != entry.LatestReplyTS {
		return true
	}
	if msg.ReplyCount > 0 && entry.LastHydratedAtMicros > 0 {
		return nowMicros-entry.LastHydratedAtMicros >= int64(slackThreadRefreshInterval/time.Microsecond)
	}
	return false
}

func latestReplyTSFromBatch(messages []slackMessage, rootTS string) string {
	for i := len(messages) - 1; i >= 0; i-- {
		ts := strings.TrimSpace(messages[i].Ts)
		if ts == "" || ts == strings.TrimSpace(rootTS) {
			continue
		}
		return ts
	}
	return ""
}

func slackTimestampMicrosOrZero(ts string) int64 {
	micros, err := slackTimestampMicros(ts)
	if err != nil {
		return 0
	}
	return micros
}

func (a *adapter) seedFirstPollCursor(ctx context.Context, src inbound.Source) inbound.Source {
	if src.State.LastUID != 0 {
		return src
	}
	seed := nowFn().UnixMicro() + syncLookbackMicros
	if seed < 0 {
		seed = 0
	}
	if err := a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     seed,
	}); err != nil {
		logext.Warnf(ctx, "[inbound.slack.seedFirstPollCursor] seed cursor failed,source_id:%s,err:%+v", src.ID, err.Error())
	}
	src.State.LastUID = seed
	return src
}

func (a *adapter) persistPollResult(ctx context.Context, src inbound.Source, lastUID int64, eventAt time.Time, lastError string) {
	if strings.TrimSpace(lastError) == "" {
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: ptrext.Of(eventAt),
			LastUID:     lastUID,
		})
		return
	}
	_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     lastUID,
		LastError:   lastError,
	})
}

func (a *adapter) pollLagSeconds(slug string, now time.Time) float64 {
	a.mu.Lock()
	last, ok := a.lastSuccessAt[slug]
	a.mu.Unlock()
	if !ok {
		return 0
	}
	return now.Sub(last).Seconds()
}

func (a *adapter) markPollSuccess(slug string, t time.Time) {
	a.mu.Lock()
	if a.lastSuccessAt == nil {
		a.lastSuccessAt = map[string]time.Time{}
	}
	a.lastSuccessAt[slug] = t
	a.mu.Unlock()
}

func (a *adapter) transientError(ctx context.Context, src inbound.Source, reason string) {
	_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     src.State.LastUID,
		LastError:   reason,
	})
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
}

func isIngestibleSlackMessage(msg slackMessage) bool {
	if strings.TrimSpace(msg.Type) != "message" {
		return false
	}
	switch strings.TrimSpace(msg.Subtype) {
	case "", "bot_message":
		return true
	default:
		return false
	}
}

func slackIdempotencyKey(teamID, channelID, ts string) string {
	parts := []string{
		"slack",
		sanitizeKeyPart(teamID),
		sanitizeKeyPart(channelID),
		slackTimestampSlug(ts),
	}
	return strings.Join(parts, "_")
}

func sanitizeKeyPart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func messageMicros(msg slackMessage) int64 {
	micros, err := slackTimestampMicros(msg.Ts)
	if err != nil {
		return 0
	}
	return micros
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
