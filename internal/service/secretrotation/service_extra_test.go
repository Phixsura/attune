// SPDX-License-Identifier: Apache-2.0

package secretrotation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	rotationrepo "github.com/Phixsura/attune/internal/repo/secretrotation"
)

// ---------------------------------------------------------------------------
// RetireKey edge cases
// ---------------------------------------------------------------------------

func TestRetireKey_EmptyKeyID(t *testing.T) {
	t.Parallel()
	svc := New(ptrext.Of(fakeSecretRepo{}), detectionStore{})
	_, err := svc.RetireKey(context.Background(), "  ", RetireOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "key id is required")
}

func TestRetireKey_PrimaryKeyRefused(t *testing.T) {
	t.Parallel()
	store := configurableStore{primaryID: "primary-999"}
	svc := New(ptrext.Of(fakeSecretRepo{}), store)
	_, err := svc.RetireKey(context.Background(), "primary-999", RetireOptions{})
	require.ErrorIs(t, err, ErrPrimaryKeyRetire)
}

func TestRetireKey_DryRunWouldRetire(t *testing.T) {
	t.Parallel()
	_, store, oldID, _ := rotationStores(t)
	repo := ptrext.Of(fakeSecretRepo{})
	report, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: false})
	require.NoError(t, err)
	require.Len(t, report.Items, 1)
	require.Equal(t, "would_retire", report.Items[0].Action)
	require.Equal(t, "secret_key_registry", report.Items[0].Location)
	require.Equal(t, oldID, report.Items[0].RowID)
	require.Empty(t, repo.retired, "dry-run must not retire")
}

func TestRetireKey_CheckKeyExistsError(t *testing.T) {
	t.Parallel()
	_, store, oldID, _ := rotationStores(t)
	repo := ptrext.Of(fakeSecretRepo{
		keys: map[string]bool{},
	})
	_, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: true})
	require.ErrorIs(t, err, rotationrepo.ErrSecretKeyNotFound)
}

func TestRetireKey_LLMCredentialListError(t *testing.T) {
	t.Parallel()
	_, store, oldID, _ := rotationStores(t)
	repo := ptrext.Of(errInjectRepo{
		llmListErr: errors.New("db timeout"),
	})
	_, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "db timeout")
}

func TestRetireKey_InboundConfigListError(t *testing.T) {
	t.Parallel()
	_, store, oldID, _ := rotationStores(t)
	repo := ptrext.Of(errInjectRepo{
		inboundListErr: errors.New("inbound fetch failed"),
	})
	_, err := New(repo, store).RetireKey(context.Background(), oldID, RetireOptions{Apply: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound fetch failed")
}

// ---------------------------------------------------------------------------
// Reencrypt edge cases
// ---------------------------------------------------------------------------

func TestReencrypt_LLMCredentialListError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	repo := ptrext.Of(errInjectRepo{
		llmListErr: errors.New("list failed"),
	})
	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "list failed")
}

func TestReencrypt_InboundConfigListError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	repo := ptrext.Of(errInjectRepo{
		inboundListErr: errors.New("inbound list failed"),
	})
	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound list failed")
}

func TestReencrypt_FromKeyIDFiltersNonMatching(t *testing.T) {
	t.Parallel()
	oldStore, store, oldID, _ := rotationStores(t)
	channelID := uuid.New()
	oldSecret := encryptValue(t, oldStore, []byte("key"), llmAAD(channelID))
	repo := ptrext.Of(fakeSecretRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      oldID,
			Ciphertext: oldSecret.Ciphertext,
		}},
	})
	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{
		FromKeyID: "non-existent-key",
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.ValuesScanned)
	require.Equal(t, 1, report.ValuesFiltered)
	require.Equal(t, 0, report.ValuesRotated)
}

