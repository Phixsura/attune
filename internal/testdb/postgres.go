//go:build integration

// Package testdb provides the real-Postgres harness for integration tests.
package testdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const serviceContainerDSNEnv = "ATTUNE_TEST_DATABASE_URL"

// withDatabase returns dsn with its database (the URL path) replaced by dbName,
// preserving scheme, credentials, host, and query parameters.
func withDatabase(dsn, dbName string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse dsn: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}

// NewPool returns a migrated, isolated PostgreSQL pool for one integration
// test. In CI, ATTUNE_TEST_DATABASE_URL points at the Postgres service
// container and NewPool creates a temporary database per test. Locally, when
// that env var is unset, it starts a fresh Docker Postgres container or falls
// back to local PostgreSQL binaries if Docker is unavailable.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup, err := OpenPool()
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	t.Cleanup(cleanup)
	return pool
}

// NewReplicaPool is like NewPool but guarantees the server accepts replication
// connections from outside the container — required by tests that run
// pg_basebackup (the restore-drill verify-backup test). In service-container
// mode (CI) replication is enabled globally by the harness; locally it is
// enabled on the throwaway container or local cluster.
func NewReplicaPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup, err := openPool(true)
	if err != nil {
		t.Fatalf("open replication-enabled postgres pool: %v", err)
	}
	t.Cleanup(cleanup)
	return pool
}

// OpenPool returns a migrated PostgreSQL pool plus a cleanup callback. It is
// intended for package-level integration fixtures that want to reuse one
// database across multiple tests.
func OpenPool() (*pgxpool.Pool, func(), error) {
	return openPool(false)
}

func openPool(replication bool) (*pgxpool.Pool, func(), error) {
	if dsn := os.Getenv(serviceContainerDSNEnv); dsn != "" {
		// Service-container mode (CI): replication is enabled globally by the CI
		// harness, so there is nothing per-pool to do here.
		return openServicePool(dsn)
	}
	return openContainerPool(replication)
}

func openContainerPool(replication bool) (*pgxpool.Pool, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	container, err := startPostgresLauncher(ctx)
	if err != nil {
		return nil, nil, err
	}

	var pool *pgxpool.Pool
	cleanup := func() {
		if pool != nil {
			pool.Close()
		}
		_ = container.Terminate(context.Background())
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("postgres dsn: %w", err)
	}

	pool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("connect postgres pool: %w", err)
	}

	if replication {
		if err := container.EnableReplication(ctx, pool); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("enable replication: %w", err)
		}
	}

	if err := database.RunMigrations(context.Background(), pool); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}
	return pool, cleanup, nil
}

type postgresLauncher interface {
	ConnectionString(context.Context, ...string) (string, error)
	EnableReplication(context.Context, *pgxpool.Pool) error
	Terminate(context.Context) error
}

type dockerContainer struct {
	id       string
	hostPort string
}

type localCluster struct {
	dataDir  string
	hostPort string
}

func startPostgresLauncher(ctx context.Context) (postgresLauncher, error) {
	docker, dockerErr := startDockerContainer(ctx)
	if dockerErr == nil {
		return docker, nil
	}

	local, localErr := startLocalCluster(ctx)
	if localErr == nil {
		return local, nil
	}

	return nil, fmt.Errorf("start postgres container: %v; local postgres fallback: %v", dockerErr, localErr)
}

func startDockerContainer(ctx context.Context) (*dockerContainer, error) {
	containerName := "attune-testdb-" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))
	containerID, err := dockerOutput(ctx,
		"run",
		"-d",
		"--rm",
		"--name", containerName,
		"-e", "POSTGRES_DB=attune",
		"-e", "POSTGRES_USER=attune",
		"-e", "POSTGRES_PASSWORD=attune",
		"-p", "127.0.0.1::5432",
		"pgvector/pgvector:pg17",
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	hostPort, err := waitForMappedPort(ctx, strings.TrimSpace(containerID), "5432/tcp")
	if err != nil {
		_ = dockerRun(ctx, "rm", "-f", strings.TrimSpace(containerID))
		return nil, err
	}

	dsn := fmt.Sprintf("postgres://attune:attune@127.0.0.1:%s/attune?sslmode=disable", hostPort)
	if err := waitForDSN(ctx, dsn); err != nil {
		_ = dockerRun(ctx, "rm", "-f", strings.TrimSpace(containerID))
		return nil, err
	}

	return ptrext.Of(dockerContainer{id: strings.TrimSpace(containerID), hostPort: hostPort}), nil
}

