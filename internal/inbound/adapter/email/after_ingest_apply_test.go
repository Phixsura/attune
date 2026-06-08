// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fakeOps records every MarkSeen / MoveTo call so the table tests can
// assert exact wire effects without dialing an IMAP server. err* fields
// let a test inject a failure on a specific operation.
type fakeOps struct {
	markSeenUIDs []imap.UID
	moveCalls    []moveCall
	errMarkSeen  error
	errMove      error
}

type moveCall struct {
	UID     imap.UID
	Mailbox string
}

func (f *fakeOps) MarkSeen(uid imap.UID) error {
	f.markSeenUIDs = append(f.markSeenUIDs, uid)
	return f.errMarkSeen
}

func (f *fakeOps) MoveTo(uid imap.UID, mailbox string) error {
	f.moveCalls = append(f.moveCalls, moveCall{UID: uid, Mailbox: mailbox})
	return f.errMove
}

// TestApplyAfterIngest_PolicyDispatch — table covers every legal policy
// the parseAfterIngest output can take, asserting which IMAP command
// (if any) fires and with what arguments. Logger.Warnf failures fall
// through silently — the cursor in pollSource is the correctness
// primitive (see comment block on applyAfterIngest).
func TestApplyAfterIngest_PolicyDispatch(t *testing.T) {
	const targetUID imap.UID = 4711
	type want struct {
		seenUIDs []imap.UID
		moves    []moveCall
	}
	tests := []struct {
		name   string
		policy afterIngestPolicy
		want   want
	}{
		{
			name:   "mark_seen calls Store with \\Seen exactly once",
			policy: afterIngestPolicy{Kind: "mark_seen"},
			want: want{
				seenUIDs: []imap.UID{targetUID},
			},
		},
		{
			name:   "keep_unseen makes no IMAP calls",
			policy: afterIngestPolicy{Kind: "keep_unseen"},
			want:   want{},
		},
		{
			name:   "move_to with folder calls Move",
			policy: afterIngestPolicy{Kind: "move_to", Folder: "Processed"},
			want: want{
				moves: []moveCall{{UID: targetUID, Mailbox: "Processed"}},
			},
		},
		{
			name:   "move_to with empty folder falls back to mark_seen",
			policy: afterIngestPolicy{Kind: "move_to", Folder: ""},
			want: want{
				seenUIDs: []imap.UID{targetUID},
			},
		},
		{
			name:   "unknown kind treated as mark_seen (defense-in-depth vs parseAfterIngest)",
			policy: afterIngestPolicy{Kind: "future_policy_v2"},
			want: want{
				seenUIDs: []imap.UID{targetUID},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := ptrext.Of(fakeOps{})
			applyAfterIngest(context.Background(), ops, targetUID, tt.policy, inboundtest.FakeLogger{})

			if got := ops.markSeenUIDs; !equalUIDs(got, tt.want.seenUIDs) {
				t.Errorf("MarkSeen UIDs = %v, want %v", got, tt.want.seenUIDs)
			}
			if got := ops.moveCalls; !equalMoveCalls(got, tt.want.moves) {
				t.Errorf("Move calls = %+v, want %+v", got, tt.want.moves)
			}
		})
	}
}

// TestApplyAfterIngest_ErrorsAreSwallowed — when the IMAP command itself
// fails (e.g. operator deleted the target folder mid-poll), the function
// must NOT panic or propagate. The lastUID cursor in pollSource has
// already advanced; a one-shot wire failure is recoverable on the next
// round and shouldn't disrupt the poll loop.
func TestApplyAfterIngest_ErrorsAreSwallowed(t *testing.T) {
	cases := []struct {
		name   string
		policy afterIngestPolicy
		ops    *fakeOps
	}{
		{
			name:   "MarkSeen error is logged + swallowed",
			policy: afterIngestPolicy{Kind: "mark_seen"},
			ops:    ptrext.Of(fakeOps{errMarkSeen: errors.New("server: permission denied")}),
		},
		{
			name:   "MoveTo error is logged + swallowed",
			policy: afterIngestPolicy{Kind: "move_to", Folder: "Archive"},
			ops:    ptrext.Of(fakeOps{errMove: errors.New("server: no such mailbox")}),
		},
		{
			name:   "MarkSeen error inside the move_to empty-folder fallback is logged + swallowed",
			policy: afterIngestPolicy{Kind: "move_to", Folder: ""},
			ops:    ptrext.Of(fakeOps{errMarkSeen: errors.New("server: permission denied")}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("applyAfterIngest panicked on err path: %v", r)
				}
			}()
			applyAfterIngest(context.Background(), tc.ops, 100, tc.policy, inboundtest.FakeLogger{})
		})
	}
}

func equalUIDs(a, b []imap.UID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalMoveCalls(a, b []moveCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