func TestReencrypt_LLMUpdateError(t *testing.T) {
	t.Parallel()
	oldStore, store, oldID, _ := rotationStores(t)
	channelID := uuid.New()
	oldSecret := encryptValue(t, oldStore, []byte("key"), llmAAD(channelID))
	repo := ptrext.Of(errInjectRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      oldID,
			Ciphertext: oldSecret.Ciphertext,
		}},
		llmUpdateErr: errors.New("update failed"),
	})
	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update failed")
}

// ---------------------------------------------------------------------------
// Email config rotation (entirely uncovered)
// ---------------------------------------------------------------------------

func TestReencrypt_RotatesEmailPasswordEncrypted(t *testing.T) {
	t.Parallel()
	oldStore, store, _, newID := rotationStores(t)
	inboundID := uuid.New()

	oldPassword := encryptValue(t, oldStore, []byte("imap-password"), nil)
	emailPlaintext := mustJSON(t, emailConfigEnvelope{
		Version:           1,
		Host:              "imap.example.com",
		Port:              993,
		TLS:               true,
		Username:          "user@example.com",
		PasswordEncrypted: oldPassword.Ciphertext,
		Folder:            "INBOX",
		StartFrom:         "latest",
		AfterIngest:       "archive",
	})
	currentOuter := encryptValue(t, store, emailPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "email",
			Config:  currentOuter.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 1, report.ValuesRotated)
	require.GreaterOrEqual(t, report.RowsUpdated, 1)

	outerPlaintext := decryptValue(t, store, repo.inbound[0].Config, nil)
	var cfg emailConfigEnvelope
	require.NoError(t, json.Unmarshal(outerPlaintext, &cfg))
	assertCipherKeyID(t, cfg.PasswordEncrypted, newID)
	gotPassword := decryptValue(t, store, cfg.PasswordEncrypted, nil)
	require.Equal(t, "imap-password", string(gotPassword))
}

func TestReencrypt_EmailPasswordAlreadyCurrent(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()

	currentPassword := encryptValue(t, store, []byte("imap-password"), nil)
	emailPlaintext := mustJSON(t, emailConfigEnvelope{
		Version:           1,
		Host:              "imap.example.com",
		Port:              993,
		TLS:               true,
		Username:          "user@example.com",
		PasswordEncrypted: currentPassword.Ciphertext,
		Folder:            "INBOX",
	})
	currentOuter := encryptValue(t, store, emailPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "email",
			Config:  currentOuter.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 0, report.ValuesRotated)
}

func TestReencrypt_EmailConfigUnmarshalError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()

	badPlaintext := []byte("not-valid-json{{{")
	currentOuter := encryptValue(t, store, badPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "email",
			Config:  currentOuter.Ciphertext,
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode email config")
}

// ---------------------------------------------------------------------------
// Webhook config: SecretPreviousEncrypted rotation
// ---------------------------------------------------------------------------

func TestReencrypt_WebhookWithPreviousSecret(t *testing.T) {
	t.Parallel()
	oldStore, store, _, newID := rotationStores(t)
	inboundID := uuid.New()

	oldCurrent := encryptValue(t, oldStore, []byte("current-secret"), nil)
	oldPrevious := encryptValue(t, oldStore, []byte("previous-secret"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                 1,
		SecretCurrentEncrypted:  oldCurrent.Ciphertext,
		SecretPreviousEncrypted: oldPrevious.Ciphertext,
		HMACAlgo:                "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 2, report.ValuesRotated, "both current and previous secrets rotated")

	outerPlaintext := decryptValue(t, store, repo.inbound[0].Config, nil)
	var cfg webhookConfigEnvelope
	require.NoError(t, json.Unmarshal(outerPlaintext, &cfg))
	assertCipherKeyID(t, cfg.SecretCurrentEncrypted, newID)
	assertCipherKeyID(t, cfg.SecretPreviousEncrypted, newID)
	require.Equal(t, "current-secret", string(decryptValue(t, store, cfg.SecretCurrentEncrypted, nil)))
	require.Equal(t, "previous-secret", string(decryptValue(t, store, cfg.SecretPreviousEncrypted, nil)))
}

func TestReencrypt_WebhookUnmarshalError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()

	currentOuter := encryptValue(t, store, []byte("{invalid-json"), nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode webhook config")
}

// ---------------------------------------------------------------------------
// Unknown inbound channel
// ---------------------------------------------------------------------------

func TestReencrypt_UnknownChannelAddsWarning(t *testing.T) {
	t.Parallel()
	oldStore, store, _, _ := rotationStores(t)
	inboundID := uuid.New()

	somePlaintext := mustJSON(t, map[string]string{"key": "value"})
	outerOld := encryptValue(t, oldStore, somePlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "unknown-channel",
			Config:  outerOld.Ciphertext,
		}},
	})

	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.NoError(t, err)
	require.Len(t, report.Warnings, 1)
	require.Contains(t, report.Warnings[0].Message, "unknown inbound channel")
	require.Equal(t, inboundID.String(), report.Warnings[0].RowID)
}