func startLocalCluster(ctx context.Context) (*localCluster, error) {
	dataDir, err := os.MkdirTemp("", "attune-testdb-local-*")
	if err != nil {
		return nil, fmt.Errorf("create postgres data dir: %w", err)
	}

	cluster := ptrext.Of(localCluster{dataDir: dataDir})
	if err := cluster.init(ctx); err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, err
	}
	if err := cluster.bootstrap(ctx); err != nil {
		_ = cluster.Terminate(context.Background())
		return nil, err
	}
	return cluster, nil
}

func (c *dockerContainer) ConnectionString(_ context.Context, options ...string) (string, error) {
	query := strings.Join(options, "&")
	if query != "" {
		query = "?" + query
	}
	return fmt.Sprintf("postgres://attune:attune@127.0.0.1:%s/attune%s", c.hostPort, query), nil
}

func (c *dockerContainer) EnableReplication(ctx context.Context, pool *pgxpool.Pool) error {
	code, _, err := dockerExec(ctx, c.id, "sh", "-c",
		`echo "host replication all all scram-sha-256" >> /var/lib/postgresql/data/pg_hba.conf`)
	if err != nil {
		return fmt.Errorf("append pg_hba: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("append pg_hba: exit %d", code)
	}
	if _, err := pool.Exec(ctx, "SELECT pg_reload_conf()"); err != nil {
		return fmt.Errorf("reload pg_hba: %w", err)
	}
	return nil
}

func (c *dockerContainer) Terminate(ctx context.Context) error {
	return dockerRun(ctx, "rm", "-f", c.id)
}

func (c *localCluster) ConnectionString(_ context.Context, options ...string) (string, error) {
	query := strings.Join(options, "&")
	if query != "" {
		query = "?" + query
	}
	return fmt.Sprintf("postgres://attune:attune@127.0.0.1:%s/attune%s", c.hostPort, query), nil
}

func (c *localCluster) EnableReplication(context.Context, *pgxpool.Pool) error {
	return nil
}

func (c *localCluster) Terminate(ctx context.Context) error {
	if c == nil {
		return nil
	}
	stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pgCtl(stopCtx, "-D", c.dataDir, "stop", "-m", "fast"); err != nil {
		_ = killPostgresProcess(c.dataDir)
	}
	return os.RemoveAll(c.dataDir)
}

func (c *localCluster) init(ctx context.Context) error {
	if err := runCommand(ctx, "initdb", "-D", c.dataDir, "-A", "trust", "--locale=C", "--encoding=UTF8"); err != nil {
		return fmt.Errorf("init postgres cluster: %w", err)
	}
	if err := prependFile(filepath.Join(c.dataDir, "pg_hba.conf"), localPgHBA()); err != nil {
		return fmt.Errorf("prepare pg_hba.conf: %w", err)
	}
	return nil
}

func (c *localCluster) bootstrap(ctx context.Context) error {
	port, err := pickFreePort()
	if err != nil {
		return fmt.Errorf("pick postgres port: %w", err)
	}
	c.hostPort = port
	if err := pgCtl(ctx, "-D", c.dataDir, "-l", filepath.Join(c.dataDir, "postgres.log"), "start",
		"-o", fmt.Sprintf("-p %s -c listen_addresses=127.0.0.1 -c wal_level=replica -c max_wal_senders=10 -c max_replication_slots=10", c.hostPort)); err != nil {
		return fmt.Errorf("start postgres cluster: %w", err)
	}

	bootstrapDSN := fmt.Sprintf("postgres://%s@127.0.0.1:%s/postgres?sslmode=disable", bootstrapUser(), c.hostPort)
	if err := waitForLocalDSN(ctx, bootstrapDSN); err != nil {
		return err
	}
	if err := bootstrapLocalRoleAndDB(ctx, c.hostPort); err != nil {
		return err
	}
	finalDSN, err := c.ConnectionString(context.Background(), "sslmode=disable")
	if err != nil {
		return err
	}
	if err := waitForLocalDSN(ctx, finalDSN); err != nil {
		return err
	}
	return nil
}

func bootstrapLocalRoleAndDB(ctx context.Context, port string) error {
	if err := runCommand(ctx, "psql", "-h", "127.0.0.1", "-p", port, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c",
		"DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'attune') THEN CREATE ROLE attune LOGIN REPLICATION SUPERUSER; END IF; END $$;"); err != nil {
		return fmt.Errorf("create attune role: %w", err)
	}
	if err := runCommand(ctx, "psql", "-h", "127.0.0.1", "-p", port, "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c",
		"CREATE DATABASE attune OWNER attune;"); err != nil {
		return fmt.Errorf("create attune database: %w", err)
	}
	return nil
}

func waitForLocalDSN(ctx context.Context, dsn string) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := pgx.Connect(pingCtx, dsn)
		cancel()
		if err == nil {
			_ = conn.Close(context.Background())
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for local postgres ready: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("wait for local postgres ready: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout := ptrext.Of(bytes.Buffer{})
	stderr := ptrext.Of(bytes.Buffer{})
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func pgCtl(ctx context.Context, args ...string) error {
	return runCommand(ctx, "pg_ctl", args...)
}

func localPgHBA() string {
	return strings.Join([]string{
		"host all all 127.0.0.1/32 trust",
		"host all all ::1/128 trust",
		"host replication all 127.0.0.1/32 trust",
		"host replication all ::1/128 trust",
		"",
	}, "\n")
}

func bootstrapUser() string {
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("LOGNAME")); u != "" {
		return u
	}
	return "postgres"
}

func prependFile(path, prefix string) error {
	rest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(prefix), rest...), 0o600)
}

