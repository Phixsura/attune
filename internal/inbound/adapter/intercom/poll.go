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
		a.markPollAttempt(src.Slug, nowFn())
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
		logext.Warnf(ctx, "[%s] parse config failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: src.State.LastEventAt,
			LastUID:     src.State.LastUID,
			// The parse error distinguishes decrypt / version / region
			// failures — don't collapse them for the operator.
			LastError: "config: " + err.Error(),
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
	a.pruneProcessedKeys(src.Slug, newUID)
	if sr.pollError != "" {
		// Per-item transient failures count toward backoff — a
		// poison-pill item retried at full tick rate forever is worse
		// than a slower cadence.
		a.markPollFailure(src.Slug)
	} else {
		a.markPollSuccess(src.Slug, nowFn())
	}
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
// conversations that were actually processed (ingested, skipped as
// invalid, or filtered); any early stop (budget, rate floor, transient
// failure, ctx) also steps the watermark back one second so unprocessed
// conversations sharing the boundary second are re-covered next tick
// (updated_at is second-precision; re-covered snapshots dedup by
// idempotency key).
//
// Pagination resumes across ticks via cfg.SyncCursor: without it, a UTC
// day whose already-covered conversations fill maxPagesPerTick pages
// would be re-listed from the day floor every tick and the unprocessed
// tail would be unreachable forever.
func (a *adapter) syncPages(ctx context.Context, src inbound.Source, cfg Config, client apiClient, where string) syncResult {
	watermark := src.State.LastUID
	if watermark == 0 {
		watermark = a.seedWatermark(cfg)
	}
	tickStart := nowFn().Unix()
	// Day-floor the query start and day-pad the query end (the same
	// defense Airbyte's production connector uses on both bounds):
	// Intercom search timestamp comparisons are date-indexed, so a
	// second-precise "now" upper bound could exclude everything updated
	// today. The real window [watermark+1, tickStart) is enforced by
	// the client-side second-precision filters below.
	queryStart := (watermark / daySeconds) * daySeconds
	queryEnd := (tickStart/daySeconds + 2) * daySeconds

	st := pageState{
		res:           syncResult{watermark: watermark},
		baseWatermark: watermark,
		tickStart:     tickStart,
		detailBudget:  effectiveDetailBudget(cfg),
		companyCache:  map[string]intercomCompany{},
	}
	cursor := resumeCursor(cfg, queryStart, queryEnd)
	endOfResults := false

	for pageNum := 0; pageNum < maxPagesPerTick && !endOfResults; pageNum++ {
		if ctx.Err() != nil {
			break
		}
		fetchCursor := cursor
		page, err := client.SearchConversations(ctx, queryStart, queryEnd, fetchCursor)
		if err != nil {
			if fetchCursor != "" {
				// A stale/rejected continuation cursor must not wedge the
				// source — drop it so the next tick starts a fresh window.
				cfg.setSyncCursor("", 0, 0)
				a.persistSyncStats(ctx, src.ID, cfg, where)
			}
			a.handleSearchFailure(ctx, src, where, err)
			st.res.failed = true
			return st.res
		}

		var stop bool
		st, stop = a.processConversationPage(ctx, client, src, cfg, page, st, where)
		if stop {
			// Mid-page stop: keep the cursor that fetched THIS page so
			// the next tick re-fetches it (processed head dedups).
			cfg.setSyncCursor(fetchCursor, queryStart, queryEnd)
			break
		}

		if page.StartingAfter == "" {
			// End of results — backfill complete for this window.
			cfg.SyncStats.BackfillDone = true
			endOfResults = true
			cfg.setSyncCursor("", 0, 0)
		} else {
			// Page fully processed — the continuation point advances even
			// when the watermark cannot (a page full of already-covered
			// items), so a saturated day drains at 10 pages per tick.
			cfg.setSyncCursor(page.StartingAfter, queryStart, queryEnd)
		}
		logext.Infof(ctx, "[%s] progress,source_id:%s,page:%d,conversations_synced:%d,end_of_results:%t",
			where, src.ID, pageNum+1, st.totalSynced, endOfResults)
		// Proactive self-throttle: stop the tick before Intercom starts
		// returning 429s. The cursor + watermark cover the remainder next
		// tick.
		if budget := client.RateBudget(); !endOfResults && budget >= 0 && budget < rateBudgetFloor {
			logext.Infof(ctx, "[%s] rate budget low,stopping tick,source_id:%s,remaining:%d", where, src.ID, budget)
			break
		}
		cursor = page.StartingAfter
	}
	if !endOfResults && st.res.watermark > st.baseWatermark {
		// Early stop mid-window: re-cover the boundary second (see the
		// function comment) at the cost of one deduped re-fetch.
		st.res.watermark--
	}
	// Persist cursor + stats on every non-failed exit — budget-exhausted
	// stops are the common case during backfill and must not drop their
	// counts. Failed ticks don't persist the watermark, so persisting
	// their counts would double-count the re-covered items next tick.
	cfg.SyncStats.ConversationsSynced += int64(st.totalSynced)
	a.persistSyncStats(ctx, src.ID, cfg, where)
	return st.res
}

// resumeCursor returns the persisted continuation cursor when it was
// minted against this exact search window — a cursor is only valid for
// the query that produced it.
func resumeCursor(cfg Config, queryStart, queryEnd int64) string {
	if cfg.SyncCursor != "" && cfg.SyncWindowStart == queryStart && cfg.SyncWindowEnd == queryEnd {
		return cfg.SyncCursor
	}
	return ""
}

// pageState threads mutable sync progress through page processing.
type pageState struct {
	res syncResult
	// baseWatermark is the effective watermark at tick start (seeded on
	// first sync) — the client-side second-precision filter boundary.
	baseWatermark int64
	// tickStart is the exclusive client-side upper bound: items updated
	// at or after it land next tick (the API window is day-padded).
	tickStart     int64
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
// exhausted or a transient per-conversation failure that must be
// retried next tick).
func (a *adapter) processConversationPage(
	ctx context.Context,
	client apiClient,
	src inbound.Source,
	cfg Config,
	page conversationPage,
	st pageState,
	where string,
) (pageState, bool) {
	prevUpdatedAt := int64(0)
	for i := range page.Conversations {
		if ctx.Err() != nil {
			return st, true
		}
		summary := page.Conversations[i]
		// Defense for the undocumented ascending sort (see the client's
		// SearchConversations): if the API ever stops honoring it, an
		// early stop would persist a too-high watermark and silently skip
		// everything older. A visible stall beats silent data loss.
		if summary.UpdatedAt < prevUpdatedAt {
			logext.Errorf(ctx, "[%s] search results out of order,stopping tick,source_id:%s,conversation_id:%s,updated_at:%d,prev:%d",
				where, src.ID, summary.ID, summary.UpdatedAt, prevUpdatedAt)
			st.res.watermark = st.baseWatermark
			st.res.pollError = "conversation search: unexpected result order"
			return st, true
		}
		prevUpdatedAt = summary.UpdatedAt
		if action := a.classifyConversation(src.Slug, summary, cfg, st); action == convSkip {
			continue
		} else if action == convAdvance {
			st.advanceWatermark(summary.UpdatedAt)
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

		outcome := a.fetchAndIngest(ctx, client, src, cfg, summary, st.companyCache, st.admins, where)
		if outcome == outcomeRetry {
			// Transient failure: stop here without advancing the
			// watermark past this conversation so it is re-covered next
			// tick, and surface the degradation on the source state.
			st.res.pollError = "conversation sync: transient"
			return st, true
		}
		if outcome == outcomeIngested {
			st.totalSynced++
		}
		a.rememberProcessed(src.Slug, summary)
		st.advanceWatermark(summary.UpdatedAt)
	}
	return st, false
}

// convAction classifies a search summary before the detail fetch.
type convAction int

const (
	// convFetch — qualifying conversation: fetch details and ingest.
	convFetch convAction = iota
	// convSkip — outside the client-side window: no watermark movement.
	convSkip
	// convAdvance — covered without work (filtered out, or an
	// already-processed boundary replay): advance the watermark for free.
	convAdvance
)

// classifyConversation applies the client-side window, the configured
// filters, and the processed-set to one search summary.
func (a *adapter) classifyConversation(slug string, summary conversation, cfg Config, st pageState) convAction {
	// Client-side second-precision window: skip items at or below the
	// watermark and items updated at/after tick start (they land next
	// tick — the day-padded API window over-fetches by design).
	if summary.UpdatedAt <= st.baseWatermark || summary.UpdatedAt >= st.tickStart {
		return convSkip
	}
	if !matchesFilter(summary, cfg) {
		return convAdvance
	}
	if a.alreadyProcessed(slug, summary) {
		// Boundary-second replay of an already-processed snapshot —
		// no detail fetch, no budget, no counters.
		return convAdvance
	}
	return convFetch
}

// advanceWatermark moves the tick watermark past a covered conversation.
func (st *pageState) advanceWatermark(updatedAt int64) {
	if updatedAt > st.res.watermark {
		st.res.watermark = updatedAt
	}
}

// snapshotKey identifies one conversation snapshot in the per-source
// processed-set (memory only, not the ingest idempotency key).
func snapshotKey(summary conversation) string {
	return summary.ID + "@" + strconv.FormatInt(summary.UpdatedAt, 10)
}

// alreadyProcessed reports whether this exact conversation snapshot was
// fully handled in an earlier tick (boundary-second re-cover).
func (a *adapter) alreadyProcessed(slug string, summary conversation) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen, ok := a.processedKeys[slug]
	if !ok {
		return false
	}
	_, done := seen[snapshotKey(summary)]
	return done
}

// rememberProcessed marks a snapshot as fully handled so boundary-second
// re-covers skip it without spending API budget.
func (a *adapter) rememberProcessed(slug string, summary conversation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.processedKeys == nil {
		a.processedKeys = map[string]map[string]int64{}
	}
	if a.processedKeys[slug] == nil {
		a.processedKeys[slug] = map[string]int64{}
	}
	a.processedKeys[slug][snapshotKey(summary)] = summary.UpdatedAt
}

// pruneProcessedKeys drops remembered snapshots at or below the
// persisted watermark — the `<= baseWatermark` filter covers those, so
// the memory set only ever holds the boundary second.
func (a *adapter) pruneProcessedKeys(slug string, watermark int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, updatedAt := range a.processedKeys[slug] {
		if updatedAt <= watermark {
			delete(a.processedKeys[slug], key)
		}
	}
}

// ingestOutcome classifies one conversation's processing result.
type ingestOutcome int

const (
	// outcomeIngested — a new feedback row was created.
	outcomeIngested ingestOutcome = iota
	// outcomeDuplicate — replay of an already-ingested snapshot; the
	// conversation counts as processed but not as newly synced.
	outcomeDuplicate
	// outcomeSkipped — deterministic per-conversation condition (empty
	// content, validation reject): retrying can never succeed, so the
	// watermark advances past it. NEVER map a deterministic failure to
	// outcomeRetry — that wedges the source on a poison-pill item.
	outcomeSkipped
	// outcomeRetry — transient failure; the tick must stop before this
	// conversation so the watermark never advances past an item that
	// was neither ingested nor filtered.
	outcomeRetry
)

// fetchAndIngest pulls the full thread for one conversation and ingests
// it. Permanent per-conversation conditions (deleted/restricted/oversized
// — the zendesk #229 lesson §4b) degrade to the summary shape so they
// never block the sync; deterministic ingest rejects (validation) skip
// the conversation; only genuinely transient failures return
// outcomeRetry so the full thread is re-fetched next tick instead of
// permanently ingesting a degraded snapshot under its idempotency key.
func (a *adapter) fetchAndIngest(ctx context.Context, client apiClient, src inbound.Source, cfg Config, summary conversation, companyCache map[string]intercomCompany, admins map[int64]intercomAdmin, where string) ingestOutcome {
	conv, err := client.GetConversation(ctx, summary.ID)
	if err != nil {
		if isTransientDetailError(err) {
			logext.Warnf(ctx, "[%s] detail fetch failed,will retry next tick,source_id:%s,conversation_id:%s,err:%s",
				where, src.ID, summary.ID, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
			return outcomeRetry
		}
		// Permanent per-conversation condition: retrying can never
		// succeed, so fall back to the summary shape (no parts) — the
		// seed message still carries signal.
		logext.Warnf(ctx, "[%s] detail fetch failed,degrading to summary,source_id:%s,conversation_id:%s,err:%s",
			where, src.ID, summary.ID, err.Error())
		if isPermanentIntercomError(err) {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "detail_auth_err")
		} else {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		}
		conv = summary
	}

	contacts := a.resolveContacts(ctx, client, conv, where)
	conv.Company = a.resolveCompany(ctx, client, conv.Company, companyCache, where)
	in := buildIngestInput(src.ID, src.Name, cfg.WorkspaceID, cfg.Region, conv, contacts, admins)
	if in.Content == "" {
		// A conversation can legitimately carry no ingestable text (empty
		// seed + only internal notes/state changes). Deterministic — skip
		// and advance; retrying would wedge the source forever.
		logext.Infof(ctx, "[%s] empty conversation,skipping,source_id:%s,conversation_id:%s", where, src.ID, conv.ID)
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		return outcomeSkipped
	}
	if _, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in); err != nil {
		if isDuplicateError(err) {
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
			return outcomeDuplicate
		}
		if isDeterministicIngestError(err) {
			// Validation rejects reproduce identically on every retry —
			// skip the conversation instead of wedging the watermark.
			logext.Warnf(ctx, "[%s] ingest rejected,skipping,source_id:%s,conversation_id:%s,err:%+v",
				where, src.ID, conv.ID, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
			return outcomeSkipped
		}
		logext.Warnf(ctx, "[%s] ingest failed,will retry next tick,source_id:%s,conversation_id:%s,err:%+v",
			where, src.ID, conv.ID, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return outcomeRetry
	}
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	return outcomeIngested
}

// isTransientDetailError reports whether a GetConversation failure is
// worth retrying next tick (rate limit, server error, network) as
// opposed to a permanent per-conversation condition (deleted,
// restricted, plan-gated, oversized/undecodable response) that degrades
// to the summary shape.
func isTransientDetailError(err error) bool {
	var rle rateLimitError
	if errors.As(err, &rle) {
		return true
	}
	var de decodeError
	if errors.As(err, &de) {
		// Deterministic for the same response (incl. size-cap
		// truncation) — retrying can never succeed.
		return false
	}
	var ae apiError
	if errors.As(err, &ae) {
		return ae.Status >= 500
	}
	// Non-API errors (network, timeout) are transient by nature.
	return true
}

// isDeterministicIngestError matches ingest rejections that reproduce
// identically on every attempt — validation failures, never infra
// errors. These must skip the conversation, not wedge the watermark.
func isDeterministicIngestError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"content is required",
		"content too long",
		"content contains a null byte",
		"invalid source",
		"invalid idempotency key",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
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

// markPollAttempt records the attempt time — the backoff reference point
// (measuring from last success would disable backoff after one interval
// and never engage it for never-succeeded sources).
func (a *adapter) markPollAttempt(slug string, t time.Time) {
	a.mu.Lock()
	if a.lastAttemptAt == nil {
		a.lastAttemptAt = map[string]time.Time{}
	}
	a.lastAttemptAt[slug] = t
	a.mu.Unlock()
}

// markPollFailure bumps the consecutive-failure counter without touching
// source state (used for per-item transient degradation, where the tick
// itself persisted normally).
func (a *adapter) markPollFailure(slug string) {
	a.mu.Lock()
	if a.failureCount == nil {
		a.failureCount = map[string]int{}
	}
	a.failureCount[slug]++
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
// Backoff schedule: 0-2 failures → no skip (60s tick is enough), 3+ →
// doubling: 120s, 240s, ... capped at 900s.
func (a *adapter) shouldSkipBackoff(slug string) bool {
	a.mu.Lock()
	failures := a.failureCount[slug]
	last, hasLast := a.lastAttemptAt[slug]
	a.mu.Unlock()
	if failures < 3 || !hasLast {
		return false
	}
	interval := defaultPollInterval
	for i := 2; i < failures && interval < 15*time.Minute; i++ {
		interval *= 2
	}
	if interval > 15*time.Minute {
		interval = 15 * time.Minute
	}
	return nowFn().Sub(last) < interval
}

// wipeBytes zeros credential bytes after use.
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
