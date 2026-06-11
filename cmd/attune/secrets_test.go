// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/secretstore"
)

func TestRunSecretsGenerateKeysetOutputsOnlyKeysetJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runSecretsGenerateKeyset(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		t.Fatalf("output is not a bare JSON object: %q", out)
	}
	if _, err := secretstore.NewTinkStoreFromJSON(trimmed); err != nil {
		t.Fatalf("output is not a usable keyset: %v", err)
	}
	if strings.Contains(out, "paste") || strings.Contains(out, "attune secrets") {
		t.Fatalf("output includes prompt text: %q", out)
	}
}

func TestRunSecretsKeysetInfoOutputsMetadataOnly(t *testing.T) {
	raw := writeSecretsConfig(t)
	store, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretsKeysetInfo(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "keyData") || strings.Contains(out, "keyValue") {
		t.Fatalf("keyset-info leaked key material: %q", out)
	}
	var info struct {
		PrimaryKeyID string                    `json:"primary_key_id"`
		Keys         []secretstore.KeyMetadata `json:"keys"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}
	if info.PrimaryKeyID != store.PrimaryKeyID() || len(info.Keys) != len(store.Keys()) {
		t.Fatalf("info = %+v; want primary %s and %d keys",
			info, store.PrimaryKeyID(), len(store.Keys()))
	}
}

func TestRunSecretsAddKeyOutputsUsableUpdatedKeyset(t *testing.T) {
	raw := writeSecretsConfig(t)
	before, err := secretstore.NewTinkStoreFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSecretsAddKey(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := secretstore.NewTinkStoreFromJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Keys()) != len(before.Keys())+1 {
		t.Fatalf("keys after add = %d; want %d", len(after.Keys()), len(before.Keys())+1)
	}
	if after.PrimaryKeyID() != before.PrimaryKeyID() {
		t.Fatalf("primary key changed to %s; want %s", after.PrimaryKeyID(), before.PrimaryKeyID())
	}
}

func TestRunSecretsRejectsUnknownVerb(t *testing.T) {
	if err := runSecrets([]string{"unknown"}); err == nil {
		t.Fatal("expected usage error")
	}
}

func writeSecretsConfig(t *testing.T) string {
	t.Helper()
	raw, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	escaped := strings.ReplaceAll(raw, "\n", "\n    ")
	body := "database:\n" +
		"  url: \"postgres://attune@localhost:5432/attune?sslmode=disable\"\n" +
		"secrets:\n" +
		"  tink_keyset: |\n" +
		"    " + escaped + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	config.SetPath(path)
	t.Cleanup(func() { config.SetPath("") })
	return raw
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	callErr := fn()
	closeErr := writer.Close()
	os.Stdout = original
	raw, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return string(raw), callErr
}
