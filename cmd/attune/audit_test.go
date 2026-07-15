// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/canonicaljson"
)

func TestVerifyExport_ValidUnsigned(t *testing.T) {
	archive := buildTestArchive(t, 3, nil)
	path := writeTemp(t, archive)
	if err := runVerifyExport([]string{path}); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifyExport_ValidSigned(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 42)
	}
	archive := buildTestArchive(t, 5, seed)
	path := writeTemp(t, archive)

	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	pubHex := hex.EncodeToString(pubKey)

	if err := runVerifyExport([]string{"--public-key", pubHex, path}); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestVerifyExport_TamperedEvent(t *testing.T) {
	archive := buildTamperedArchive(t)
	path := writeTemp(t, archive)
	err := runVerifyExport([]string{path})
	if err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestVerifyExport_WrongKey(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	archive := buildTestArchive(t, 2, seed)
	path := writeTemp(t, archive)

	wrongKey := make([]byte, ed25519.PublicKeySize)
	for i := range wrongKey {
		wrongKey[i] = 0xff
	}
	err := runVerifyExport([]string{"--public-key", hex.EncodeToString(wrongKey), path})
	if err == nil {
		t.Fatal("expected signature failure")
	}
}

func TestVerifyExport_EmptyArchive(t *testing.T) {
	archive := buildTestArchive(t, 0, nil)
	path := writeTemp(t, archive)
	if err := runVerifyExport([]string{path}); err != nil {
		t.Fatalf("verify empty: %v", err)
	}
}

func TestGenerateSigningKey(t *testing.T) {
	if err := runGenerateSigningKey(); err != nil {
		t.Fatalf("generate: %v", err)
	}
}

func TestExportPublicKey(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	if err := runExportPublicKey([]string{"--signing-key", hex.EncodeToString(seed)}); err != nil {
		t.Fatalf("export-public-key: %v", err)
	}
}

func TestExportPublicKey_NoArg(t *testing.T) {
	if err := runExportPublicKey(nil); err == nil {
		t.Fatal("expected error with no args")
	}
}

func TestRunAuditDispatch(t *testing.T) {
	t.Run("missing subcommand", func(t *testing.T) {
		err := runAudit(nil)
		if err == nil {
			t.Fatal("expected error for missing subcommand")
		}
	})

	t.Run("generate signing key", func(t *testing.T) {
		if err := runAudit([]string{"generate-signing-key"}); err != nil {
			t.Fatalf("runAudit(generate-signing-key): %v", err)
		}
	})

	t.Run("export public key dispatches", func(t *testing.T) {
		if err := runAudit([]string{"export-public-key"}); err == nil {
			t.Fatal("expected usage error from export-public-key dispatch without args")
		}
	})

	t.Run("unknown subcommand", func(t *testing.T) {
		err := runAudit([]string{"bogus"})
		if err == nil {
			t.Fatal("expected error for unknown subcommand")
		}
	})
}

type testChainEvent struct {
	Sequence  int             `json:"sequence"`
	Event     json.RawMessage `json:"event"`
	EventHash string          `json:"event_hash"`
	PrevHash  string          `json:"prev_hash"`
	ChainHash string          `json:"chain_hash"`
}

type testManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type testManifestIntegrity struct {
	Algorithm      string `json:"algorithm"`
	ChainAlgorithm string `json:"chain_algorithm"`
	ChainHash      string `json:"chain_hash"`
	EventCount     int    `json:"event_count"`
}

type testManifestSigning struct {
	Algorithm            string `json:"algorithm"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

type testManifest struct {
	Format   string `json:"format"`
	ExportID string `json:"export_id"`
	TenantID string `json:"tenant_id"`
	Stats    struct {
		TotalEvents int `json:"total_events"`
	} `json:"stats"`
	Files     []testManifestFile    `json:"files"`
	Integrity testManifestIntegrity `json:"integrity"`
	Signing   *testManifestSigning  `json:"signing,omitempty"`
}

func buildTestArchive(t *testing.T, n int, signKey []byte) []byte {
	t.Helper()
	var events []testChainEvent
	prevHash := testSHA256("genesis")
	for i := 0; i < n; i++ {
		event := map[string]interface{}{
			"id":     float64(i + 1),
			"action": "test.action",
		}
		eventJSON, _ := json.Marshal(event)
		canonical, err := canonicaljson.MarshalEntry(eventJSON)
		if err != nil {
			t.Fatal(err)
		}
		eventHash := testSHA256Bytes(canonical)
		chainInput := make([]byte, 0, len(canonical)+len(prevHash))
		chainInput = append(chainInput, canonical...)
		chainInput = append(chainInput, []byte(prevHash)...)
		chainHash := testSHA256Bytes(chainInput)
		events = append(events, testChainEvent{
			Sequence:  i + 1,
			Event:     eventJSON,
			EventHash: eventHash,
			PrevHash:  prevHash,
			ChainHash: chainHash,
		})
		prevHash = chainHash
	}

	finalHash := testSHA256("empty")
	if n > 0 {
		finalHash = events[n-1].ChainHash
	}

	var eventLines bytes.Buffer
	for _, e := range events {
		line, _ := json.Marshal(e)
		eventLines.Write(line)
		eventLines.WriteByte('\n')
	}
	eventsData := eventLines.Bytes()
	csvData := []byte("id,action\n")

	m := testManifest{
		Format:   "attune-audit-evidence-v1",
		ExportID: "test-export-id",
		TenantID: "test-tenant",
		Files: []testManifestFile{
			{Name: "events.jsonl", SHA256: testSHA256Bytes(eventsData)},
			{Name: "events.csv", SHA256: testSHA256Bytes(csvData)},
		},
		Integrity: testManifestIntegrity{
			Algorithm:      "SHA-256",
			ChainAlgorithm: "SHA-256(canonical_event || previous_chain_hash)",
			ChainHash:      finalHash,
			EventCount:     n,
		},
	}
	m.Stats.TotalEvents = n
	if signKey != nil {
		privKey := ed25519.NewKeyFromSeed(signKey[:ed25519.SeedSize])
		pubKey := privKey.Public().(ed25519.PublicKey)
		m.Signing = &testManifestSigning{ // ptrext:allow test-fixture
			Algorithm:            "Ed25519",
			PublicKeyFingerprint: testSHA256Bytes(pubKey),
		}
	}
	manifestJSON, _ := json.Marshal(m)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf) // ptrext:allow write-target
	writeZipFile(t, zw, "manifest.json", manifestJSON)
	writeZipFile(t, zw, "events.jsonl", eventsData)
	writeZipFile(t, zw, "events.csv", csvData)

	if signKey != nil {
		privKey := ed25519.NewKeyFromSeed(signKey[:ed25519.SeedSize])
		sig := ed25519.Sign(privKey, manifestJSON)
		writeZipFile(t, zw, "manifest.sig", sig)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTamperedArchive(t *testing.T) []byte {
	t.Helper()
	event := map[string]interface{}{"id": float64(1), "action": "original"}
	eventJSON, _ := json.Marshal(event)
	canonical, _ := canonicaljson.MarshalEntry(eventJSON)
	eventHash := testSHA256Bytes(canonical)
	prevHash := testSHA256("genesis")
	chainInput := make([]byte, 0, len(canonical)+len(prevHash))
	chainInput = append(chainInput, canonical...)
	chainInput = append(chainInput, []byte(prevHash)...)
	chainHash := testSHA256Bytes(chainInput)

	tamperedEvent := map[string]interface{}{"id": float64(1), "action": "tampered"}
	tamperedJSON, _ := json.Marshal(tamperedEvent)

	ce := testChainEvent{
		Sequence:  1,
		Event:     tamperedJSON,
		EventHash: eventHash,
		PrevHash:  prevHash,
		ChainHash: chainHash,
	}

	line, _ := json.Marshal(ce)
	eventsData := append(line, '\n')
	csvData := []byte("id,action\n")

	m := testManifest{
		Format:   "attune-audit-evidence-v1",
		ExportID: "tampered-export",
		TenantID: "test-tenant",
		Files: []testManifestFile{
			{Name: "events.jsonl", SHA256: testSHA256Bytes(eventsData)},
			{Name: "events.csv", SHA256: testSHA256Bytes(csvData)},
		},
		Integrity: testManifestIntegrity{
			Algorithm:      "SHA-256",
			ChainAlgorithm: "SHA-256(canonical_event || previous_chain_hash)",
			ChainHash:      chainHash,
			EventCount:     1,
		},
	}
	m.Stats.TotalEvents = 1
	manifestJSON, _ := json.Marshal(m)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf) // ptrext:allow write-target
	writeZipFile(t, zw, "manifest.json", manifestJSON)
	writeZipFile(t, zw, "events.jsonl", eventsData)
	writeZipFile(t, zw, "events.csv", csvData)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeZipFile(t *testing.T, zw *zip.Writer, name string, data []byte) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func testSHA256Bytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