// ---------------------------------------------------------------------------
// InboundConfig: outer decrypt error
// ---------------------------------------------------------------------------

func TestReencrypt_InboundOuterDecryptError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  []byte{0x01, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt inbound config")
}

func TestReencrypt_InboundUpdateError(t *testing.T) {
	t.Parallel()
	oldStore, store, _, _ := rotationStores(t)
	inboundID := uuid.New()

	oldWebhookSecret := encryptValue(t, oldStore, []byte("secret"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                1,
		SecretCurrentEncrypted: oldWebhookSecret.Ciphertext,
		HMACAlgo:               "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(errInjectRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
		inboundUpdateErr: errors.New("inbound update failed"),
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "inbound update failed")
}

// ---------------------------------------------------------------------------
// rotateValue error paths
// ---------------------------------------------------------------------------

func TestRotateValue_DecryptError(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(failingStore{
		primaryID:  "primary-1",
		decryptErr: errors.New("decrypt boom"),
	})
	svc := New(ptrext.Of(fakeSecretRepo{}), store)
	report := svc.newReport(ReencryptOptions{})

	// ciphertext with valid Tink prefix; key id != primary so rotateValue
	// calls decrypt.
	ciphertext := []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0xDE, 0xAD}
	_, _, err := svc.rotateValue(ciphertext, "1", nil, "test.location", "row-1", ReencryptOptions{}, report)
	require.Error(t, err)
	require.Contains(t, err.Error(), "test.location row=row-1")
}

func TestRotateValue_EncryptError(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(failingStore{
		primaryID:  "primary-1",
		encryptErr: errors.New("encrypt boom"),
		plaintext:  []byte("decrypted"),
	})
	svc := New(ptrext.Of(fakeSecretRepo{}), store)
	report := svc.newReport(ReencryptOptions{})

	ciphertext := []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0xDE, 0xAD}
	_, _, err := svc.rotateValue(ciphertext, "1", nil, "test.location", "row-1", ReencryptOptions{}, report)
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypt")
}

func TestRotateValue_DetectedKeyIDError(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(failingStore{primaryID: "primary-1"})
	svc := New(ptrext.Of(fakeSecretRepo{}), store)
	report := svc.newReport(ReencryptOptions{})

	// Ciphertext too short and wrong prefix: fails KeyIDFromCiphertext.
	_, _, err := svc.rotateValue([]byte{0x02, 0x03}, "", nil, "test.location", "row-1", ReencryptOptions{}, report)
	require.Error(t, err)
	require.Contains(t, err.Error(), "test.location row=row-1")
}

// ---------------------------------------------------------------------------
// detectedKeyID edge cases
// ---------------------------------------------------------------------------

func TestDetectedKeyID_StoredKeyIDWithNoTinkPrefix(t *testing.T) {
	t.Parallel()
	svc := New(nil, detectionStore{})
	// Non-Tink prefix + storedKeyID set: reaches "storedKeyID != ''" + err path.
	_, err := svc.detectedKeyID([]byte{0x02, 0x03}, "stored-key-1")
	require.Error(t, err)
}

