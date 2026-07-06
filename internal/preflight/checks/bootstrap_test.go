// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
)

type fakeBootstrapAdminCounter struct {
	count int
	err   error
}

func (f fakeBootstrapAdminCounter) Count(context.Context) (int, error) {
	return f.count, f.err
}

func TestBootstrapAdminResult_SkipsWhenConsoleDisabled(t *testing.T) {
	t.Parallel()

	r := checkBootstrapAdmin(context.Background(), &preflight.Environment{Cfg: &config.Config{}})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Contains(t, r.Message, "Console not enabled")
}

func TestBootstrapAdminResult_FailsWhenFreshAndSeedMissing(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	}, fakeBootstrapAdminCounter{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "No admins exist")
	require.Contains(t, r.Remediation, "console.bootstrap_admin")
}

func TestBootstrapAdminResult_PassesWhenFreshAndSeedPresent(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
		Console: config.ConsoleConfig{
			BootstrapAdmin: config.BootstrapAdminConfig{
				Email:    "admin@example.com",
				Password: "correct horse battery staple",
			},
		},
	}, fakeBootstrapAdminCounter{})
	require.Equal(t, preflight.StatusPass, r.Status)
	require.Contains(t, r.Message, "Bootstrap admin seed configured")
}

func TestBootstrapAdminResult_PassesWhenAdminsExist(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
		Console: config.ConsoleConfig{
			BootstrapAdmin: config.BootstrapAdminConfig{
				Email:    "admin@example.com",
				Password: "correct horse battery staple",
			},
		},
	}, fakeBootstrapAdminCounter{count: 2})
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Contains(t, r.Message, "bootstrap seed is still configured")
	require.Contains(t, r.Remediation, "Remove console.bootstrap_admin")
}

func TestBootstrapAdminResult_CountError(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	}, fakeBootstrapAdminCounter{err: errors.New("db down")})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "Unable to inspect admins table")
}
