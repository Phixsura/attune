// SPDX-License-Identifier: Apache-2.0

package secretrotation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	rotationrepo "github.com/Phixsura/attune/internal/repo/secretrotation"
)

func TestReencryptDryRunReportsWithoutWriting(t *testing.T) {
	oldStore, store, oldID, _ := rotationStores(t)
	channelID := uuid.New()
	oldSecret := encryptValue(t, oldStore, []byte("provider-key"), llmAAD(channelID))
	repo := ptrext.Of(fakeSecretRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      oldID,
			Ciphertext: oldSecret.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.commits) != 1 || repo.commits[0] {
		t.Fatalf("commit flags = %v; want single dry-run false", repo.commits)
	}
	if report.ValuesRotated != 1 || report.RowsUpdated != 0 {
		t.Fatalf("report = %+v; want one rotated value and no writes", report)
	}
	if len(report.Items) != 1 || report.Items[0].Action != "would_rewrite" {
		t.Fatalf("items = %+v; want dry-run rewrite item", report.Items)
	}
	if repo.llm[0].KeyID != oldID {
		t.Fatalf("dry run key id = %q; want unchanged %q", repo.llm[0].KeyID, oldID)
	}
}

func TestReencryptApplyRewritesLLMAndNestedInbound(t *testing.T) {
	oldStore, store, oldID, newID := rotationStores(t)
	channelID := uuid.New()
	inboundID := uuid.New()
	oldProviderKey := encryptValue(t, oldStore, []byte("provider-key"), llmAAD(channelID))
	oldWebhookSecret := encryptValue(t, oldStore, []byte("webhook-secret"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                1,
		SecretCurrentEncrypted: oldWebhookSecret.Ciphertext,
		HMACAlgo:               "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      oldID,
			Ciphertext: oldProviderKey.Ciphertext,
		}},
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.commits) != 1 || !repo.commits[0] {
		t.Fatalf("commit flags = %v; want single apply true", repo.commits)
	}
	if report.RowsUpdated != 2 || report.ValuesRotated != 2 {
		t.Fatalf("report = %+v; want LLM row plus inbound container rewritten", report)
	}
	if !reportHasAction(report, "container_rewritten") {
		t.Fatalf("items = %+v; want current outer container rewrite", report.Items)
	}
	if repo.llm[0].KeyID != newID {
		t.Fatalf("llm key id = %q; want %q", repo.llm[0].KeyID, newID)
	}
	gotProviderKey := decryptValue(t, store, repo.llm[0].Ciphertext, llmAAD(channelID))
	if string(gotProviderKey) != "provider-key" {
		t.Fatalf("provider key plaintext = %q", gotProviderKey)
	}
	outerPlaintext := decryptValue(t, store, repo.inbound[0].Config, nil)
	var webhook webhookConfigEnvelope
	if err := json.Unmarshal(outerPlaintext, &webhook); err != nil {
		t.Fatal(err)
	}
	assertCipherKeyID(t, webhook.SecretCurrentEncrypted, newID)
	gotWebhookSecret := decryptValue(t, store, webhook.SecretCurrentEncrypted, nil)
	if string(gotWebhookSecret) != "webhook-secret" {
		t.Fatalf("webhook secret plaintext = %q", gotWebhookSecret)
	}
}

func TestRetireKeyRefusesReferencesAndAppliesWhenClear(t *testing.T) {
	oldStore, store, oldID, _ := rotationStores(t)
	channelID := uuid.New()
	oldSecret := encryptValue(t, oldStore, []byte("provider-key"), llmAAD(channelID))
	repo := ptrext.Of(fakeSecretRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      oldID,
			Ciphertext: oldSecret.Ciphertext,
		}},
	})

	_, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: true})
	if !errors.Is(err, ErrKeyStillReferenced) {
		t.Fatalf("err = %v; want ErrKeyStillReferenced", err)
	}
	if len(repo.retired) != 0 {
		t.Fatalf("retired = %v; want none while references exist", repo.retired)
	}

	repo.llm = nil
	report, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.retired) != 1 || repo.retired[0] != oldID {
		t.Fatalf("retired = %v; want %q", repo.retired, oldID)
	}
	if len(report.Items) != 1 || report.Items[0].Action != "retired" {
		t.Fatalf("items = %+v; want retired item", report.Items)
	}
}

