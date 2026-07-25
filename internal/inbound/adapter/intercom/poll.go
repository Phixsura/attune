// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	pollSourceTimeout = 30 * time.Second

	// defaultMaxDetailFetches caps per-tick conversation detail API calls.
	// Overridable via Config.MaxDetailFetches.
	defaultMaxDetailFetches = 50

	// maxPagesPerTick caps search pagination within a single poll tick.
	// At 150 conversations/page this lists up to 1,500 conversations per
	// 60s tick during backfill.
	maxPagesPerTick = 10

	// daySeconds floors search start times to UTC midnight — Intercom
	// search timestamps are date-indexed, so sub-day filter precision is
	// unreliable (the same defense Airbyte's production connector uses).
	daySeconds = 86400

	// rateBudgetFloor is the X-RateLimit-Remaining threshold below which
	// a tick stops early. Private apps share one 10k req/min budget per
	// app across the workspace; attune must not starve other consumers
	// during backfill. ~1% of the default budget.
	rateBudgetFloor = 100
)

// nowFn is overrideable in tests; production uses time.Now.
var nowFn = time.Now

// jsonMarshal is a test seam for config persistence.
var jsonMarshal = json.Marshal

type intercomConfigUpdater interface {
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
		case sourceID := <-a.syncNow:
			a.pollSingleSource(ctx, sourceID)
		case <-ctx.Done():
			return
		}
	}
}

func (a *adapter) pollAllSources(ctx context.Context) {
	sources, err := a.deps.Sources.List(ctx, channelName)
	if err != nil {
		logext.Warnf(ctx, "[inbound.intercom.pollLoop] list sources failed,err:%+v", err.Error())
		return
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		if !src.Enabled {
			continue
		}
		if a.shouldSkipBackoff(src.Slug) {
			continue
		}
		srcCtx, cancel := context.WithTimeout(ctx, pollSourceTimeout)
		a.pollSource(srcCtx, src)
		cancel()
	}
}

// pollSingleSource fetches one source by ID and polls it immediately.
// Used by TriggerSync (sync-now).
func (a *adapter) pollSingleSource(ctx context.Context, sourceID string) {
	src, err := a.deps.Sources.Get(ctx, sourceID)
	if err != nil {
		logext.Warnf(ctx, "[inbound.intercom.syncNow] get source failed,source_id:%s,err:%+v", sourceID, err.Error())
		return
	}
	if !src.Enabled || src.Channel != channelName {
		return
	}
	srcCtx, cancel := context.WithTimeout(ctx, pollSourceTimeout)
	defer cancel()
	a.pollSource(srcCtx, src)
}

func (a *adapter) pollSource(ctx context.Context, src inbound.Source) {
	const where = "inbound.intercom.pollSource"
	start := nowFn()
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, a.pollLagSeconds(src.Slug, start))
	defer func() {
		a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(start).Seconds())
	}()

	cfg, token, err := parseConfig(src.Config, a.deps.Secrets)
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
	defer wipeBytes(token)

	apiBase := baseURL(cfg.Region)
	if err := validateHost(apiBase); err != nil {
		logext.Warnf(ctx, "[%s] host validation failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return
	}
	client := a.newClient(cfg.Region, string(token))

	sr := a.syncPages(ctx, src, cfg, client, where)
	if sr.failed {
		return // error already handled inside syncPages
	}

	newUID := src.State.LastUID
	if sr.watermark > newUID {
		newUID = sr.watermark
	}
	a.persistPollResult(ctx, src, newUID, nowFn(), sr.pollError)
	a.markPollSuccess(src.Slug, nowFn())
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, 0)
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
}

// syncResult aggregates state across all pages in a single poll tick.
type syncResult struct {
	watermark int64  // max fully-processed updated_at
	pollError string // transient per-item error note, if any
	failed    bool   // true if the search fetch failed (already handled)
}

