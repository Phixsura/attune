// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// maxBatchUIDs caps how many messages a single pollSource tick handles
// so a backlogged mailbox does not stall the loop for tens of minutes.
const maxBatchUIDs = 100

// maxMessageBytes caps a single RFC822 message fetch. With
// maxBatchUIDs (100) this bounds peak per-tick memory at ~800 MiB even
// against a hostile / malformed IMAP server returning enormous bodies
// (#66 review H-2). Over-size messages are dropped with `validate_err`
// — the UID cursor still advances so a single bad message doesn't
// wedge the source.
const maxMessageBytes = 8 * 1024 * 1024

// errMessageTooLarge — sentinel returned by fetchOne when a message
// exceeds maxMessageBytes. Maps to validate_err at the caller.
var errMessageTooLarge = errors.New("email: message exceeds size cap")

// wipe overwrites b with zeros. Used to scrub the decrypted IMAP
// password from the heap after LOGIN (#66 review M-4). Best-effort —
// the conversion `string(b)` at the imapclient call site materialises
// a copy that we can't reach, but zeroing our own slice shortens dwell
// time and the compiler must respect the loop body.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

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
		logext.Warnf(ctx, "[%s] decrypt config failed,source_id:%s,err:%+v", where, src.ID, err.Error())
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: src.State.LastEventAt,
			LastUID:     src.State.LastUID,
			LastError:   "decrypt config",
		})
		return
	}
	defer wipe(password)

	if err := ValidateOutboundHost(cfg.Host); err != nil {
		logext.Warnf(ctx, "[%s] blocked host,source_id:%s,host:%s,err:%s",
			where, src.ID, cfg.Host, err.Error())
		_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "imap host blocked")
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
		return
	}
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	// Enforce the SSRF guard at dial time as well as the ValidateOutboundHost
	// pre-check: this is rebinding-proof (it sees the resolved IP) and closes the
	// pre-check's fail-open-on-DNS-error path. Loopback + RFC1918 stay allowed for
	// co-located on-prem IMAP, matching ValidateOutboundHost; metadata/link-local
	// are always refused.
	options := ptrext.Of(imapclient.Options{
		Dialer: nethardening.Policy{AllowLoopback: true, AllowPrivate: true}.Dialer(),
	})
	cli, err := dialIMAP(ctx, addr, options)
	if err != nil {
		a.transientError(ctx, src, "dial: imap server unreachable")
		return
	}
	defer func() { _ = cli.Logout() }()

	if err := cli.Login(cfg.Username, string(password)).Wait(); err != nil {
		// Only **AUTHENTICATIONFAILED** disables the source — that's the
		// operator-actionable case (wrong creds, account locked out,
		// app-password revoked). Network timeouts / TLS resets /
		// server-side BAD or NO responses without a specific code are
		// transient and should NOT auto-disable: a single bad TCP
		// round-trip would otherwise flip a healthy source off and
		// require manual operator re-enable (#66 review H-1).
		//
		// We log a sanitised constant message because servers
		// occasionally echo the LOGIN command back in BAD/NO responses,
		// which could leak the password fragment to console + log
		// readers (review M2, #66). Operators see the real error in
		// the IMAP server's own logs.
		var imapErr *imap.Error
		if errors.As(err, &imapErr) && imapErr.Code == imap.ResponseCodeAuthenticationFailed {
			logext.Warnf(ctx, "[%s] login auth failed (disabling),source_id:%s", where, src.ID)
			_ = a.deps.Sources.SetEnabled(ctx, src.ID, false, "imap authentication failed")
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
			a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", false)
			return
		}
		logext.Warnf(ctx, "[%s] login transient err,source_id:%s", where, src.ID)
		a.transientError(ctx, src, "login: transient")
		return
	}

	selectData, err := cli.Select(cfg.Folder, ptrext.Of(imap.SelectOptions{})).Wait()
	if err != nil {
		a.transientError(ctx, src, "select: "+err.Error())
		return
	}

	src = a.seedFirstPollCursor(ctx, src, cfg.StartFrom, selectData)

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
	lastUID := a.ingestUIDs(ctx, cli, src, cfg, policy, uids)

	a.persistPollResult(ctx, src, lastUID)
	a.markPollSuccess(src.Slug, nowFn())
	a.deps.Metrics.SetPollLag(channelName, src.TenantID, src.Slug, 0)
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)
}

