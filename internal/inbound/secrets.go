// SPDX-License-Identifier: Apache-2.0

package inbound

// SecretStore encrypts small adapter secrets before they are persisted in
// inbound_sources. The production implementation is the shared Tink-backed
// infra/secretstore.TinkStore.
type SecretStore interface {
	Encrypt(plaintext []byte) (ciphertext []byte, err error)
	Decrypt(ciphertext []byte) (plaintext []byte, err error)
}

type PrimaryKeyStore interface {
	PrimaryKeyID() string
}

func PrimaryKeyID(secrets SecretStore) string {
	if keyed, ok := secrets.(PrimaryKeyStore); ok {
		return keyed.PrimaryKeyID()
	}
	return ""
}