func TestDetectedKeyIDRejectsMetadataMismatch(t *testing.T) {
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	value, err := store.EncryptValue([]byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(nil, store).detectedKeyID(value.Ciphertext, value.KeyID+"0")
	if err == nil || !strings.Contains(err.Error(), "does not match ciphertext key_id") {
		t.Fatalf("err = %v; want key id mismatch", err)
	}
}

func TestDetectedKeyIDRecognizesLegacyInboundEnvelope(t *testing.T) {
	ciphertext := append([]byte{0x01, 0x00}, make([]byte, 30)...)
	keyID, err := New(nil, detectionStore{}).detectedKeyID(ciphertext, "")
	if err != nil {
		t.Fatal(err)
	}
	if keyID != secretstore.LegacyInboundKeyID {
		t.Fatalf("key id = %q; want %q", keyID, secretstore.LegacyInboundKeyID)
	}
}

func TestDetectedKeyIDPrefersKnownTinkKeyOverLegacyHeader(t *testing.T) {
	ciphertext := append([]byte{0x01, 0x00, 0x12, 0x34, 0x56}, make([]byte, 30)...)
	keyID, err := New(nil, detectionStore{
		keys: []secretstore.KeyMetadata{{KeyID: "1193046"}},
	}).detectedKeyID(ciphertext, "")
	if err != nil {
		t.Fatal(err)
	}
	if keyID != "1193046" {
		t.Fatalf("key id = %q; want Tink key id 1193046", keyID)
	}
}

func TestDetectedKeyIDDoesNotNeedStoreForNonLegacyTinkPrefix(t *testing.T) {
	ciphertext := append([]byte{0x01, 0x12, 0x34, 0x56, 0x78}, make([]byte, 30)...)
	keyID, err := New(nil, nil).detectedKeyID(ciphertext, "")
	if err != nil {
		t.Fatal(err)
	}
	if keyID != "305419896" {
		t.Fatalf("key id = %q; want Tink key id 305419896", keyID)
	}
}

type fakeSecretRepo struct {
	llm     []rotationrepo.LLMCredentialRow
	inbound []rotationrepo.InboundConfigRow
	commits []bool
	retired []string
	keys    map[string]bool
}

func (r *fakeSecretRepo) WithLockedSecrets(
	ctx context.Context,
	commit bool,
	fn func(context.Context, rotationrepo.Queries) error,
) error {
	r.commits = append(r.commits, commit)
	return fn(ctx, fakeSecretQueries{repo: r})
}

type fakeSecretQueries struct {
	repo *fakeSecretRepo
}

func (q fakeSecretQueries) CheckKeyExists(_ context.Context, keyID string) error {
	if q.repo.keys == nil {
		return nil
	}
	if q.repo.keys[keyID] {
		return nil
	}
	return rotationrepo.ErrSecretKeyNotFound
}

func (q fakeSecretQueries) ListLLMCredentialsForUpdate(
	context.Context,
) ([]rotationrepo.LLMCredentialRow, error) {
	out := make([]rotationrepo.LLMCredentialRow, len(q.repo.llm))
	copy(out, q.repo.llm)
	return out, nil
}

func (q fakeSecretQueries) UpdateLLMCredential(
	_ context.Context,
	id uuid.UUID,
	keyID string,
	ciphertext []byte,
) error {
	for i, row := range q.repo.llm {
		if row.ID == id {
			q.repo.llm[i].KeyID = keyID
			q.repo.llm[i].Ciphertext = append([]byte(nil), ciphertext...)
			return nil
		}
	}
	return fmt.Errorf("llm row not found: %s", id)
}

func (q fakeSecretQueries) ListInboundConfigsForUpdate(
	context.Context,
) ([]rotationrepo.InboundConfigRow, error) {
	out := make([]rotationrepo.InboundConfigRow, len(q.repo.inbound))
	copy(out, q.repo.inbound)
	return out, nil
}

func (q fakeSecretQueries) UpdateInboundConfig(
	_ context.Context,
	id uuid.UUID,
	config []byte,
) error {
	for i, row := range q.repo.inbound {
		if row.ID == id {
			q.repo.inbound[i].Config = append([]byte(nil), config...)
			return nil
		}
	}
	return fmt.Errorf("inbound row not found: %s", id)
}

func (q fakeSecretQueries) RetireKey(_ context.Context, keyID string) error {
	q.repo.retired = append(q.repo.retired, keyID)
	return nil
}

func rotationStores(t *testing.T) (*secretstore.TinkStore, *secretstore.TinkStore, string, string) {
	t.Helper()
	oldRaw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	oldStore := mustStore(t, oldRaw)
	combinedRaw, newID, err := secretstore.AddAES256GCMKeyToKeysetJSON(oldRaw, true)
	if err != nil {
		t.Fatal(err)
	}
	return oldStore, mustStore(t, combinedRaw), oldStore.PrimaryKeyID(), newID
}

func mustStore(t *testing.T, raw string) *secretstore.TinkStore {
	t.Helper()
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func encryptValue(
	t *testing.T,
	store *secretstore.TinkStore,
	plaintext []byte,
	aad []byte,
) secretstore.EncryptedValue {
	t.Helper()
	value, err := store.EncryptValue(plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func decryptValue(t *testing.T, store *secretstore.TinkStore, ciphertext []byte, aad []byte) []byte {
	t.Helper()
	plaintext, err := store.DecryptValue(secretstore.EncryptedValue{Ciphertext: ciphertext}, aad)
	if err != nil {
		t.Fatal(err)
	}
	return plaintext
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func llmAAD(id uuid.UUID) []byte {
	return secretstore.AssociatedData("llm_channel", id.String(), "api_key")
}

func assertCipherKeyID(t *testing.T, ciphertext []byte, want string) {
	t.Helper()
	got, err := secretstore.KeyIDFromCiphertext(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ciphertext key id = %q; want %q", got, want)
	}
}

func reportHasAction(report *Report, action string) bool {
	for _, item := range report.Items {
		if item.Action == action {
			return true
		}
	}
	return false
}

type detectionStore struct {
	keys []secretstore.KeyMetadata
}

func (s detectionStore) PrimaryKeyID() string { return "" }

func (s detectionStore) EncryptValue(_, _ []byte) (secretstore.EncryptedValue, error) {
	return secretstore.EncryptedValue{}, errors.New("unexpected encrypt")
}

func (s detectionStore) DecryptValue(secretstore.EncryptedValue, []byte) ([]byte, error) {
	return nil, errors.New("unexpected decrypt")
}

func (s detectionStore) Keys() []secretstore.KeyMetadata { return s.keys }
