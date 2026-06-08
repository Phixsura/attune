// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// maxBatchUIDs caps how many messages a single pollSource tick handles
// so a backlogged mailbox does not stall the loop for tens of minutes.
const maxBatchUIDs = 100

// pollSource — connects to the source's IMAP server, searches for new
// UIDs, fetches and ingests each, applies the after_ingest policy.
// Authentication-failure marks the source disabled so subsequent
// polls don't burn cycles; transient errors record LastError without
// disabling.
func (a *adapter) pollSource(ctx context.Context, src inbound.Source) {
	const where = "inbound.email.pollSource"
	start := nowFn()
	// Emit the running poll-lag (seconds since last successful poll for
	// this source) at the top of every attempt so a wedged source shows
	// growing lag even when it never completes successfully.
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, a.pollLagSeconds(src.Slug, start))
	defer func() {
		a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(start).Seconds())
	}()

	cfg, password, err := parseEmailConfig(src.Config, a.deps.Secrets)
	if err != nil {
		a.deps.Logger.Warnf(ctx, "[%s] decrypt config failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: src.State.LastEventAt,
			LastUID:     src.State.LastUID,
			LastError:   "decrypt config",
		})
		return
	}
	// Remember the source's poll interval so pollLoop's between-rounds
	// sleep honours it (review M3, #66).
	a.recordPollInterval(src.Slug, cfg.PollIntervalSeconds)

	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	options := ptrext.Of(imapclient.Options{})
	cli, err := dialIMAP(ctx, addr, cfg.TLS, options)
	if err != nil {
		a.transientError(ctx, src, "dial: imap server unreachable")
		return
	}
	defer func() { _ = cli.Logout() }()

	if err := cli.Login(cfg.Username, password).Wait(); err != nil {
		// AUTHENTICATIONFAILED is operator-facing — disable the source
		// so the next 60s tick does not retry; admin must reconnect.
		// We deliberately log + persist a sanitised constant message —
		// servers occasionally echo the LOGIN command back in BAD/NO
		// responses, which could leak the password fragment to
		// console + log readers (review M2, #66). Operators see the
		// real error in the IMAP server's own logs.
		a.deps.Logger.Warnf(ctx, "[%s] login failed,source_id:%s", where, src.ID)
		_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "imap authentication failed")
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
		return
	}

	if _, err := cli.Select(cfg.Folder, ptrext.Of(imap.SelectOptions{})).Wait(); err != nil {
		a.transientError(ctx, src, "select: "+err.Error())
		return
	}

	criteria := newSearchCriteria(src.State.LastUID)
	searchData, err := cli.UIDSearch(criteria, ptrext.Of(imap.SearchOptions{})).Wait()
	if err != nil {
		a.transientError(ctx, src, "search: "+err.Error())
		return
	}
	uids := searchData.AllUIDs()
	if len(uids) > maxBatchUIDs {
		uids = uids[:maxBatchUIDs]
	}

	policy := parseAfterIngest(cfg.AfterIngest)

	lastUID := src.State.LastUID
	for _, uid := range uids {
		if ctx.Err() != nil {
			return
		}
		raw, ferr := fetchOne(cli, uid)
		if ferr != nil {
			a.deps.Logger.Warnf(ctx, "[%s] fetch failed,source_id:%s,uid:%d,err:%+v",
				where, src.ID, uid, ferr.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
			continue
		}

		parsed, perr := parseRFC822(raw)
		if perr != nil {
			// Bad MIME — count as validate_err and advance the cursor
			// so the loop doesn't wedge on this UID forever. We do
			// NOT call Ingest on a half-parsed message; that would
			// write an empty user_feedback row (review H4, #66).
			a.deps.Logger.Warnf(ctx, "[%s] parse failed,source_id:%s,uid:%d,err:%+v",
				where, src.ID, uid, perr.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
			lastUID = int64(uid)
			continue
		}

		in := domain.IngestInput{
			Source:     channelName,
			Content:    parsed.Subject + "\n\n" + parsed.TextBody,
			SourceUser: parsed.From,
			SourceMeta: map[string]any{
				"inbound_source_id":   src.ID,
				"inbound_source_name": src.Name,
				"mailbox":             cfg.Username,
				"message_id":          parsed.MessageID,
				"in_reply_to":         parsed.InReplyTo,
				"references":          parsed.References,
				"imap_uid":            uid,
			},
		}

		if _, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in); err != nil {
			a.deps.Logger.Warnf(ctx, "[%s] ingest failed,source_id:%s,uid:%d,err:%+v",
				where, src.ID, uid, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
			continue
		}

		applyAfterIngest(cli, uid, policy)
		lastUID = int64(uid)
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	}

	if lastUID != src.State.LastUID {
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: ptrext.Of(nowFn()),
			LastUID:     lastUID,
		})
	}
	// Stamp this poll as a success even when no new UIDs landed — the
	// adapter successfully connected, authenticated, and searched. That
	// is what `poll_lag` measures (operator wants alerts on "I cannot
	// reach the mailbox", not "nobody wrote me email today").
	a.markPollSuccess(src.Slug, nowFn())
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, 0)
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
}