// seedFirstPollCursor honours spec §Email IMAP adapter's `start_from`
// policy on the first poll for a source (LastUID == 0). `now` (default)
// skips existing backlog by stamping LastUID to UIDNext-1; `all_unseen`
// starts from 0 and fetches every existing message. Subsequent polls
// already have a non-zero LastUID and pass through unchanged (G1 fix,
// #66).
func (a *adapter) seedFirstPollCursor(ctx context.Context, src inbound.Source, startFrom string, selectData *imap.SelectData) inbound.Source {
	if src.State.LastUID != 0 || strings.EqualFold(startFrom, "all_unseen") {
		return src
	}
	const where = "inbound.email.seedFirstPollCursor"
	var seedUID int64
	if selectData != nil && selectData.UIDNext > 0 {
		seedUID = int64(selectData.UIDNext) - 1
	}
	if seedUID < 0 {
		seedUID = 0
	}
	if err := a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: src.State.LastEventAt,
		LastUID:     seedUID,
	}); err != nil {
		logext.Warnf(ctx, "[%s] seed LastUID failed,source_id:%s,err:%+v",
			where, src.ID, err.Error())
	}
	src.State.LastUID = seedUID
	return src
}

// ingestUIDs walks the UID batch, fetches + parses + ingests each
// message, and returns the last UID we either ingested or chose to
// skip past. Per-UID failures bump metrics + advance the cursor (no
// wedging on a single bad message); ctx-cancel exits early.
func (a *adapter) ingestUIDs(ctx context.Context, cli *imapclient.Client, src inbound.Source, cfg Config, policy afterIngestPolicy, uids []imap.UID) int64 {
	const where = "inbound.email.ingestUIDs"
	lastUID := src.State.LastUID
	for _, uid := range uids {
		if ctx.Err() != nil {
			return lastUID
		}
		raw, ferr := fetchOne(cli, uid)
		if ferr != nil {
			// errMessageTooLarge is a validate_err (the message is
			// permanently un-ingestable; advancing the cursor prevents
			// an infinite retry loop). Anything else is transient_err.
			if errors.Is(ferr, errMessageTooLarge) {
				logext.Warnf(ctx, "[%s] msg too large,source_id:%s,uid:%d,cap:%d",
					where, src.ID, uid, maxMessageBytes)
				a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
				lastUID = int64(uid)
				continue
			}
			logext.Warnf(ctx, "[%s] fetch failed,source_id:%s,uid:%d,err:%+v",
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
			logext.Warnf(ctx, "[%s] parse failed,source_id:%s,uid:%d,err:%+v",
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
			logext.Warnf(ctx, "[%s] ingest failed,source_id:%s,uid:%d,err:%+v",
				where, src.ID, uid, err.Error())
			a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
			continue
		}
		applyAfterIngest(ctx, clientOps{cli}, uid, policy)
		lastUID = int64(uid)
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	}
	return lastUID
}

// persistPollResult writes back the per-poll state row. Two cases:
//   - new UIDs landed → advance LastUID + LastEventAt;
//   - LastUID unchanged but the row carried a stale last_error → clear it
//     so the Console UI doesn't stay red (G2 fix, #66).
func (a *adapter) persistPollResult(ctx context.Context, src inbound.Source, lastUID int64) {
	switch {
	case lastUID != src.State.LastUID:
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: ptrext.Of(nowFn()),
			LastUID:     lastUID,
		})
	case src.State.LastError != "":
		_ = a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
			LastEventAt: src.State.LastEventAt,
			LastUID:     lastUID,
		})
	}
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
// the previous bool-toggle TLS escape hatch is gone (#66 review H2 / H3).
// A loopback reverse proxy that terminates TLS can front a plain-IMAP
// server if the operator truly needs one.
var dialIMAP = func(_ context.Context, addr string, opt *imapclient.Options) (*imapclient.Client, error) {
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
				// LimitReader to maxMessageBytes+1: if we get the +1
				// byte, the server tried to hand us something too big
				// for downstream parsing.
				b, err := io.ReadAll(io.LimitReader(body.Literal, maxMessageBytes+1))
				if err != nil {
					return nil, err
				}
				if len(b) > maxMessageBytes {
					return nil, errMessageTooLarge
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

// imapOps is the narrow surface applyAfterIngest needs from the IMAP
// client — one method per logical operation, returning a plain error.
// Production wraps a live *imapclient.Client in clientOps below; tests
// supply a recording stub. We deliberately hide the underlying
// *imapclient.FetchCommand / *imapclient.MoveCommand return types: those
// carry unexported fields and would otherwise be unconstructible in
// tests (the original v2-beta caveat that pushed this function to a
// no-op for v0.3 — review H3, #66).
type imapOps interface {
	// MarkSeen adds \Seen to a single UID. Silent=true on the wire so
	// the server skips the untagged FETCH echo we'd otherwise ignore.
	MarkSeen(uid imap.UID) error
	// MoveTo moves a single UID to the named mailbox. Caller validates
	// mailbox != "" — passing "" would be a server BAD response.
	MoveTo(uid imap.UID, mailbox string) error
}

// clientOps adapts a live *imapclient.Client to imapOps.
//
// Store returns *FetchCommand because IMAP STORE replies are FETCH-shaped
// (the server echoes the post-store flag vector). With Silent=true the
// echo is suppressed, but we still call Close() to drain the wire and
// let the next command go out.
//
// Move returns *MoveCommand with an exported Wait(); we discard the
// returned *MoveData (the destination UIDs are not interesting to attune).
type clientOps struct{ c *imapclient.Client }

func (o clientOps) MarkSeen(uid imap.UID) error {
	flags := ptrext.Of(imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	})
	return o.c.Store(imap.UIDSetNum(uid), flags, nil).Close()
}

func (o clientOps) MoveTo(uid imap.UID, mailbox string) error {
	_, err := o.c.Move(imap.UIDSetNum(uid), mailbox).Wait()
	return err
}

// applyAfterIngest applies the configured after-ingest policy.
//
// The correctness primitive of the email adapter is the lastUID cursor:
// each successful Ingest advances it, so the same message is never
// re-ingested. The IMAP \Seen flag or move-to-folder is a UX nicety on
// top of that cursor — a single STORE/MOVE failure here is logged and
// swallowed, on the assumption that the operator will reconcile manually
// (e.g. they deleted the move-target folder) and the next poll round
// won't re-ingest because the cursor already advanced.
//
// move_to with an empty folder degrades to mark_seen — parseAfterIngest
// accepts the malformed "move_to:" string (its own test calls this out
// as "a config bug but parsed"), and forwarding the empty mailbox to
// the IMAP server would trip a BAD response on every poll. mark_seen is
// the spec-documented safe default; this matches the unknown-policy
// fallback in parseAfterIngest.
func applyAfterIngest(ctx context.Context, ops imapOps, uid imap.UID, policy afterIngestPolicy) {
	const where = "inbound.email.applyAfterIngest"
	switch policy.Kind {
	case "keep_unseen":
		// Intentional no-op — operator wants to preserve the IMAP
		// new-message indicator (helpdesk lite). Cursor still guards
		// against re-ingest.
		return
	case "move_to":
		if policy.Folder == "" {
			logext.Warnf(ctx, "[%s] move_to with empty folder, falling back to mark_seen,uid:%d", where, uid)
			if err := ops.MarkSeen(uid); err != nil {
				logext.Warnf(ctx, "[%s] store \\Seen failed,uid:%d,err:%+v", where, uid, err.Error())
			}
			return
		}
		if err := ops.MoveTo(uid, policy.Folder); err != nil {
			// Most likely cause: the destination folder does not exist
			// (operator typo, or they archived it). Log + continue —
			// the cursor has already advanced so we won't re-ingest.
			logext.Warnf(ctx, "[%s] move failed,uid:%d,folder:%s,err:%+v",
				where, uid, policy.Folder, err.Error())
		}
	default: // "mark_seen" + unknown fall-through (parseAfterIngest pins unknown → mark_seen)
		if err := ops.MarkSeen(uid); err != nil {
			// Already \Seen, server-side permission glitch, etc. — log,
			// don't retry. The cursor guards against re-ingest regardless.
			logext.Warnf(ctx, "[%s] store \\Seen failed,uid:%d,err:%+v", where, uid, err.Error())
		}
	}
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
var nowFn = time.Now