// syncPages walks the updated_at-ascending search results and processes
// each qualifying conversation. The watermark only advances past
// conversations that were actually processed (ingested or filtered), so
// budget-exhausted items are re-covered next tick.
func (a *adapter) syncPages(ctx context.Context, src inbound.Source, cfg Config, client apiClient, where string) syncResult {
	watermark := src.State.LastUID
	if watermark == 0 {
		watermark = a.seedWatermark(cfg)
	}
	// Day-floor the query start (date-indexed search); re-filter
	// client-side at second precision below.
	queryStart := (watermark / daySeconds) * daySeconds
	// Exclusive upper bound: "now" so in-flight updates land next tick.
	queryEnd := nowFn().Unix() + 1

	st := pageState{
		res:           syncResult{watermark: watermark},
		baseWatermark: watermark,
		detailBudget:  effectiveDetailBudget(cfg),
		companyCache:  map[string]intercomCompany{},
	}
	cursor := ""

	for pageNum := 0; pageNum < maxPagesPerTick; pageNum++ {
		if ctx.Err() != nil {
			return st.res
		}
		page, err := client.SearchConversations(ctx, queryStart, queryEnd, cursor)
		if err != nil {
			a.handleSearchFailure(ctx, src, where, err)
			st.res.failed = true
			return st.res
		}

		var stop bool
		st, stop = a.processConversationPage(ctx, client, src, cfg, page, st, where)
		if stop {
			return st.res
		}

		if page.StartingAfter == "" {
			// End of results — backfill complete for this window.
			cfg.SyncStats.BackfillDone = true
			cfg.SyncStats.ConversationsSynced += int64(st.totalSynced)
			a.persistSyncStats(ctx, src.ID, cfg, where)
			logext.Infof(ctx, "[%s] progress,source_id:%s,page:%d,conversations_synced:%d,end_of_results:true",
				where, src.ID, pageNum+1, st.totalSynced)
			return st.res
		}
		// Proactive self-throttle: stop the tick before Intercom starts
		// returning 429s. The watermark holds only processed items, so
		// the remainder is re-covered next tick.
		if budget := client.RateBudget(); budget >= 0 && budget < rateBudgetFloor {
			logext.Infof(ctx, "[%s] rate budget low,stopping tick,source_id:%s,remaining:%d", where, src.ID, budget)
			break
		}
		cursor = page.StartingAfter
		logext.Infof(ctx, "[%s] progress,source_id:%s,page:%d,conversations_synced:%d,end_of_results:false",
			where, src.ID, pageNum+1, st.totalSynced)
	}
	cfg.SyncStats.ConversationsSynced += int64(st.totalSynced)
	a.persistSyncStats(ctx, src.ID, cfg, where)
	return st.res
}

// pageState threads mutable sync progress through page processing.
type pageState struct {
	res syncResult
	// baseWatermark is the effective watermark at tick start (seeded on
	// first sync) — the client-side second-precision filter boundary.
	baseWatermark int64
	detailFetches int
	detailBudget  int
	totalSynced   int
	// companyCache dedups GetCompany calls within one poll tick.
	companyCache map[string]intercomCompany
	// admins resolves assignee IDs to teammate names; nil until the
	// first conversation needs it (one ListAdmins call per tick, lazy).
	admins map[int64]intercomAdmin
}

// processConversationPage handles one search page. Returns the updated
// state and stop=true when the caller should stop paginating (budget
// exhausted or source disabled).
func (a *adapter) processConversationPage(
	ctx context.Context,
	client apiClient,
	src inbound.Source,
	cfg Config,
	page conversationPage,
	st pageState,
	where string,
) (pageState, bool) {
	for i := range page.Conversations {
		if ctx.Err() != nil {
			return st, true
		}
		summary := page.Conversations[i]
		// Client-side second-precision filter over the day-floored window.
		if summary.UpdatedAt <= st.baseWatermark {
			continue
		}
		if !matchesFilter(summary, cfg) {
			// Filtered out — still advance the watermark past it.
			if summary.UpdatedAt > st.res.watermark {
				st.res.watermark = summary.UpdatedAt
			}
			continue
		}
		if st.detailFetches >= st.detailBudget {
			// Budget exhausted: stop here; watermark stays at the last
			// processed item so this conversation is re-covered next tick.
			logext.Infof(ctx, "[%s] detail budget exhausted,source_id:%s,budget:%d", where, src.ID, st.detailBudget)
			return st, true
		}
		st.detailFetches++

		// Lazy teammate resolution: one ListAdmins per tick, and only
		// when a qualifying conversation actually has an assignee.
		if st.admins == nil && (summary.AdminAssigneeID > 0 || summary.TeamAssigneeID > 0) {
			st.admins = a.resolveAdmins(ctx, client, where)
		}

		disabled, ingested := a.fetchAndIngest(ctx, client, src, cfg, summary, st.companyCache, st.admins, where)
		if disabled {
			return st, true
		}
		if ingested {
			st.totalSynced++
		}
		if summary.UpdatedAt > st.res.watermark {
			st.res.watermark = summary.UpdatedAt
		}
	}
	return st, false
}