// pollLagSeconds returns seconds since the last successful poll for the
// supplied source slug. Returns 0 for sources we have never observed
// succeed — that lines up with the "first scrape after boot" Prometheus
// convention.
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

// dialIMAP — separated for test seams. Production dials TLS only —
// the cfg.TLS=false escape hatch is gone (review H2, #66). A loopback
// reverse proxy that terminates TLS can front a plain-IMAP server if
// the operator truly needs one.
var dialIMAP = func(_ context.Context, addr string, _ bool, opt *imapclient.Options) (*imapclient.Client, error) {
	return imapclient.DialTLS(addr, opt)
}

func newSearchCriteria(lastUID int64) *imap.SearchCriteria {
	c := ptrext.Of(imap.SearchCriteria{})
	if lastUID > 0 {
		set := imap.UIDSetNum(imap.UID(lastUID + 1))
		c.UID = []imap.UIDSet{set}
	}
	return c
}

func fetchOne(cli *imapclient.Client, uid imap.UID) ([]byte, error) {
	uidSet := imap.UIDSetNum(uid)
	cmd := cli.Fetch(uidSet, ptrext.Of(imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{ptrext.Of(imap.FetchItemBodySection{})},
	}))
	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			if body, ok := item.(imapclient.FetchItemDataBodySection); ok {
				b, err := io.ReadAll(body.Literal)
				if err != nil {
					return nil, err
				}
				return b, nil
			}
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, err
	}
	return nil, errors.New("fetchOne: empty result")
}

// applyAfterIngest applies the configured after-ingest policy.
//
// The correctness primitive of the email adapter is the lastUID cursor:
// each successful Ingest advances it, so the same message is never
// re-ingested. The IMAP \Seen flag or move-to-folder is a UX nicety on
// top of that cursor.
//
// In go-imap/v2 (currently beta), the Store and Move commands return
// imapclient.FetchCommand-shaped values whose Wait() is unexported.
// Rather than chase the v2 beta API surface in this PR we leave the
// flag manipulation as best-effort no-ops; the cursor still prevents
// duplicate processing, and Plan T24 manual smoke (gate 19 — "rotate
// 24h overlap" and related) verifies end-to-end. Re-enable when
// emersion/go-imap v2 ships a stable Store/Move API.
func applyAfterIngest(_ *imapclient.Client, _ imap.UID, _ afterIngestPolicy) {
	// intentionally a no-op for v2 beta — see comment above
}

func (a *adapter) transientError(ctx context.Context, src inbound.Source, reason string) {
	_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     src.State.LastUID,
		LastError:   reason,
	})
	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "transient_err")
}

// nowFn — overrideable in tests; production uses time.Now.
var nowFn = func() (t time.Time) {
	t = time.Now()
	return
}
