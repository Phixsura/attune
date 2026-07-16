// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestBuildRotatedConfigPromotesCurrentSecret(t *testing.T) {
	t.Parallel()

	secrets := inboundtest.FakeSecrets{}
	current, err := secrets.Encrypt([]byte("old-secret"))
	if err != nil {
		t.Fatalf("encrypt current: %v", err)
	}

	before := time.Now()
	newSecret, envelope, expires, err := buildRotatedConfig(secrets, Config{
		SecretCurrentEncrypted: current,
	})
	after := time.Now()
	if err != nil {
		t.Fatalf("buildRotatedConfig() error = %v, want nil", err)
	}
	if len(newSecret) != SecretLen {
		t.Fatalf("new secret length = %d, want %d", len(newSecret), SecretLen)
	}
	minExpires := before.Add(GraceWindow).Add(-time.Second)
	maxExpires := after.Add(GraceWindow).Add(time.Second)
	if expires.Before(minExpires) || expires.After(maxExpires) {
		t.Fatalf("expires = %s, want between %s and %s", expires, minExpires, maxExpires)
	}

	plain, err := secrets.Decrypt(envelope)
	if err != nil {
		t.Fatalf("decrypt envelope: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(plain, &cfg); err != nil {
		t.Fatalf("unmarshal rotated config: %v", err)
	}
	if cfg.Version != ConfigVersion || cfg.HMACAlgo != "sha256" {
		t.Fatalf("rotated config version/algo = %d/%q, want %d/sha256", cfg.Version, cfg.HMACAlgo, ConfigVersion)
	}
	if string(cfg.SecretPreviousEncrypted) != string(current) {
		t.Fatal("rotated config did not preserve previous secret")
	}
	decryptedCurrent, err := secrets.Decrypt(cfg.SecretCurrentEncrypted)
	if err != nil {
		t.Fatalf("decrypt new current secret: %v", err)
	}
	if string(decryptedCurrent) != string(newSecret) {
		t.Fatal("new current ciphertext does not decrypt to returned secret")
	}
	if cfg.PreviousExpiresAt == nil || !cfg.PreviousExpiresAt.Equal(expires) {
		t.Fatalf("previous expiry = %v, want %s", cfg.PreviousExpiresAt, expires)
	}
}

func TestBuildRotatedConfigSurfacesEncryptionErrors(t *testing.T) {
	t.Parallel()

	_, _, _, err := buildRotatedConfig(failingWebhookSecrets{encryptErr: errors.New("new secret unavailable")}, Config{})
	if err == nil || !strings.Contains(err.Error(), "encrypt new") {
		t.Fatalf("buildRotatedConfig(first encrypt) error = %v, want encrypt new", err)
	}

	secrets := ptrext.Of(countingWebhookSecrets{failOnCall: 2})
	_, _, _, err = buildRotatedConfig(secrets, Config{})
	if err == nil || !strings.Contains(err.Error(), "encrypt config") {
		t.Fatalf("buildRotatedConfig(second encrypt) error = %v, want encrypt config", err)
	}
}

func TestLoadRotateConfigDecryptsAndRejectsGraceWindow(t *testing.T) {
	t.Parallel()

	secrets := inboundtest.FakeSecrets{}
	envelope := mustWebhookConfigEnvelope(t, secrets, Config{Version: ConfigVersion, HMACAlgo: "sha256"})

	cfg, err := loadRotateConfig(context.Background(), ptrext.Of(fakeWebhookRotateTx{row: fakeWebhookRotateRow{value: envelope}}), secrets, "src-1")
	if err != nil {
		t.Fatalf("loadRotateConfig() error = %v, want nil", err)
	}
	if cfg.Version != ConfigVersion || cfg.HMACAlgo != "sha256" {
		t.Fatalf("loaded config = %+v", cfg)
	}

	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	blockedEnvelope := mustWebhookConfigEnvelope(t, secrets, Config{
		Version:           ConfigVersion,
		HMACAlgo:          "sha256",
		PreviousExpiresAt: ptrext.Of(future),
	})
	cfg, err = loadRotateConfig(context.Background(), ptrext.Of(fakeWebhookRotateTx{
		row: fakeWebhookRotateRow{value: blockedEnvelope},
	}), secrets, "src-1")
	if !errors.Is(err, ErrRotationInGraceWindow) {
		t.Fatalf("loadRotateConfig(grace) error = %v, want ErrRotationInGraceWindow", err)
	}
	if cfg.PreviousExpiresAt == nil || !cfg.PreviousExpiresAt.Equal(future) {
		t.Fatalf("blocked config expiry = %v, want %s", cfg.PreviousExpiresAt, future)
	}
}

