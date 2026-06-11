//go:build integration

package secretrotation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tinkaead "github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"

	"github.com/Phixsura/attune/internal/inbound/adapter/email"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	rotationrepo "github.com/Phixsura/attune/internal/repo/secretrotation"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
	rotationsvc "github.com/Phixsura/attune/internal/service/secretrotation"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ReencryptRotatesLLMAndNestedInboundSecrets(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	fixture := seedSecretRotationFixture(t, ctx, pool)
	svc := rotationsvc.New(rotationrepo.New(pool), fixture.store)

	assertRetireBlocked(t, ctx, svc, fixture.oldID)
	assertDryRunReport(t, ctx, svc, fixture.llmID, fixture.oldID, pool)
	assertApplyReport(t, ctx, svc, fixture, pool)
	assertRetireSucceeds(t, ctx, pool, svc, fixture.oldID)
}

type rotationFixture struct {
	store     *secretstore.TinkStore
	oldID     string
	newID     string
	llmID     uuid.UUID
	webhookID uuid.UUID
	emailID   uuid.UUID
}

func seedSecretRotationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) rotationFixture {
	t.Helper()
	oldRaw, combinedRaw, oldID, newID := rotationKeysets(t)
	oldStore := mustTinkStore(t, oldRaw)
	combinedStore := mustTinkStore(t, combinedRaw)
	assertPrimaryKeyID(t, combinedStore, newID)
	if err := llmconfigsvc.NewService(llmconfigrepo.New(pool), combinedStore).SyncKeyRegistry(ctx); err != nil {
		t.Fatalf("sync key registry: %v", err)
	}
	tenantID := "tenant-secret-rotation"
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "secret-rotation", "Secret Rotation",
	); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	llmID := insertOldLLMChannel(t, ctx, pool, oldStore, oldID)
	webhookID := insertOldWebhookSource(t, ctx, pool, oldStore, tenantID)
	emailID := insertOldEmailSource(t, ctx, pool, oldStore, tenantID)
	return rotationFixture{
		store:     combinedStore,
		oldID:     oldID,
		newID:     newID,
		llmID:     llmID,
		webhookID: webhookID,
		emailID:   emailID,
	}
}

func assertRetireBlocked(t *testing.T, ctx context.Context, svc *rotationsvc.Service, oldID string) {
	t.Helper()
	if _, err := svc.RetireKey(ctx, oldID, rotationsvc.RetireOptions{Apply: true}); !errors.Is(err, rotationsvc.ErrKeyStillReferenced) {
		t.Fatalf("retire referenced key err = %v; want %v", err, rotationsvc.ErrKeyStillReferenced)
	}
}

func assertDryRunReport(
	t *testing.T,
	ctx context.Context,
	svc *rotationsvc.Service,
	llmID uuid.UUID,
	oldID string,
	pool *pgxpool.Pool,
) {
	t.Helper()
	dryRun, err := svc.Reencrypt(ctx, rotationsvc.ReencryptOptions{})
	if err != nil {
		t.Fatalf("dry-run reencrypt: %v", err)
	}
	if dryRun.RowsUpdated != 0 || dryRun.ValuesRotated != 6 {
		t.Fatalf("dry-run rows=%d values=%d; want rows=0 values=6", dryRun.RowsUpdated, dryRun.ValuesRotated)
	}
	assertLLMKeyID(t, ctx, pool, llmID, oldID)
}

func assertApplyReport(
	t *testing.T,
	ctx context.Context,
	svc *rotationsvc.Service,
	fixture rotationFixture,
	pool *pgxpool.Pool,
) {
	t.Helper()
	applied, err := svc.Reencrypt(ctx, rotationsvc.ReencryptOptions{Apply: true})
	if err != nil {
		t.Fatalf("apply reencrypt: %v", err)
	}
	if applied.RowsUpdated != 3 || applied.ValuesRotated != 6 {
		t.Fatalf("apply rows=%d values=%d; want rows=3 values=6", applied.RowsUpdated, applied.ValuesRotated)
	}
	assertLLMSecret(t, ctx, pool, fixture.store, fixture.llmID, fixture.newID, "sk-old")
	assertWebhookSecrets(t, ctx, pool, fixture.store, fixture.webhookID, fixture.newID)
	assertEmailSecret(t, ctx, pool, fixture.store, fixture.emailID, fixture.newID)
}