func TestDetectedKeyID_NonLegacyNonTinkError(t *testing.T) {
	t.Parallel()
	svc := New(nil, detectionStore{})
	// Neither legacy nor Tink, no stored key: hits final "return '', err".
	_, err := svc.detectedKeyID([]byte{0x02, 0x03, 0x04}, "")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Reencrypt: inbound nested re-encrypt error on outer container
// ---------------------------------------------------------------------------

func TestReencrypt_InboundNestedChangedOuterEncryptError(t *testing.T) {
	t.Parallel()
	// oldStore encrypts the inner secret; combined store has a new primary.
	// The outerEncryptFailStore wraps the combined store so inner re-encrypt
	// succeeds (call 1) but outer container re-encrypt fails (call 2).
	oldStore, combinedStore, _, _ := rotationStores(t)

	oldWebhookSecret := encryptValue(t, oldStore, []byte("secret"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                1,
		SecretCurrentEncrypted: oldWebhookSecret.Ciphertext,
		HMACAlgo:               "sha256",
	})

	// Outer envelope encrypted with combinedStore's new primary key, so the
	// outer rotateValue sees it as current (no outer rotation needed). But
	// the nested webhook secret was encrypted with oldStore, so inner
	// rotation fires, triggering container re-encrypt at line 252.
	outerCipher := encryptValue(t, combinedStore, webhookPlaintext, nil)
	wrappedStore := ptrext.Of(outerEncryptFailStore{
		inner: combinedStore,
	})
	inboundID := uuid.New()
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  outerCipher.Ciphertext,
		}},
	})

	_, err := New(repo, wrappedStore).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "encrypt inbound config")
}

// ---------------------------------------------------------------------------
// hasLocalKey with valid store
// ---------------------------------------------------------------------------

func TestHasLocalKey_Found(t *testing.T) {
	t.Parallel()
	store := detectionStore{
		keys: []secretstore.KeyMetadata{{KeyID: "key-abc"}},
	}
	svc := New(nil, store)
	require.True(t, svc.hasLocalKey("key-abc"))
}

func TestHasLocalKey_NotFound(t *testing.T) {
	t.Parallel()
	store := detectionStore{
		keys: []secretstore.KeyMetadata{{KeyID: "key-abc"}},
	}
	svc := New(nil, store)
	require.False(t, svc.hasLocalKey("key-xyz"))
}

// ---------------------------------------------------------------------------
// test doubles
// ---------------------------------------------------------------------------

// configurableStore is a minimal Store with a settable primary key id.
type configurableStore struct {
	primaryID string
	keys      []secretstore.KeyMetadata
}

func (s configurableStore) PrimaryKeyID() string { return s.primaryID }

func (s configurableStore) EncryptValue(_, _ []byte) (secretstore.EncryptedValue, error) {
	return secretstore.EncryptedValue{}, errors.New("unexpected encrypt")
}

func (s configurableStore) DecryptValue(secretstore.EncryptedValue, []byte) ([]byte, error) {
	return nil, errors.New("unexpected decrypt")
}

func (s configurableStore) Keys() []secretstore.KeyMetadata { return s.keys }

// failingStore is a Store implementation that can selectively fail encrypt or
// decrypt operations.
type failingStore struct {
	primaryID  string
	decryptErr error
	encryptErr error
	plaintext  []byte
}

func (s *failingStore) PrimaryKeyID() string { return s.primaryID }

func (s *failingStore) EncryptValue(plaintext, _ []byte) (secretstore.EncryptedValue, error) {
	if s.encryptErr != nil {
		return secretstore.EncryptedValue{}, s.encryptErr
	}
	return secretstore.EncryptedValue{KeyID: s.primaryID, Ciphertext: plaintext}, nil
}

func (s *failingStore) DecryptValue(_ secretstore.EncryptedValue, _ []byte) ([]byte, error) {
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	return s.plaintext, nil
}

func (s *failingStore) Keys() []secretstore.KeyMetadata { return nil }

