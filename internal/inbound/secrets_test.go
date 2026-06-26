// SPDX-License-Identifier: Apache-2.0

package inbound_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/inbound"
)

// stubSecrets implements SecretStore without PrimaryKeyStore.
type stubSecrets struct{}

func (stubSecrets) Encrypt(b []byte) ([]byte, error) { return b, nil }
func (stubSecrets) Decrypt(b []byte) ([]byte, error) { return b, nil }

// stubKeyedSecrets implements both SecretStore and PrimaryKeyStore.
type stubKeyedSecrets struct{ stubSecrets }

func (stubKeyedSecrets) PrimaryKeyID() string { return "key-123" }

func TestPrimaryKeyID_WithPrimaryKeyStore(t *testing.T) {
	t.Parallel()
	got := inbound.PrimaryKeyID(stubKeyedSecrets{})
	require.Equal(t, "key-123", got)
}

func TestPrimaryKeyID_WithoutPrimaryKeyStore(t *testing.T) {
	t.Parallel()
	got := inbound.PrimaryKeyID(stubSecrets{})
	require.Equal(t, "", got)
}
