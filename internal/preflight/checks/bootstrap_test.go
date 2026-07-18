// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestBootstrapAdminResult_FailsWhenConfigMissing(t *testing.T) {
	t.Parallel()

	r := checkBootstrapAdmin(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Equal(t, "Config not loaded", r.Message)

	r = bootstrapAdminResult(context.Background(), nil, fakeBootstrapAdminCounter{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Equal(t, "Config not loaded", r.Message)
}

func TestBootstrapAdminResult_FailsWhenDatabaseUnavailable(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing"}
	r := checkBootstrapAdmin(context.Background(), &preflight.Environment{Cfg: cfg})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "Database not available")
	require.Contains(t, r.Remediation, "database.url")

	r = bootstrapAdminResult(context.Background(), cfg, nil)
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "Database not available")
	require.Contains(t, r.Remediation, "database.url")
}

func TestBootstrapAdmin_CheckFailsWhenRepoCannotCount(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := checkBootstrapAdmin(ctx, &preflight.Environment{
		Cfg: &config.Config{
			ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
		},
		Pool: newUnreachablePreflightPool(t),
	})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Equal(t, "Unable to inspect admins table", r.Message)
	require.Contains(t, r.Remediation, "database.url")
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

func TestBootstrapAdminResult_WarnsWhenAdminsExistAndSeedCleared(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	}, fakeBootstrapAdminCounter{count: 1})
	require.Equal(t, preflight.StatusWarn, r.Status)
	require.Equal(t, "1 admin(s) already exist", r.Message)
	require.Empty(t, r.Remediation)
}

func TestBootstrapAdminResult_CountError(t *testing.T) {
	t.Parallel()

	r := bootstrapAdminResult(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	}, fakeBootstrapAdminCounter{err: errors.New("db down")})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.Contains(t, r.Message, "Unable to inspect admins table")
}