// fetchAndIngest pulls the full thread for one conversation and ingests
// it. Returns disabled=true when a permanent error on the search-level
// API disabled the source; per-conversation failures degrade gracefully.
func (a *adapter) fetchAndIngest(ctx context.Context, client apiClient, src inbound.Source, cfg Config, summary conversation, companyCache map[string]intercomCompany, admins map[int64]intercomAdmin, where string) (disabled, ingested bool) {
	conv, err := client.GetConversation(ctx, summary.ID)
	if err != nil {
		// Per-conversation degradation: a deleted/restricted conversation
		// must not block the rest of the sync (zendesk #229 lesson §4b).
		logext.Warnf(ctx, "[%s] detail fetch failed,skipping,source_id:%s,conversation_id:%s,err:%s",
			where, src.ID, summary.ID, err.Error())
		if isPermanentIntercomError(err) {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "detail_auth_err")
		} else {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
		}
		// Fall back to the summary shape (no parts) — the seed message
		// still carries signal.
		conv = summary
	}

	contacts := a.resolveContacts(ctx, client, conv, where)
	conv.Company = a.resolveCompany(ctx, client, conv.Company, companyCache, where)
	in := buildIngestInput(src.ID, src.Name, cfg.WorkspaceID, conv, contacts, admins)
	if _, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in); err != nil {
		if isDuplicateError(err) {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		} else {
			logext.Warnf(ctx, "[%s] ingest failed,source_id:%s,conversation_id:%s,err:%+v",
				where, src.ID, conv.ID, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		}
		return false, false
	}
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	return false, true
}

// resolveAdmins fetches the workspace teammate directory once per tick.
// Failure returns an empty (non-nil) map so the lazy trigger doesn't
// re-fire on every conversation.
func (a *adapter) resolveAdmins(ctx context.Context, client apiClient, where string) map[int64]intercomAdmin {
	result := map[int64]intercomAdmin{}
	admins, err := client.ListAdmins(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s] resolve admins failed,err:%+v", where, err.Error())
		return result
	}
	for _, ad := range admins {
		if id, perr := strconv.ParseInt(ad.ID, 10, 64); perr == nil {
			result[id] = ad
		}
	}
	return result
}

// resolveCompany upgrades a conversation's embedded company reference
// (id/name only) to the full profile (monthly_spend, plan, size) — the
// revenue context behind attribution. Cached per poll tick: many
// conversations share one company. Resolution failure falls back to the
// embedded reference — never blocks ingestion.
func (a *adapter) resolveCompany(ctx context.Context, client apiClient, ref *intercomCompany, cache map[string]intercomCompany, where string) *intercomCompany {
	if ref == nil || ref.ID == "" {
		return ref
	}
	if cached, ok := cache[ref.ID]; ok {
		return ptrext.Of(cached)
	}
	full, err := client.GetCompany(ctx, ref.ID)
	if err != nil {
		logext.Warnf(ctx, "[%s] resolve company failed,company_id:%s,err:%+v", where, ref.ID, err.Error())
		cache[ref.ID] = ptrext.Indirect(ref) // negative-cache the bare ref for this tick
		return ref
	}
	if full.Name == "" {
		full.Name = ref.Name
	}
	cache[ref.ID] = full
	return ptrext.Of(full)
}

// resolveContacts batch-resolves the conversation's contact references.
// On failure, returns an empty map — normalization falls back to the
// author identity inline in the thread.
func (a *adapter) resolveContacts(ctx context.Context, client apiClient, conv conversation, where string) map[string]intercomContact {
	if len(conv.Contacts.Contacts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(conv.Contacts.Contacts))
	for _, ref := range conv.Contacts.Contacts {
		if ref.ID != "" {
			ids = append(ids, ref.ID)
		}
	}
	resolved, err := client.SearchContacts(ctx, ids)
	if err != nil {
		logext.Warnf(ctx, "[%s] resolve contacts failed,err:%+v", where, err.Error())
		return nil
	}
	result := make(map[string]intercomContact, len(resolved))
	for _, c := range resolved {
		result[c.ID] = c
	}
	return result
}

