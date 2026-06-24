// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"errors"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/database"
)

func TestClassifySchema(t *testing.T) {
	cases := []struct {
		name    string
		in      schemaInputs
		want    Status
		wantMsg string
	}{
		{
			name: "current and verified passes",
			in:   schemaInputs{Total: 24, Applied: 24, Pending: 0, MaxApplied: 24},
			want: StatusPass,
		},
		{
			name: "newer-than-binary fails even with empty checksum (no ErrMissingFile)",
			// Backup is at version 25 but this binary carries 24, and the checksum
			// ledger did NOT flag it (empty-checksum future row). The independent
			// max-version guard must still FAIL rather than report healthy.
			in:      schemaInputs{Total: 24, Applied: 24, Pending: 0, MaxApplied: 25},
			want:    StatusFail,
			wantMsg: "newer attune",
		},
		{
			name: "older backup warns (recoverable, applies on boot)",
			// manifest mismatch is EXPECTED when the binary embeds more
			// migrations than the backup carries — must not fail the drill.
			in: schemaInputs{
				Total: 24, Applied: 20, Pending: 4,
				ManifestErr: database.ErrManifestReorder{Stored: "old", Computed: "new"},
			},
			want:    StatusWarn,
			wantMsg: "predates",
		},
		{
			name: "checksum drift beats pending (older backup WITH drift still fails)",
			// Precedence guard: an older backup (pending>0) that also has
			// genuine checksum drift on an applied migration must FAIL, not warn.
			in: schemaInputs{
				Total: 24, Applied: 20, Pending: 4,
				ChecksumErr: database.ErrChecksumDrift{Drifted: []database.ChecksumDrift{{Version: 3}}},
			},
			want:    StatusFail,
			wantMsg: "drift",
		},
		{
			name: "dirty migration fails loudly",
			in: schemaInputs{
				Total: 24, Applied: 24, Pending: 0,
				DirtyErr: database.ErrDirtyMigration{Version: 15, Filename: "015_x.sql"},
			},
			want: StatusFail,
		},
		{
			name: "backup newer than binary fails (missing file)",
			in: schemaInputs{
				Total: 20, Applied: 20, Pending: 0,
				ChecksumErr: database.ErrMissingFile{Version: 24, Filename: "024_x.sql"},
			},
			want: StatusFail,
		},
		{
			name: "checksum drift fails",
			in: schemaInputs{
				Total: 24, Applied: 24, Pending: 0,
				ChecksumErr: database.ErrChecksumDrift{Drifted: []database.ChecksumDrift{{Version: 3}}},
			},
			want: StatusFail,
		},
		{
			name: "reorder at same version fails",
			in: schemaInputs{
				Total: 24, Applied: 24, Pending: 0,
				ManifestErr: database.ErrManifestReorder{Stored: "a", Computed: "b"},
			},
			want: StatusFail,
		},
		{
			name: "generic dirty error fails",
			in:   schemaInputs{Total: 24, Applied: 24, DirtyErr: errors.New("boom")},
			want: StatusFail,
		},
		{
			name: "generic checksum error fails",
			in:   schemaInputs{Total: 24, Applied: 24, ChecksumErr: errors.New("boom")},
			want: StatusFail,
		},
		{
			name: "generic manifest error fails",
			in:   schemaInputs{Total: 24, Applied: 24, Pending: 0, ManifestErr: errors.New("boom")},
			want: StatusFail,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySchema(tc.in)
			if got.Status != tc.want {
				t.Fatalf("classifySchema(%+v).Status = %q, want %q\nmessage: %s",
					tc.in, got.Status, tc.want, got.Message)
			}
			if got.Name != "schema" {
				t.Fatalf("Name = %q, want \"schema\"", got.Name)
			}
			if tc.wantMsg != "" && !strings.Contains(got.Message, tc.wantMsg) {
				t.Fatalf("message %q does not contain %q", got.Message, tc.wantMsg)
			}
		})
	}
}