// outerEncryptFailStore delegates to an inner TinkStore but fails on the
// second EncryptValue call. This simulates the outer container re-encryption
// failure path in rotateInboundConfigs.
type outerEncryptFailStore struct {
	inner     *secretstore.TinkStore
	callCount int
}

func (s *outerEncryptFailStore) PrimaryKeyID() string { return s.inner.PrimaryKeyID() }

func (s *outerEncryptFailStore) EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error) {
	s.callCount++
	// Let the first encrypt call succeed (inner rotation), fail on the
	// second (outer container re-encrypt).
	if s.callCount > 1 {
		return secretstore.EncryptedValue{}, errors.New("encrypt inbound config outer boom")
	}
	return s.inner.EncryptValue(plaintext, aad)
}

func (s *outerEncryptFailStore) DecryptValue(value secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	return s.inner.DecryptValue(value, aad)
}

func (s *outerEncryptFailStore) Keys() []secretstore.KeyMetadata { return s.inner.Keys() }

// errInjectRepo is a Repo implementation where query methods can return
// injected errors for testing error paths.
type errInjectRepo struct {
	llm              []rotationrepo.LLMCredentialRow
	inbound          []rotationrepo.InboundConfigRow
	llmListErr       error
	llmUpdateErr     error
	inboundListErr   error
	inboundUpdateErr error
	commits          []bool
}

func (r *errInjectRepo) WithLockedSecrets(
	ctx context.Context,
	commit bool,
	fn func(context.Context, rotationrepo.Queries) error,
) error {
	r.commits = append(r.commits, commit)
	return fn(ctx, errInjectQueries{repo: r})
}

type errInjectQueries struct {
	repo *errInjectRepo
}

func (q errInjectQueries) CheckKeyExists(context.Context, string) error {
	return nil
}

func (q errInjectQueries) ListLLMCredentialsForUpdate(
	context.Context,
) ([]rotationrepo.LLMCredentialRow, error) {
	if q.repo.llmListErr != nil {
		return nil, q.repo.llmListErr
	}
	out := make([]rotationrepo.LLMCredentialRow, len(q.repo.llm))
	copy(out, q.repo.llm)
	return out, nil
}

func (q errInjectQueries) UpdateLLMCredential(
	_ context.Context,
	_ uuid.UUID,
	_ string,
	_ []byte,
) error {
	if q.repo.llmUpdateErr != nil {
		return q.repo.llmUpdateErr
	}
	return nil
}

func (q errInjectQueries) ListInboundConfigsForUpdate(
	context.Context,
) ([]rotationrepo.InboundConfigRow, error) {
	if q.repo.inboundListErr != nil {
		return nil, q.repo.inboundListErr
	}
	out := make([]rotationrepo.InboundConfigRow, len(q.repo.inbound))
	copy(out, q.repo.inbound)
	return out, nil
}

func (q errInjectQueries) UpdateInboundConfig(
	_ context.Context,
	_ uuid.UUID,
	_ []byte,
) error {
	if q.repo.inboundUpdateErr != nil {
		return q.repo.inboundUpdateErr
	}
	return nil
}

func (q errInjectQueries) RetireKey(context.Context, string) error {
	return nil
}

// Compile-time interface checks.
var (
	_ Store = configurableStore{}
	_ Store = (*failingStore)(nil)
	_ Store = (*outerEncryptFailStore)(nil)
	_ Repo  = (*errInjectRepo)(nil)
)

// ---------------------------------------------------------------------------
// rotateLLMCredentials: rotateValue error for bad ciphertext
// ---------------------------------------------------------------------------

func TestReencrypt_LLMBadCiphertextRotateValueError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	channelID := uuid.New()
	repo := ptrext.Of(fakeSecretRepo{
		llm: []rotationrepo.LLMCredentialRow{{
			ID:         channelID,
			KeyID:      "",
			Ciphertext: []byte{0x02, 0x03},
		}},
	})
	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "llm_channels.credential_ciphertext")
}

// ---------------------------------------------------------------------------
// rotateInboundConfigs: rotateValue error on outer config
// ---------------------------------------------------------------------------