func assertRetireSucceeds(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	svc *rotationsvc.Service,
	oldID string,
) {
	t.Helper()
	retired, err := svc.RetireKey(ctx, oldID, rotationsvc.RetireOptions{Apply: true})
	if err != nil {
		t.Fatalf("retire old key: %v", err)
	}
	if retired.RetiredKeyID != oldID {
		t.Fatalf("retired key = %s; want %s", retired.RetiredKeyID, oldID)
	}
	var status string
	var retiredSet bool
	if err := pool.QueryRow(ctx,
		`SELECT status, retired_at IS NOT NULL FROM secret_key_registry WHERE key_id = $1`,
		oldID,
	).Scan(&status, &retiredSet); err != nil {
		t.Fatalf("read retired key: %v", err)
	}
	if status != "DISABLED" || !retiredSet {
		t.Fatalf("registry status=%s retired=%t; want DISABLED true", status, retiredSet)
	}
}

func assertPrimaryKeyID(t *testing.T, store *secretstore.TinkStore, want string) {
	t.Helper()
	if store.PrimaryKeyID() != want {
		t.Fatalf("primary key = %s; want %s", store.PrimaryKeyID(), want)
	}
}

func rotationKeysets(t *testing.T) (oldRaw, combinedRaw, oldID, newID string) {
	t.Helper()
	km := keyset.NewManager()
	oldUint, err := km.Add(tinkaead.AES256GCMKeyTemplate())
	if err != nil {
		t.Fatalf("add old key: %v", err)
	}
	if err := km.SetPrimary(oldUint); err != nil {
		t.Fatalf("set old primary: %v", err)
	}
	oldHandle, err := km.Handle()
	if err != nil {
		t.Fatalf("old handle: %v", err)
	}
	newUint, err := km.Add(tinkaead.AES256GCMKeyTemplate())
	if err != nil {
		t.Fatalf("add new key: %v", err)
	}
	if err := km.SetPrimary(newUint); err != nil {
		t.Fatalf("set new primary: %v", err)
	}
	combinedHandle, err := km.Handle()
	if err != nil {
		t.Fatalf("combined handle: %v", err)
	}
	return writeKeyset(t, oldHandle), writeKeyset(t, combinedHandle),
		fmt.Sprintf("%d", oldUint), fmt.Sprintf("%d", newUint)
}

func writeKeyset(t *testing.T, handle *keyset.Handle) string {
	t.Helper()
	var buf bytes.Buffer
	if err := insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(&buf)); err != nil {
		t.Fatalf("write keyset: %v", err)
	}
	return buf.String()
}

func mustTinkStore(t *testing.T, raw string) *secretstore.TinkStore {
	t.Helper()
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatalf("new Tink store: %v", err)
	}
	return store
}

func insertOldLLMChannel(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	oldID string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	value, err := store.EncryptValue(
		[]byte("sk-old"),
		secretstore.AssociatedData("llm_channel", id.String(), "api_key"),
	)
	if err != nil {
		t.Fatalf("encrypt llm credential: %v", err)
	}
	if value.KeyID != oldID {
		t.Fatalf("llm key id = %s; want %s", value.KeyID, oldID)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO llm_channels
		 (id, name, protocol, base_url, auth_mode, credential_key_id,
		  credential_ciphertext, status, priority, weight, timeout_seconds)
		VALUES ($1, 'old primary', 'openai-compat', 'http://localhost:11434',
		        'bearer', $2, $3, 'enabled', 0, 1, 60)`,
		id, value.KeyID, value.Ciphertext,
	)
	if err != nil {
		t.Fatalf("insert llm channel: %v", err)
	}
	return id
}

func insertOldWebhookSource(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	tenantID string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	current, err := store.EncryptValue([]byte("current-secret"), nil)
	if err != nil {
		t.Fatalf("encrypt webhook current: %v", err)
	}
	previous, err := store.EncryptValue([]byte("previous-secret"), nil)
	if err != nil {
		t.Fatalf("encrypt webhook previous: %v", err)
	}
	raw, err := json.Marshal(webhook.Config{
		Version:                 webhook.ConfigVersion,
		SecretCurrentEncrypted:  current.Ciphertext,
		SecretPreviousEncrypted: previous.Ciphertext,
		HMACAlgo:                "sha256",
	})
	if err != nil {
		t.Fatalf("marshal webhook config: %v", err)
	}
	outer, err := store.EncryptValue(raw, nil)
	if err != nil {
		t.Fatalf("encrypt webhook outer: %v", err)
	}
	insertInbound(t, ctx, pool, id, tenantID, "webhook", outer.Ciphertext)
	return id
}

func insertOldEmailSource(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	tenantID string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	password, err := store.EncryptValue([]byte("email-password"), nil)
	if err != nil {
		t.Fatalf("encrypt email password: %v", err)
	}
	raw, err := json.Marshal(email.Config{
		Version:           email.ConfigVersion,
		Host:              "mail.example.com",
		Port:              993,
		TLS:               true,
		Username:          "alerts@example.com",
		PasswordEncrypted: password.Ciphertext,
		Folder:            "INBOX",
		StartFrom:         "now",
		AfterIngest:       "mark_seen",
	})
	if err != nil {
		t.Fatalf("marshal email config: %v", err)
	}
	outer, err := store.EncryptValue(raw, nil)
	if err != nil {
		t.Fatalf("encrypt email outer: %v", err)
	}
	insertInbound(t, ctx, pool, id, tenantID, "email", outer.Ciphertext)
	return id
}

func insertInbound(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id uuid.UUID,
	tenantID string,
	channel string,
	config []byte,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO inbound_sources (id, tenant_id, channel, name, slug, config, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE)`,
		id, tenantID, channel, channel+" source", channel+"-source", config,
	)
	if err != nil {
		t.Fatalf("insert inbound %s source: %v", channel, err)
	}
}

