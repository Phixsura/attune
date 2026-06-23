package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckPgvectorVersion(t *testing.T) {
	t.Parallel()

	// Note: checkPgvectorVersion returns nil for unparseable versions
	// (assumes unknown format is new enough). Only errors on confirmed old versions.
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "valid 0.5.0",
			version: "0.5.0",
			wantErr: false,
		},
		{
			name:    "valid 0.6.0",
			version: "0.6.0",
			wantErr: false,
		},
		{
			name:    "valid 0.7.0",
			version: "0.7.0",
			wantErr: false,
		},
		{
			name:    "valid 1.0.0",
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "too old 0.4.0",
			version: "0.4.0",
			wantErr: true,
		},
		{
			name:    "too old 0.4.9",
			version: "0.4.9",
			wantErr: true,
		},
		{
			name:    "invalid format assumes ok",
			version: "invalid",
			wantErr: false, // Unparseable = assume new enough
		},
		{
			name:    "empty assumes ok",
			version: "",
			wantErr: false, // Unparseable = assume new enough
		},
		{
			name:    "partial version assumes ok",
			version: "0.5",
			wantErr: false, // Has major.minor, but 0.5 >= 0.5 = ok
		},
		{
			name:    "major only assumes ok",
			version: "1",
			wantErr: false, // Unparseable (< 2 parts) = assume new enough
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkPgvectorVersion(tt.version)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckPgvectorVersion_BoundaryConditions(t *testing.T) {
	t.Parallel()

	// Exactly at minimum version
	err := checkPgvectorVersion("0.5.0")
	require.NoError(t, err)

	// Just below minimum
	err = checkPgvectorVersion("0.4.99")
	require.Error(t, err)

	// Large version numbers
	err = checkPgvectorVersion("10.20.30")
	require.NoError(t, err)
}
