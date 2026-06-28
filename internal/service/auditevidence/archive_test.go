// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/canonicaljson"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

func TestBuildChain_Empty(t *testing.T) {
	chained, finalHash, err := buildChain(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chained) != 0 {
		t.Fatalf("expected 0 events, got %d", len(chained))
	}
	expected := sha256Hex([]byte("empty"))
	if finalHash != expected {
		t.Fatalf("expected final hash %s, got %s", expected, finalHash)
	}
}

func TestBuildChain_Integrity(t *testing.T) {
	entries := makeTestEntries(5)
	chained, finalHash, err := buildChain(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chained) != 5 {
		t.Fatalf("expected 5 events, got %d", len(chained))
	}

	genesis := sha256Hex([]byte("genesis"))
	if chained[0].PrevHash != genesis {
		t.Fatalf("first event prev_hash should be genesis hash")
	}

	verifyChainLinks(t, chained)

	if chained[len(chained)-1].ChainHash != finalHash {
		t.Fatal("final hash doesn't match last chain_hash")
	}
}

func TestBuildChain_Determinism(t *testing.T) {
	entries := makeTestEntries(5)
	chained1, finalHash1, err := buildChain(entries)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	chained2, finalHash2, err := buildChain(entries)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if finalHash1 != finalHash2 {
		t.Fatal("chain is not deterministic")
	}
	for i := range chained1 {
		if chained1[i].ChainHash != chained2[i].ChainHash {
			t.Errorf("event %d: chain not deterministic", i)
		}
	}
}

func verifyChainLinks(t *testing.T, chained []chainedEvent) {
	t.Helper()
	for i, ce := range chained {
		if ce.Sequence != i+1 {
			t.Errorf("event %d: expected sequence %d, got %d", i, i+1, ce.Sequence)
		}
		canonical, err := canonicaljson.MarshalEntry(ce.Event)
		if err != nil {
			t.Fatalf("event %d: canonicalize: %v", i, err)
		}
		eventHash := sha256Hex(canonical)
		if ce.EventHash != eventHash {
			t.Errorf("event %d: event_hash mismatch", i)
		}
		chainInput := make([]byte, 0, len(canonical)+len(ce.PrevHash))
		chainInput = append(chainInput, canonical...)
		chainInput = append(chainInput, []byte(ce.PrevHash)...)
		chainHash := sha256Hex(chainInput)
		if ce.ChainHash != chainHash {
			t.Errorf("event %d: chain_hash mismatch", i)
		}
		if i > 0 && ce.PrevHash != chained[i-1].ChainHash {
			t.Errorf("event %d: prev_hash doesn't match previous chain_hash", i)
		}
	}
}