func TestReencrypt_InboundOuterRotateValueError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	// Valid plaintext but outer ciphertext with non-Tink non-legacy prefix.
	somePlaintext := mustJSON(t, map[string]string{"key": "value"})
	outerBad := encryptValue(t, store, somePlaintext, nil)
	// Corrupt the ciphertext prefix so detectedKeyID fails on the outer
	// rotateValue call. Replace the Tink prefix 0x01 with 0x02.
	outerBad.Ciphertext[0] = 0x02
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "unknown-channel",
			Config:  outerBad.Ciphertext,
		}},
	})
	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt inbound config")
}

// ---------------------------------------------------------------------------
// rotateWebhookConfig: rotateValue error on secret_current_encrypted
// ---------------------------------------------------------------------------

func TestReencrypt_WebhookCurrentSecretRotateValueError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	// Bad ciphertext inside the webhook config that will make detectedKeyID
	// fail.
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                1,
		SecretCurrentEncrypted: []byte{0x02, 0x03},
		HMACAlgo:               "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret_current_encrypted")
}

// ---------------------------------------------------------------------------
// rotateWebhookConfig: rotateValue error on secret_previous_encrypted
// ---------------------------------------------------------------------------

func TestReencrypt_WebhookPreviousSecretRotateValueError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	// Current secret is valid and uses the primary key (no rotation needed).
	// Previous secret has bad ciphertext.
	validCurrent := encryptValue(t, store, []byte("current"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                 1,
		SecretCurrentEncrypted:  validCurrent.Ciphertext,
		SecretPreviousEncrypted: []byte{0x02, 0x03},
		HMACAlgo:                "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret_previous_encrypted")
}

// ---------------------------------------------------------------------------
// rotateWebhookConfig: no changes path
// ---------------------------------------------------------------------------

func TestReencrypt_WebhookCurrentSecretAlreadyCurrent(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	validCurrent := encryptValue(t, store, []byte("current"), nil)
	webhookPlaintext := mustJSON(t, webhookConfigEnvelope{
		Version:                1,
		SecretCurrentEncrypted: validCurrent.Ciphertext,
		HMACAlgo:               "sha256",
	})
	currentOuter := encryptValue(t, store, webhookPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "webhook",
			Config:  currentOuter.Ciphertext,
		}},
	})
	report, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{Apply: true})
	require.NoError(t, err)
	require.Equal(t, 0, report.ValuesRotated)
}

// ---------------------------------------------------------------------------
// rotateEmailConfig: rotateValue error on password_encrypted
// ---------------------------------------------------------------------------

func TestReencrypt_EmailPasswordRotateValueError(t *testing.T) {
	t.Parallel()
	_, store, _, _ := rotationStores(t)
	inboundID := uuid.New()
	emailPlaintext := mustJSON(t, emailConfigEnvelope{
		Version:           1,
		Host:              "imap.example.com",
		Port:              993,
		PasswordEncrypted: []byte{0x02, 0x03},
	})
	currentOuter := encryptValue(t, store, emailPlaintext, nil)
	repo := ptrext.Of(fakeSecretRepo{
		inbound: []rotationrepo.InboundConfigRow{{
			ID:      inboundID,
			Channel: "email",
			Config:  currentOuter.Ciphertext,
		}},
	})

	_, err := New(repo, store).Reencrypt(context.Background(), ReencryptOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "password_encrypted")
}

// ---------------------------------------------------------------------------
// decryptValue: detectedKeyID error when keyID is empty
// ---------------------------------------------------------------------------

func TestDecryptValue_EmptyKeyIDDetectionError(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(failingStore{primaryID: "primary-1"})
	svc := New(ptrext.Of(fakeSecretRepo{}), store)
	// Non-Tink, non-legacy ciphertext with empty keyID triggers
	// detectedKeyID error inside decryptValue.
	_, err := svc.decryptValue([]byte{0x02, 0x03}, "", nil)
	require.Error(t, err)
}
