// SPDX-License-Identifier: Apache-2.0

package guardpolicy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePolicyName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty", "", ErrPolicyNameRequired},
		{"valid", "my-policy", nil},
		{"too_long", strings.Repeat("a", MaxPolicyNameBytes+1), ErrPolicyNameTooLong},
		{"max_length", strings.Repeat("a", MaxPolicyNameBytes), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePolicyName(tt.input)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func validatePolicyName(name string) error {
	if name == "" {
		return ErrPolicyNameRequired
	}
	if len(name) > MaxPolicyNameBytes {
		return ErrPolicyNameTooLong
	}
	return nil
}

func TestMaxPriority(t *testing.T) {
	t.Parallel()
	require.Equal(t, 10000, MaxPriority)
}

func TestMaxPolicies(t *testing.T) {
	t.Parallel()
	require.Equal(t, 50, MaxPolicies)
}
