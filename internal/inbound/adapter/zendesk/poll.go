// SPDX-License-Identifier: Apache-2.0

package zendesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	pollSourceTimeout = 30 * time.Second

	// defaultMaxCommentFetches caps per-ticket comment API calls per poll
	// tick. Overridable via Config.MaxCommentFetches.
	defaultMaxCommentFetches = 50

	// maxBulkResolve is the Zendesk show_many batch cap.
	maxBulkResolve = 100

	// maxPagesPerTick caps continuous pagination within a single poll
	// tick to prevent unbounded runtime. At ~1000 tickets/page this
	// processes up to 10,000 tickets per 60s tick during backfill.
	maxPagesPerTick = 10
)

// nowFn is overrideable in tests; production uses time.Now.
var nowFn = time.Now

// jsonMarshal is a test seam for config persistence.
var jsonMarshal = json.Marshal

type zendeskConfigUpdater interface {
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
		logext.Warnf(ctx, "[inbound.zendesk.pollLoop] list sources failed,err:%+v", err.Error())
		return
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return
		}
		if !src.Enabled {
			continue
		}
		// In-memory state is keyed by source ID — slugs are only unique
		// per tenant and this list spans all tenants.
		if a.shouldSkipBackoff(src.ID) {
			continue
		}
		a.markPollAttempt(src.ID, nowFn())
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
		logext.Warnf(ctx, "[inbound.zendesk.syncNow] get source failed,source_id:%s,err:%+v", sourceID, err.Error())
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
	const where = "inbound.zendesk.pollSource"
	start := nowFn()
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, a.pollLagSeconds(src.ID, start))
	defer func() {
		a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(start).Seconds())
	}()

	cfg, cred, err := parseConfig(src.Config, a.deps.Secrets)
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
	defer wipeCred(cred)

	apiBase := baseURL(cfg.Subdomain)
	if err := validateHost(apiBase); err != nil {
		logext.Warnf(ctx, "[%s] host validation failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		return
	}
	client := a.newClient(apiBase, cred)

	sr := a.syncPages(ctx, src, cfg, cred, client, apiBase, where)
	if sr.failed {
		return // error already handled inside syncPages
	}

	// Update source state.
	newUID := src.State.LastUID
	if sr.maxGenTS > newUID {
		newUID = sr.maxGenTS
	}
	a.persistPollResult(ctx, src, newUID, nowFn(), sr.pollError)
	if sr.pollError != "" {
		// Per-item transient failures count toward backoff — a
		// poison-pill ticket retried at full tick rate forever is worse
		// than a slower cadence. The lag gauge deliberately keeps its
		// value: a degraded source must not report zero lag.
		a.markPollFailure(src.ID)
	} else {
		a.markPollSuccess(src.ID, nowFn())
		a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, 0)
	}
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
}

// syncResult aggregates state across all pages in a single poll tick.
type syncResult struct {
	maxGenTS  int64
	pollError string
	failed    bool // true if export fetch failed (error already handled)
}