func assertLLMKeyID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx,
		`SELECT credential_key_id FROM llm_channels WHERE id = $1`, id,
	).Scan(&got); err != nil {
		t.Fatalf("read llm key id: %v", err)
	}
	if got != want {
		t.Fatalf("llm credential key id = %s; want %s", got, want)
	}
}

func assertLLMSecret(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	id uuid.UUID,
	wantKeyID string,
	wantPlain string,
) {
	t.Helper()
	var keyID string
	var ciphertext []byte
	if err := pool.QueryRow(ctx,
		`SELECT credential_key_id, credential_ciphertext FROM llm_channels WHERE id = $1`, id,
	).Scan(&keyID, &ciphertext); err != nil {
		t.Fatalf("read llm secret: %v", err)
	}
	if keyID != wantKeyID {
		t.Fatalf("llm key id = %s; want %s", keyID, wantKeyID)
	}
	plain, err := store.DecryptValue(secretstore.EncryptedValue{
		KeyID:      keyID,
		Ciphertext: ciphertext,
	}, secretstore.AssociatedData("llm_channel", id.String(), "api_key"))
	if err != nil {
		t.Fatalf("decrypt llm secret: %v", err)
	}
	if string(plain) != wantPlain {
		t.Fatalf("llm plaintext = %q; want %q", plain, wantPlain)
	}
}

func assertWebhookSecrets(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	id uuid.UUID,
	wantKeyID string,
) {
	t.Helper()
	var outer []byte
	if err := pool.QueryRow(ctx, `SELECT config FROM inbound_sources WHERE id = $1`, id).Scan(&outer); err != nil {
		t.Fatalf("read webhook config: %v", err)
	}
	assertCipherKeyID(t, outer, wantKeyID, "webhook outer")
	raw, err := store.DecryptValue(secretstore.EncryptedValue{Ciphertext: outer}, nil)
	if err != nil {
		t.Fatalf("decrypt webhook outer: %v", err)
	}
	var cfg webhook.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode webhook config: %v", err)
	}
	assertCipherKeyID(t, cfg.SecretCurrentEncrypted, wantKeyID, "webhook current")
	assertCipherKeyID(t, cfg.SecretPreviousEncrypted, wantKeyID, "webhook previous")
}

func assertEmailSecret(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *secretstore.TinkStore,
	id uuid.UUID,
	wantKeyID string,
) {
	t.Helper()
	var outer []byte
	if err := pool.QueryRow(ctx, `SELECT config FROM inbound_sources WHERE id = $1`, id).Scan(&outer); err != nil {
		t.Fatalf("read email config: %v", err)
	}
	assertCipherKeyID(t, outer, wantKeyID, "email outer")
	raw, err := store.DecryptValue(secretstore.EncryptedValue{Ciphertext: outer}, nil)
	if err != nil {
		t.Fatalf("decrypt email outer: %v", err)
	}
	var cfg email.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("decode email config: %v", err)
	}
	assertCipherKeyID(t, cfg.PasswordEncrypted, wantKeyID, "email password")
}

func assertCipherKeyID(t *testing.T, ciphertext []byte, want string, label string) {
	t.Helper()
	got, err := secretstore.KeyIDFromCiphertext(ciphertext)
	if err != nil {
		t.Fatalf("%s key id: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s key id = %s; want %s", label, got, want)
	}
}