// seedWatermark determines the first-sync starting point.
func (a *adapter) seedWatermark(cfg Config) int64 {
	switch cfg.StartFrom {
	case "now":
		// Start from 5 minutes ago to catch recent conversations.
		return nowFn().Add(-5 * time.Minute).Unix()
	default:
		// Full backfill from the beginning of time.
		return 0
	}
}

func effectiveDetailBudget(cfg Config) int {
	if cfg.MaxDetailFetches > 0 {
		return cfg.MaxDetailFetches
	}
	return defaultMaxDetailFetches
}

func (a *adapter) handleSearchFailure(ctx context.Context, src inbound.Source, where string, err error) {
	var rle rateLimitError
	if errors.As(err, &rle) {
		logext.Warnf(ctx, "[%s] rate limited,source_id:%s,retry_after:%s", where, src.ID, rle.RetryAfter)
		a.transientError(ctx, src, fmt.Sprintf("rate limited: retry after %s", rle.RetryAfter))
		select {
		case <-time.After(rle.RetryAfter):
		case <-ctx.Done():
		}
		return
	}
	if isPermanentIntercomError(err) {
		logext.Warnf(ctx, "[%s] auth failed,disabling source,source_id:%s,err:%s", where, src.ID, err.Error())
		_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "intercom authentication failed")
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
		return
	}
	logext.Warnf(ctx, "[%s] conversation search failed,source_id:%s,err:%+v", where, src.ID, err.Error())
	a.transientError(ctx, src, "conversation search: transient")
}

// persistSyncStats re-encrypts and writes the config blob with updated stats.
func (a *adapter) persistSyncStats(ctx context.Context, sourceID string, cfg Config, where string) {
	updater, ok := a.deps.Sources.(intercomConfigUpdater)
	if !ok {
		return
	}
	raw, err := jsonMarshal(cfg)
	if err != nil {
		logext.Warnf(ctx, "[%s] marshal config failed,source_id:%s,err:%+v", where, sourceID, err.Error())
		return
	}
	encrypted, err := a.deps.Secrets.Encrypt(raw)
	if err != nil {
		logext.Warnf(ctx, "[%s] encrypt config failed,source_id:%s,err:%+v", where, sourceID, err.Error())
		return
	}
	if err := updater.UpdateConfig(ctx, sourceID, encrypted); err != nil {
		logext.Warnf(ctx, "[%s] persist config failed,source_id:%s,err:%+v", where, sourceID, err.Error())
	}
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
	if a.failureCount != nil {
		delete(a.failureCount, slug)
	}
	a.mu.Unlock()
}

func (a *adapter) transientError(ctx context.Context, src inbound.Source, reason string) {
	_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     src.State.LastUID,
		LastError:   reason,
	})
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
	a.mu.Lock()
	if a.failureCount == nil {
		a.failureCount = map[string]int{}
	}
	a.failureCount[src.Slug]++
	a.mu.Unlock()
}

// shouldSkipBackoff returns true if the source has consecutive failures
// and the backoff interval hasn't elapsed since the last attempt.
// Backoff schedule: 0-2 failures → no skip (60s tick is enough),
// 3+ → doubling: effectively 120s, 240s, ... max 900s.
func (a *adapter) shouldSkipBackoff(slug string) bool {
	a.mu.Lock()
	failures := a.failureCount[slug]
	last, hasLast := a.lastSuccessAt[slug]
	a.mu.Unlock()
	if failures < 3 {
		return false
	}
	interval := defaultPollInterval
	for i := 2; i < failures && interval < 15*time.Minute; i++ {
		interval *= 2
	}
	if interval > 15*time.Minute {
		interval = 15 * time.Minute
	}
	if !hasLast {
		return false
	}
	return nowFn().Sub(last) < interval
}

// wipeBytes zeros credential bytes after use.
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