// syncPages fetches up to maxPagesPerTick pages of tickets and processes
// each. Returns aggregated state for cursor/state persistence.
func (a *adapter) syncPages(ctx context.Context, src inbound.Source, cfg Config, cred credential, client apiClient, apiBase, where string) syncResult {
	cursor := cfg.SyncCursor
	var startTime int64
	if cursor == "" {
		startTime = a.seedStartTime(cfg)
	}

	var res syncResult
	var totalSynced int
	for pageNum := 0; pageNum < maxPagesPerTick; pageNum++ {
		if ctx.Err() != nil {
			break
		}
		page, err := a.fetchTicketPage(ctx, src, cfg, &cred, &client, apiBase, cursor, startTime, where) // ptrext:allow out-param-refresh
		if err != nil {
			a.handleExportFailure(ctx, src, where, err)
			res.failed = true
			return res
		}

		result := a.processTicketPage(ctx, client, src, cfg, page, where)
		if result.disabled {
			return res
		}
		if result.pollError != "" {
			res.pollError = result.pollError
		}
		if !result.completed {
			// Mid-page stop (comment budget, transient comment/ingest
			// failure): the export cursor must NOT advance past
			// unprocessed tickets — this page is re-fetched next tick
			// and the processed head skips for free. Advancing the
			// cursor here would permanently lose the remainder (the
			// incremental export never re-lists a passed snapshot).
			break
		}
		if result.lastGenTS > res.maxGenTS {
			res.maxGenTS = result.lastGenTS
		}
		totalSynced += len(page.Tickets)

		// 3.2: Update sync stats — only for completed pages, so a
		// re-fetched partial page is never double-counted.
		cfg.SyncStats.TicketsSynced += int64(len(page.Tickets))
		if result.lastTicketID > 0 {
			cfg.SyncStats.LastTicketID = result.lastTicketID
		}

		if page.AfterCursor != "" {
			cfg.SyncCursor = page.AfterCursor
			cursor = page.AfterCursor
			startTime = 0
			// The cursor moved past this page — its snapshots are never
			// re-listed, so the memory set can drop them.
			a.clearProcessedTickets(src.ID)
		}
		if page.EndOfStream {
			cfg.SyncStats.BackfillDone = true
		}
		if err := a.persistConfig(ctx, src.ID, cred, cfg); err != nil {
			logext.Warnf(ctx, "[%s] persist config failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		}
		logext.Infof(ctx, "[%s] progress,source_id:%s,page:%d,tickets_synced:%d,cursor:%s,end_of_stream:%v",
			where, src.ID, pageNum+1, totalSynced, cursor, page.EndOfStream)
		if page.EndOfStream {
			break
		}
	}
	return res
}

// fetchTicketPage fetches one page, attempting OAuth refresh on auth failure.
func (a *adapter) fetchTicketPage(ctx context.Context, src inbound.Source, cfg Config, cred *credential, client *apiClient, apiBase, cursor string, startTime int64, where string) (ticketPage, error) { // ptrext:allow out-param-refresh
	page, err := (*client).IncrementalTickets(ctx, cursor, startTime)             // ptrext:allow out-param-deref
	if err != nil && isPermanentZendeskError(err) && cred.Mode == AuthModeOAuth { // ptrext:allow out-param-deref
		newCred, refreshed := a.tryOAuthRefresh(ctx, src, cfg, *cred, *client, where) // ptrext:allow out-param-deref
		if refreshed {
			*cred = newCred                                             // ptrext:allow out-param-write
			*client = a.newClient(apiBase, newCred)                     // ptrext:allow out-param-write
			return (*client).IncrementalTickets(ctx, cursor, startTime) // ptrext:allow out-param-deref
		}
	}
	return page, err
}

// pageResult captures state from a processed ticket page.
type pageResult struct {
	lastGenTS    int64
	lastTicketID int64
	pollError    string
	disabled     bool
	// completed is true when every qualifying ticket on the page was
	// handled — the export cursor may advance. A mid-page stop (comment
	// budget, transient failure) leaves it false so the page is
	// re-fetched next tick.
	completed bool
}

// processTicketPage resolves metadata and ingests each non-deleted ticket.
// Stops (completed=false) at the first ticket it cannot fully handle
// this tick, so the cursor never advances past unprocessed tickets.
func (a *adapter) processTicketPage(ctx context.Context, client apiClient, src inbound.Source, cfg Config, page ticketPage, where string) pageResult {
	// Collect unique user/org IDs for batch resolution.
	userIDs := make(map[int64]struct{})
	orgIDs := make(map[int64]struct{})
	for _, t := range page.Tickets {
		if t.Status == "deleted" {
			continue
		}
		if t.RequesterID > 0 {
			userIDs[t.RequesterID] = struct{}{}
		}
		if t.OrganizationID > 0 {
			orgIDs[t.OrganizationID] = struct{}{}
		}
	}

	users := a.resolveUsers(ctx, client, userIDs, where)
	orgs := a.resolveOrganizations(ctx, client, orgIDs, where)

	var res pageResult
	commentBudget := effectiveCommentBudget(cfg)
	var commentFetches int
	for _, t := range page.Tickets {
		if ctx.Err() != nil {
			return res
		}
		if t.Status == "deleted" {
			continue
		}
		// 3.1: Ticket filtering.
		if !matchesFilter(t, cfg.Filter) {
			continue
		}
		if a.alreadyProcessedTicket(src.ID, t) {
			// Re-fetched partial page: this snapshot was fully handled
			// on an earlier pass — skip for free.
			if t.GeneratedTimestamp > res.lastGenTS {
				res.lastGenTS = t.GeneratedTimestamp
			}
			continue
		}
		if commentFetches >= commentBudget {
			// Budget exhausted: stop WITHOUT advancing the cursor so the
			// remainder of this page (with full comments) lands next
			// tick — an incomplete comment-less snapshot would be locked
			// in forever under its idempotency key.
			logext.Infof(ctx, "[%s] comment budget exhausted,source_id:%s,budget:%d", where, src.ID, commentBudget)
			return res
		}
		commentFetches++
		comments, ok := a.fetchComments(ctx, client, src, t.ID, where)
		if !ok {
			// Transient comment failure: retry this ticket next tick
			// instead of permanently ingesting a comment-less snapshot.
			res.pollError = "comments: transient"
			return res
		}

		in := buildIngestInput(src.ID, src.Name, cfg.Subdomain, t, comments, users, orgs)
		if !a.ingestTicket(ctx, src, t.ID, in, where) {
			// Transient infra failure (DB down): stop WITHOUT advancing
			// the cursor — continuing would let the persisted cursor
			// pass this ticket and lose it forever.
			res.pollError = "ticket sync: transient"
			return res
		}
		a.rememberProcessedTicket(src.ID, t)
		if t.GeneratedTimestamp > res.lastGenTS {
			res.lastGenTS = t.GeneratedTimestamp
		}
		res.lastTicketID = t.ID
	}
	res.completed = true
	return res
}

// ingestTicket submits one ticket. ok=false only for transient infra
// failures — duplicates and deterministic validation rejects count as
// handled (retrying them can never change the outcome).
func (a *adapter) ingestTicket(ctx context.Context, src inbound.Source, ticketID int64, in domain.IngestInput, where string) bool {
	if _, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in); err != nil {
		switch {
		case isDuplicateError(err):
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		case isDeterministicIngestError(err):
			// Validation rejects reproduce identically on every retry —
			// skip the ticket instead of wedging the cursor.
			logext.Warnf(ctx, "[%s] ingest rejected,skipping,source_id:%s,ticket_id:%d,err:%+v",
				where, src.ID, ticketID, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		default:
			logext.Warnf(ctx, "[%s] ingest failed,will retry next tick,source_id:%s,ticket_id:%d,err:%+v",
				where, src.ID, ticketID, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
			return false
		}
		return true
	}
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	return true
}

// fetchComments retrieves public comments for a single ticket. ok=false
// signals a transient failure (retry next tick); permanent per-ticket
// conditions degrade to a comment-less ingest (retrying cannot succeed).
func (a *adapter) fetchComments(ctx context.Context, client apiClient, src inbound.Source, ticketID int64, where string) ([]comment, bool) {
	comments, cerr := client.TicketComments(ctx, ticketID)
	if cerr == nil {
		return comments, true
	}
	if isPermanentZendeskError(cerr) {
		// Degrade gracefully: skip this ticket's comments but don't disable
		// the entire source. A single restricted ticket should not block all
		// other ticket syncing.
		logext.Warnf(ctx, "[%s] comments auth failed,skipping ticket,source_id:%s,ticket_id:%d,err:%s",
			where, src.ID, ticketID, cerr.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "comment_auth_err")
		return nil, true
	}
	logext.Warnf(ctx, "[%s] comments fetch failed,will retry next tick,source_id:%s,ticket_id:%d,err:%+v",
		where, src.ID, ticketID, cerr.Error())
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
	return nil, false
}

// isDeterministicIngestError matches ingest rejections that reproduce
// identically on every attempt — validation failures, never infra
// errors. These must skip the ticket, not wedge the cursor.
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

// ticketSnapshotKey identifies one ticket snapshot in the per-source
// processed-set.
func ticketSnapshotKey(t ticket) string {
	return fmt.Sprintf("%d@%d", t.ID, t.GeneratedTimestamp)
}

// alreadyProcessedTicket reports whether this exact ticket snapshot was
// fully handled on an earlier pass of a re-fetched page.
func (a *adapter) alreadyProcessedTicket(sourceID string, t ticket) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen, ok := a.processedTickets[sourceID]
	if !ok {
		return false
	}
	_, done := seen[ticketSnapshotKey(t)]
	return done
}

// rememberProcessedTicket marks a snapshot as fully handled so a
// re-fetched partial page skips it without spending comment budget.
func (a *adapter) rememberProcessedTicket(sourceID string, t ticket) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.processedTickets == nil {
		a.processedTickets = map[string]map[string]struct{}{}
	}
	if a.processedTickets[sourceID] == nil {
		a.processedTickets[sourceID] = map[string]struct{}{}
	}
	a.processedTickets[sourceID][ticketSnapshotKey(t)] = struct{}{}
}

// clearProcessedTickets drops the memory set once the cursor advances —
// the export stream never re-lists snapshots behind the cursor.
func (a *adapter) clearProcessedTickets(sourceID string) {
	a.mu.Lock()
	delete(a.processedTickets, sourceID)
	a.mu.Unlock()
}

// tryOAuthRefresh attempts to refresh an expired OAuth access token.
// Returns the updated credential and true if refresh succeeded.
func (a *adapter) tryOAuthRefresh(ctx context.Context, src inbound.Source, cfg Config, cred credential, client apiClient, where string) (credential, bool) {
	if cred.RefreshToken == "" {
		logext.Warnf(ctx, "[%s] no refresh token available,source_id:%s", where, src.ID)
		return cred, false
	}
	newTok, err := client.RefreshOAuthToken(ctx, cred.RefreshToken, cred.ClientID, cred.ClientSecret)
	if err != nil {
		logext.Warnf(ctx, "[%s] oauth refresh failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		return cred, false
	}
	newCred := credential{
		Mode:         AuthModeOAuth,
		AccessToken:  newTok.AccessToken,
		RefreshToken: newTok.RefreshToken,
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
	}
	if err := a.persistConfig(ctx, src.ID, newCred, cfg); err != nil {
		logext.Warnf(ctx, "[%s] persist refreshed token failed,source_id:%s,err:%+v", where, src.ID, err.Error())
	}
	logext.Infof(ctx, "[%s] oauth token refreshed,source_id:%s", where, src.ID)
	return newCred, true
}

// effectiveCommentBudget returns the per-tick comment fetch cap.
func effectiveCommentBudget(cfg Config) int {
	if cfg.MaxCommentFetches > 0 {
		return cfg.MaxCommentFetches
	}
	return defaultMaxCommentFetches
}

// seedStartTime determines the start_time parameter for the first sync.
func (a *adapter) seedStartTime(cfg Config) int64 {
	switch cfg.StartFrom {
	case "now":
		// Start from 5 minutes ago to catch recent tickets.
		return nowFn().Add(-5 * time.Minute).Unix()
	default:
		// Full backfill: start_time=0 fetches everything.
		return 0
	}
}

func (a *adapter) handleExportFailure(ctx context.Context, src inbound.Source, where string, err error) {
	// Rate limit: log the retry-after duration but don't disable the source.
	var rle rateLimitError
	if errors.As(err, &rle) {
		logext.Warnf(ctx, "[%s] rate limited,source_id:%s,retry_after:%s", where, src.ID, rle.RetryAfter)
		a.transientError(ctx, src, fmt.Sprintf("rate limited: retry after %s", rle.RetryAfter))
		// Sleep for the retry-after duration (capped in parseRetryAfter).
		select {
		case <-time.After(rle.RetryAfter):
		case <-ctx.Done():
		}
		return
	}
	if isPermanentZendeskError(err) {
		logext.Warnf(ctx, "[%s] auth failed,disabling source,source_id:%s,err:%s", where, src.ID, err.Error())
		_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "zendesk authentication failed")
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
		return
	}
	logext.Warnf(ctx, "[%s] incremental export failed,source_id:%s,err:%+v", where, src.ID, err.Error())
	a.transientError(ctx, src, "incremental export: transient")
}

// resolveUsers batch-resolves user IDs. On failure, returns an empty map.
func (a *adapter) resolveUsers(ctx context.Context, client apiClient, ids map[int64]struct{}, where string) map[int64]zendeskUser {
	result := make(map[int64]zendeskUser, len(ids))
	batch := make([]int64, 0, len(ids))
	for id := range ids {
		batch = append(batch, id)
	}
	for len(batch) > 0 {
		chunk := batch
		if len(chunk) > maxBulkResolve {
			chunk = batch[:maxBulkResolve]
		}
		batch = batch[len(chunk):]
		users, err := client.ShowUsers(ctx, chunk)
		if err != nil {
			logext.Warnf(ctx, "[%s] resolve users failed,err:%+v", where, err.Error())
			break
		}
		for _, u := range users {
			result[u.ID] = u
		}
	}
	return result
}

// resolveOrganizations batch-resolves organization IDs. On failure, returns an empty map.
func (a *adapter) resolveOrganizations(ctx context.Context, client apiClient, ids map[int64]struct{}, where string) map[int64]zendeskOrganization {
	result := make(map[int64]zendeskOrganization, len(ids))
	batch := make([]int64, 0, len(ids))
	for id := range ids {
		batch = append(batch, id)
	}
	for len(batch) > 0 {
		chunk := batch
		if len(chunk) > maxBulkResolve {
			chunk = batch[:maxBulkResolve]
		}
		batch = batch[len(chunk):]
		organizations, err := client.ShowOrganizations(ctx, chunk)
		if err != nil {
			logext.Warnf(ctx, "[%s] resolve organizations failed,err:%+v", where, err.Error())
			break
		}
		for _, o := range organizations {
			result[o.ID] = o
		}
	}
	return result
}

// persistConfig re-encrypts and writes the config blob.
func (a *adapter) persistConfig(ctx context.Context, sourceID string, cred credential, cfg Config) error {
	updater, ok := a.deps.Sources.(zendeskConfigUpdater)
	if !ok {
		return nil
	}

	// Re-encrypt inner credential.
	switch cred.Mode {
	case AuthModeAPIToken:
		enc, err := a.deps.Secrets.Encrypt(cred.APIToken)
		if err != nil {
			return fmt.Errorf("encrypt zendesk api_token: %w", err)
		}
		cfg.APITokenEncrypted = enc
	case AuthModeOAuth:
		tok := oauthToken{
			AccessToken:  cred.AccessToken,
			RefreshToken: cred.RefreshToken,
			ClientID:     cred.ClientID,
			ClientSecret: cred.ClientSecret,
		}
		tokJSON, err := jsonMarshal(tok)
		if err != nil {
			return fmt.Errorf("marshal zendesk oauth_token: %w", err)
		}
		enc, err := a.deps.Secrets.Encrypt(tokJSON)
		if err != nil {
			return fmt.Errorf("encrypt zendesk oauth_token: %w", err)
		}
		cfg.OAuthTokenEncrypted = enc
	}

	raw, err := jsonMarshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal zendesk config: %w", err)
	}
	encrypted, err := a.deps.Secrets.Encrypt(raw)
	if err != nil {
		return fmt.Errorf("encrypt zendesk config: %w", err)
	}
	return updater.UpdateConfig(ctx, sourceID, encrypted)
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

func (a *adapter) pollLagSeconds(sourceID string, now time.Time) float64 {
	a.mu.Lock()
	last, ok := a.lastSuccessAt[sourceID]
	a.mu.Unlock()
	if !ok {
		return 0
	}
	return now.Sub(last).Seconds()
}

func (a *adapter) markPollSuccess(sourceID string, t time.Time) {
	a.mu.Lock()
	if a.lastSuccessAt == nil {
		a.lastSuccessAt = map[string]time.Time{}
	}
	a.lastSuccessAt[sourceID] = t
	// Reset backoff on success.
	if a.failureCount != nil {
		delete(a.failureCount, sourceID)
	}
	a.mu.Unlock()
}

// markPollAttempt records the attempt time — the backoff reference point
// (measuring from last success would disable backoff after one interval
// and never engage it for never-succeeded sources).
func (a *adapter) markPollAttempt(sourceID string, t time.Time) {
	a.mu.Lock()
	if a.lastAttemptAt == nil {
		a.lastAttemptAt = map[string]time.Time{}
	}
	a.lastAttemptAt[sourceID] = t
	a.mu.Unlock()
}

// markPollFailure bumps the consecutive-failure counter without touching
// source state (used for per-item transient degradation, where the tick
// itself persisted normally).
func (a *adapter) markPollFailure(sourceID string) {
	a.mu.Lock()
	if a.failureCount == nil {
		a.failureCount = map[string]int{}
	}
	a.failureCount[sourceID]++
	a.mu.Unlock()
}

func (a *adapter) transientError(ctx context.Context, src inbound.Source, reason string) {
	_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     src.State.LastUID,
		LastError:   reason,
	})
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
	a.markPollFailure(src.ID)
}

// shouldSkipBackoff returns true if the source has consecutive failures
// and the backoff interval hasn't elapsed since the last attempt.
// Backoff schedule: 0-2 failures → no skip (60s tick is enough), 3+ →
// doubling: 120s, 240s, ... capped at 900s.
func (a *adapter) shouldSkipBackoff(sourceID string) bool {
	a.mu.Lock()
	failures := a.failureCount[sourceID]
	last, hasLast := a.lastAttemptAt[sourceID]
	a.mu.Unlock()
	if failures < 3 || !hasLast {
		return false
	}
	// Exponential interval: 60 * 2^(failures-2), capped at 900s.
	interval := defaultPollInterval
	for i := 2; i < failures && interval < 15*time.Minute; i++ {
		interval *= 2
	}
	if interval > 15*time.Minute {
		interval = 15 * time.Minute
	}
	return nowFn().Sub(last) < interval
}

// wipeCred zeros credential bytes after use.
func wipeCred(c credential) {
	wipeBytes(c.APIToken)
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