func pickFreePort() (string, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return "", errors.New("unexpected local tcp addr type")
	}
	return fmt.Sprintf("%d", addr.Port), nil
}

func killPostgresProcess(dataDir string) error {
	pidPath := filepath.Join(dataDir, "postmaster.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 {
		return errors.New("postmaster.pid was empty")
	}
	pid := strings.TrimSpace(lines[0])
	if pid == "" {
		return errors.New("postmaster.pid missing pid")
	}
	return runCommand(context.Background(), "kill", "-TERM", pid)
}

func waitForMappedPort(ctx context.Context, containerID, containerPort string) (string, error) {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		out, err := dockerOutput(ctx, "port", containerID, containerPort)
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				_, port, splitErr := net.SplitHostPort(line)
				if splitErr == nil && port != "" {
					return port, nil
				}
				lastErr = fmt.Errorf("parse docker port output %q: %w", out, splitErr)
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("docker port output was empty for container %s", containerID)
			}
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for mapped port: %w", ctx.Err())
		case <-deadline.C:
			return "", fmt.Errorf("wait for mapped port: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func waitForDSN(ctx context.Context, dsn string) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		conn, err := pgx.Connect(pingCtx, dsn)
		cancel()
		if err == nil {
			_ = conn.Close(context.Background())
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for postgres ready: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("wait for postgres ready: %w", lastErr)
		case <-ticker.C:
		}
	}
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout := ptrext.Of(bytes.Buffer{})
	stderr := ptrext.Of(bytes.Buffer{})
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func dockerRun(ctx context.Context, args ...string) error {
	_, err := dockerOutput(ctx, args...)
	return err
}

func dockerExec(ctx context.Context, containerID string, args ...string) (int, string, error) {
	fullArgs := append([]string{"exec", containerID}, args...)
	cmd := exec.CommandContext(ctx, "docker", fullArgs...)
	stdout := ptrext.Of(bytes.Buffer{})
	stderr := ptrext.Of(bytes.Buffer{})
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	output := stdout.String()
	if err != nil {
		exitCode := 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return exitCode, output, fmt.Errorf("docker %s: %w: %s", strings.Join(fullArgs, " "), err, strings.TrimSpace(stderr.String()))
	}
	return 0, output, nil
}

func openServicePool(adminDSN string) (*pgxpool.Pool, func(), error) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("connect service postgres: %w", err)
	}

	dbName := "attune_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()); err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("create test database %s: %w", dbName, err)
	}
	dropDB := func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = admin.Exec(dropCtx, `DROP DATABASE IF EXISTS `+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
	}

	// Build the per-test DSN as a real connection string (not adminDSN with a
	// ConnConfig database override) so pool.Config().ConnString() reflects the
	// per-test database. External tools (pg_dump, pg_basebackup) and any other
	// DSN consumer then target the seeded test DB, not the base admin one.
	perTestDSN, err := withDatabase(adminDSN, dbName)
	if err != nil {
		dropDB()
		return nil, nil, fmt.Errorf("build per-test dsn: %w", err)
	}
	cfg, err := pgxpool.ParseConfig(perTestDSN)
	if err != nil {
		dropDB()
		return nil, nil, fmt.Errorf("parse per-test dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		dropDB()
		return nil, nil, fmt.Errorf("connect test database %s: %w", dbName, err)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		dropDB()
		return nil, nil, fmt.Errorf("run migrations in %s: %w", dbName, err)
	}
	return pool, func() {
		pool.Close()
		dropDB()
	}, nil
}
