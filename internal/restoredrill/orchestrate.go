// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// RestoreAndDrill is the push-button restore drill: it provisions an ephemeral
// database from adminURL, restores backupFile into it (shelling to psql for a
// plain SQL dump or pg_restore for custom/dir/tar), MEASURES the restore time as
// the RTO, runs the full verification battery, and tears the ephemeral database
// down on exit. The restored target is isolated by construction — nothing
// touches production. restoreTool is "psql" (default) or "pg_restore".
func RestoreAndDrill(ctx context.Context, adminURL, backupFile, restoreTool string, store *secretstore.TinkStore, opts Options) (DrillReport, error) {
	admin, err := database.NewPool(ctx, adminURL)
	if err != nil {
		return DrillReport{}, fmt.Errorf("connect admin database: %w", err)
	}
	defer admin.Close()

	name, err := ephemeralDBName()
	if err != nil {
		return DrillReport{}, err
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return DrillReport{}, fmt.Errorf("create ephemeral database: %w", err)
	}
	defer dropEphemeral(admin, name)

	ephemeralURL, err := withDatabase(adminURL, name)
	if err != nil {
		return DrillReport{}, err
	}

	restoreStart := time.Now()
	if err := runRestore(ctx, ephemeralURL, backupFile, restoreTool); err != nil {
		return DrillReport{}, fmt.Errorf("restore into ephemeral database: %w", err)
	}
	rto := time.Since(restoreStart)
	opts.RestoreDuration = ptrext.Of(rto)

	target, err := database.NewPool(ctx, ephemeralURL)
	if err != nil {
		return DrillReport{}, fmt.Errorf("connect ephemeral database: %w", err)
	}
	defer target.Close()

	return Run(ctx, target, store, time.Now(), opts), nil
}

func ephemeralDBName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate ephemeral name: %w", err)
	}
	return "attune_drill_" + hex.EncodeToString(b), nil
}

// withDatabase rewrites a postgres URL's database component.
func withDatabase(rawURL, db string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse admin url: %w", err)
	}
	u.Path = "/" + db
	return u.String(), nil
}

func dropEphemeral(admin *pgxpool.Pool, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = admin.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
		logext.Warnf(ctx, "[restoredrill] failed to drop ephemeral database %s; manual cleanup may be needed,err:%s", name, err.Error())
	}
}

// runRestore shells to psql or pg_restore, restoring ATOMICALLY: with
// --single-transaction any object error rolls the whole restore back and exits
// non-zero, so a partial restore can never be left behind and reported as a
// passing drill. The connection is passed as a URI with the password stripped
// into PGPASSWORD — the secret never appears in argv (ps-visible), while every
// other DSN parameter (sslmode, sslrootcert, options, …) is preserved.
func runRestore(ctx context.Context, dbURL, file, tool string) error {
	connURI, env, err := restoreConn(dbURL)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch tool {
	case "pg_restore":
		// --single-transaction implies --exit-on-error and makes it atomic.
		cmd = exec.CommandContext(ctx, "pg_restore",
			"--no-owner", "--no-privileges", "--single-transaction", "-d", connURI, file)
	case "psql", "":
		cmd = exec.CommandContext(ctx, "psql",
			"-v", "ON_ERROR_STOP=1", "--single-transaction", "-d", connURI, "-f", file)
	default:
		return fmt.Errorf("unknown restore tool %q (use psql or pg_restore)", tool)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", tool, err, lastLines(string(out), 5))
	}
	return nil
}

// restoreConn splits dbURL into a connection URI safe for argv (password
// removed) and an environment carrying the password via PGPASSWORD. libpq parses
// the URI and honors every query parameter, so the restore connection matches
// exactly what the operator specified — including mutual-TLS (sslrootcert/
// sslcert/sslkey) and libpq options that PG*-env reconstruction would drop.
func restoreConn(dbURL string) (connURI string, env []string, err error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse restore url: %w", err)
	}
	env = os.Environ()
	if pass, ok := u.User.Password(); ok {
		env = append(env, "PGPASSWORD="+pass)
		if usr := u.User.Username(); usr != "" {
			u.User = url.User(usr) // drop the password from the argv-visible URI
		} else {
			u.User = nil
		}
	}
	return u.String(), env, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