func TestBuildChain_TamperDetection(t *testing.T) {
	entries := makeTestEntries(3)
	chained, originalHash, err := buildChain(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = chained

	tampered := make([]auditlogrepo.Entry, len(entries))
	copy(tampered, entries)
	tampered[1].Action = "tampered.action"
	_, tamperedHash, err := buildChain(tampered)
	if err != nil {
		t.Fatalf("tampered build: %v", err)
	}
	if originalHash == tamperedHash {
		t.Fatal("tampered chain produced same hash")
	}
}

func TestBuildArchive_ZIPStructure(t *testing.T) {
	entries := makeTestEntries(3)
	result, err := buildArchive(archiveParams{
		jobID:         "job-1",
		tenantID:      "tenant-1",
		createdByType: "admin",
		createdBy:     "admin-1",
		entries:       entries,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.totalEvents != 3 {
		t.Fatalf("expected 3 events, got %d", result.totalEvents)
	}
	if !strings.HasPrefix(result.filename, "audit-evidence-") {
		t.Fatalf("unexpected filename: %s", result.filename)
	}

	zr, err := zip.NewReader(bytes.NewReader(result.data), int64(len(result.data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	assertZipContains(t, zr, "manifest.json", "events.jsonl", "events.csv")

	m := readManifest(t, zr)
	if m.Format != "attune-audit-evidence-v1" {
		t.Errorf("manifest format: want attune-audit-evidence-v1, got %s", m.Format)
	}
	if m.ExportID != "job-1" {
		t.Errorf("manifest export_id: want job-1, got %s", m.ExportID)
	}
	if m.CreatedBy.Type != "admin" {
		t.Errorf("manifest created_by.type: want admin, got %s", m.CreatedBy.Type)
	}
	if m.Stats.TotalEvents != 3 {
		t.Errorf("manifest stats.total_events: want 3, got %d", m.Stats.TotalEvents)
	}
	if m.Integrity.Algorithm != "SHA-256" {
		t.Errorf("manifest integrity.algorithm: want SHA-256, got %s", m.Integrity.Algorithm)
	}
	if len(m.Files) != 2 {
		t.Errorf("manifest files: want 2, got %d", len(m.Files))
	}
	if m.Signing != nil {
		t.Errorf("manifest signing should be nil without key")
	}
	if m.Stats.ActionCounts["api_key.create"] != 3 {
		t.Errorf("manifest action_counts[api_key.create]: want 3, got %d", m.Stats.ActionCounts["api_key.create"])
	}
}

func assertZipContains(t *testing.T, zr *zip.Reader, names ...string) {
	t.Helper()
	have := make(map[string]bool)
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, name := range names {
		if !have[name] {
			t.Errorf("missing zip entry: %s", name)
		}
	}
}

func readManifest(t *testing.T, zr *zip.Reader) manifest {
	t.Helper()
	f := findZipFile(zr, "manifest.json")
	if f == nil {
		t.Fatal("manifest.json not found")
	}
	rc, err := f.Open()
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer rc.Close()
	var m manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return m
}

func TestBuildArchive_WithSigning(t *testing.T) {
	entries := makeTestEntries(2)
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	result, err := buildArchive(archiveParams{
		jobID:         "signed-job",
		tenantID:      "tenant-signed",
		createdByType: "api_key",
		createdBy:     "apikey:123",
		entries:       entries,
		signKey:       seed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(result.data), int64(len(result.data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	sigFile := findZipFile(zr, "manifest.sig")
	if sigFile == nil {
		t.Fatal("manifest.sig not found in signed archive")
	}

	manifestFile := findZipFile(zr, "manifest.json")
	if manifestFile == nil {
		t.Fatal("manifest.json not found")
	}
	manifestRC, _ := manifestFile.Open()
	defer manifestRC.Close()
	var manifestBuf bytes.Buffer
	if _, err := manifestBuf.ReadFrom(manifestRC); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(manifestBuf.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if m.CreatedBy.Type != "api_key" {
		t.Errorf("manifest created_by.type: want api_key, got %s", m.CreatedBy.Type)
	}

	sigRC, _ := sigFile.Open()
	defer sigRC.Close()
	var sigBuf bytes.Buffer
	if _, err := sigBuf.ReadFrom(sigRC); err != nil {
		t.Fatalf("read sig: %v", err)
	}

	privKey := ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])
	pubKey := privKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pubKey, manifestBuf.Bytes(), sigBuf.Bytes()) {
		t.Fatal("Ed25519 signature verification failed")
	}

	if err := json.Unmarshal(manifestBuf.Bytes(), &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if m.Signing == nil {
		t.Fatal("manifest signing should not be nil")
	}
	if m.Signing.Algorithm != "Ed25519" {
		t.Errorf("manifest signing.algorithm: want Ed25519, got %s", m.Signing.Algorithm)
	}
	expectedFP := sha256HexTest(pubKey)
	if m.Signing.PublicKeyFingerprint != expectedFP {
		t.Errorf("manifest signing.public_key_fingerprint mismatch")
	}
}

func TestBuildCSV(t *testing.T) {
	entries := makeTestEntries(2)
	csv := string(buildCSV(entries))
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 data lines, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "id,") {
		t.Errorf("CSV header missing: %s", lines[0])
	}
}

func TestCSVEscape(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"simple", "simple"},
		{"has,comma", `"has,comma"`},
		{`has"quote`, `"has""quote"`},
		{"has\nnewline", `"has` + "\n" + `newline"`},
		{"=cmd()", "'=cmd()"},
		{"+cmd()", "'+cmd()"},
		{"-cmd()", "'-cmd()"},
		{"@cmd()", "'@cmd()"},
		{"+has,comma", `"'+has,comma"`},
	}
	for _, tt := range tests {
		got := csvEscape(tt.in)
		if got != tt.want {
			t.Errorf("csvEscape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func findZipFile(zr *zip.Reader, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func makeTestEntries(n int) []auditlogrepo.Entry {
	entries := make([]auditlogrepo.Entry, n)
	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	for i := range entries {
		entries[i] = auditlogrepo.Entry{
			ID:         int64(i + 1),
			TenantID:   "test-tenant",
			ActorType:  "admin",
			ActorID:    "user-1",
			ActorEmail: "admin@example.com",
			Action:     "api_key.create",
			TargetType: "api_key",
			TargetID:   "key-" + string(rune('a'+i)),
			Summary:    "Created API key",
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
		}
	}
	return entries
}

func sha256HexTest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Verify that the chain hash computation matches manually computed values.
func TestBuildChain_ManualVerification(t *testing.T) {
	entries := makeTestEntries(1)
	chained, _, err := buildChain(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(chained) != 1 {
		t.Fatal("expected 1")
	}
	ce := chained[0]

	canonical, err := canonicaljson.MarshalEntry(ce.Event)
	if err != nil {
		t.Fatal(err)
	}
	eventHash := sha256HexTest(canonical)
	if ce.EventHash != eventHash {
		t.Fatal("event hash mismatch with manual computation")
	}

	genesis := sha256HexTest([]byte("genesis"))
	chainInput := make([]byte, 0, len(canonical)+len(genesis))
	chainInput = append(chainInput, canonical...)
	chainInput = append(chainInput, []byte(genesis)...)
	chainHash := sha256HexTest(chainInput)
	if ce.ChainHash != chainHash {
		t.Fatal("chain hash mismatch with manual computation")
	}
}