func TestLoadRotateConfigSurfacesDecodeErrors(t *testing.T) {
	t.Parallel()

	secrets := inboundtest.FakeSecrets{}
	for _, tc := range []struct {
		name string
		row  fakeWebhookRotateRow
		want string
	}{
		{name: "missing source", row: fakeWebhookRotateRow{err: pgx.ErrNoRows}, want: "source not found"},
		{name: "select error", row: fakeWebhookRotateRow{err: errors.New("select failed")}, want: "select"},
		{name: "decrypt error", row: fakeWebhookRotateRow{value: []byte{0x01}}, want: "decrypt config"},
		{name: "unmarshal error", row: fakeWebhookRotateRow{value: mustWebhookEncrypt(t, secrets, []byte("{"))}, want: "unmarshal config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadRotateConfig(context.Background(), ptrext.Of(fakeWebhookRotateTx{row: tc.row}), secrets, "src-1")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadRotateConfig() error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func mustWebhookConfigEnvelope(t *testing.T, secrets inboundtest.FakeSecrets, cfg Config) []byte {
	t.Helper()
	plain, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return mustWebhookEncrypt(t, secrets, plain)
}

func mustWebhookEncrypt(t *testing.T, secrets inboundtest.FakeSecrets, plain []byte) []byte {
	t.Helper()
	envelope, err := secrets.Encrypt(plain)
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	return envelope
}

type failingWebhookSecrets struct {
	encryptErr error
	decryptErr error
}

func (f failingWebhookSecrets) Encrypt([]byte) ([]byte, error) { return nil, f.encryptErr }
func (f failingWebhookSecrets) Decrypt([]byte) ([]byte, error) { return nil, f.decryptErr }

type countingWebhookSecrets struct {
	calls      int
	failOnCall int
}

func (c *countingWebhookSecrets) Encrypt(b []byte) ([]byte, error) {
	c.calls++
	if c.calls == c.failOnCall {
		return nil, errors.New("encrypt failed")
	}
	return inboundtest.FakeSecrets{}.Encrypt(b)
}

func (c *countingWebhookSecrets) Decrypt(b []byte) ([]byte, error) {
	return inboundtest.FakeSecrets{}.Decrypt(b)
}

type fakeWebhookRotateTx struct {
	row fakeWebhookRotateRow
}

func (f *fakeWebhookRotateTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeWebhookRotateTx) Commit(context.Context) error          { return nil }
func (f *fakeWebhookRotateTx) Rollback(context.Context) error        { return nil }
func (f *fakeWebhookRotateTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}

func (f *fakeWebhookRotateTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeWebhookRotateTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeWebhookRotateTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}

func (f *fakeWebhookRotateTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (f *fakeWebhookRotateTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (f *fakeWebhookRotateTx) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }
func (f *fakeWebhookRotateTx) Conn() *pgx.Conn                                  { return nil }

type fakeWebhookRotateRow struct {
	value []byte
	err   error
}

func (r fakeWebhookRotateRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected scan destination")
	}
	out, ok := dest[0].(*[]byte)
	if !ok {
		return errors.New("unexpected scan type")
	}
	*out = append([]byte(nil), r.value...)
	return nil
}
