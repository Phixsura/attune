// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
)

type fakeBootstrapAdminStore struct {
	count        int
	countErr     error
	bootstrapErr error

	bootstraps []admin.NewAdmin
}

func (s *fakeBootstrapAdminStore) Count(context.Context) (int, error) {
	return s.count, s.countErr
}

func (s *fakeBootstrapAdminStore) Bootstrap(_ context.Context, n admin.NewAdmin) error {
	s.bootstraps = append(s.bootstraps, n)
	return s.bootstrapErr
}

func TestBootstrapAdminBranches(t *testing.T) {
	tests := []struct {
		name          string
		store         *fakeBootstrapAdminStore
		cfg           BootstrapConfig
		wantErr       string
		wantBootstrap bool
	}{
		{
			name:    "count error",
			store:   ptrext.Of(fakeBootstrapAdminStore{countErr: errors.New("count failed")}),
			cfg:     BootstrapConfig{Email: "admin@example.test", Password: "strong-secret-value"},
			wantErr: "count admins",
		},
		{
			name:  "existing admin skips bootstrap",
			store: ptrext.Of(fakeBootstrapAdminStore{count: 1}),
			cfg:   BootstrapConfig{Email: "admin@example.test", Password: "strong-secret-value"},
		},
		{
			name:    "missing credentials",
			store:   ptrext.Of(fakeBootstrapAdminStore{}),
			cfg:     BootstrapConfig{},
			wantErr: "console.bootstrap_admin.{email,password} are unset",
		},
		{
			name:    "weak password",
			store:   ptrext.Of(fakeBootstrapAdminStore{}),
			cfg:     BootstrapConfig{Email: "admin@example.test", Password: "short"},
			wantErr: "must be at least",
		},
		{
			name: "bootstrap error",
			store: ptrext.Of(fakeBootstrapAdminStore{
				bootstrapErr: errors.New("insert failed"),
			}),
			cfg:           BootstrapConfig{Email: "admin@example.test", Password: "strong-secret-value"},
			wantErr:       "bootstrap",
			wantBootstrap: true,
		},
		{
			name: "already bootstrapped is benign",
			store: ptrext.Of(fakeBootstrapAdminStore{
				bootstrapErr: admin.ErrAlreadyBootstrapped,
			}),
			cfg:           BootstrapConfig{Email: "admin@example.test", Password: "strong-secret-value"},
			wantBootstrap: true,
		},
		{
			name:          "success",
			store:         ptrext.Of(fakeBootstrapAdminStore{}),
			cfg:           BootstrapConfig{Email: "admin@example.test", Password: "strong-secret-value"},
			wantBootstrap: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := BootstrapAdmin(context.Background(), tc.store, tc.cfg)

			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantBootstrap {
				require.Len(t, tc.store.bootstraps, 1)
				got := tc.store.bootstraps[0]
				require.Equal(t, tc.cfg.Email, got.Email)
				require.Equal(t, tc.cfg.Email, got.DisplayName)
				require.Equal(t, "admin", got.Role)
				require.NotEmpty(t, got.PasswordHash)
				require.True(t, VerifyOrDummy(got.PasswordHash, tc.cfg.Password))
				require.False(t, strings.Contains(got.PasswordHash, tc.cfg.Password))
			} else {
				require.Empty(t, tc.store.bootstraps)
			}
		})
	}
}
