// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/canonicaljson"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func runAudit(args []string) error {
	if len(args) == 0 {
		return errors.New("unknown audit subcommand, expected verify-export, generate-signing-key, or export-public-key")
	}
	switch args[0] {
	case "verify-export":
		return runVerifyExport(args[1:])
	case "generate-signing-key":
		return runGenerateSigningKey()
	case "export-public-key":
		return runExportPublicKey(args[1:])
	default:
		return fmt.Errorf("unknown audit subcommand %q", args[0])
	}
}

func runVerifyExport(args []string) error {
	fs := flag.NewFlagSet("audit verify-export", flag.ContinueOnError)
	pubKeyHex := fs.String("public-key", "", "hex-encoded Ed25519 public key for signature verification")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: attune audit verify-export [--public-key <hex>] <archive.zip>")
	}
	archivePath := fs.Arg(0)

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	manifestData, err := readZipEntry(zr, "manifest.json")
	if err != nil {
		return fmt.Errorf("read manifest.json: %w", err)
	}
	eventsData, err := readZipEntry(zr, "events.jsonl")
	if err != nil {
		return fmt.Errorf("read events.jsonl: %w", err)
	}

	var m verifyManifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	fmt.Printf("Archive:      %s\n", archivePath)
	fmt.Printf("Format:       %s\n", m.Format)
	fmt.Printf("Export ID:    %s\n", m.ExportID)
	fmt.Printf("Tenant:       %s\n", m.TenantID)
	fmt.Printf("Events:       %d\n", m.Stats.TotalEvents)
	fmt.Println()

	if err := verifyFileHashes(zr, m); err != nil {
		return err
	}

	if err := verifyChain(eventsData, m); err != nil {
		return err
	}

	if err := verifySignature(zr, manifestData, strings.TrimSpace(ptrext.Indirect(pubKeyHex)), m.Signing); err != nil {
		return err
	}
	return nil
}

type verifyManifest struct {
	Format    string          `json:"format"`
	ExportID  string          `json:"export_id"`
	TenantID  string          `json:"tenant_id"`
	Stats     verifyStats     `json:"stats"`
	Files     []verifyFile    `json:"files"`
	Integrity verifyIntegrity `json:"integrity"`
	Signing   *verifySigning  `json:"signing,omitempty"`
}

type verifyStats struct {
	TotalEvents int `json:"total_events"`
}

type verifyFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type verifyIntegrity struct {
	Algorithm  string `json:"algorithm"`
	ChainHash  string `json:"chain_hash"`
	EventCount int    `json:"event_count"`
}

type verifySigning struct {
	Algorithm            string `json:"algorithm"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

type chainEvent struct {
	Sequence  int             `json:"sequence"`
	Event     json.RawMessage `json:"event"`
	EventHash string          `json:"event_hash"`
	PrevHash  string          `json:"prev_hash"`
	ChainHash string          `json:"chain_hash"`
}

func verifyFileHashes(zr *zip.ReadCloser, m verifyManifest) error {
	for _, f := range m.Files {
		data, err := readZipEntry(zr, f.Name)
		if err != nil {
			return fmt.Errorf("FAIL  read %s: %w", f.Name, err)
		}
		actual := sha256Hex(data)
		if actual != f.SHA256 {
			return fmt.Errorf("FAIL  %s hash mismatch (expected %s, got %s)", f.Name, f.SHA256, actual)
		}
		fmt.Printf("PASS  %s hash verified (SHA-256)\n", f.Name)
	}
	return nil
}

func verifyChain(eventsData []byte, m verifyManifest) error {
	trimmed := strings.TrimSpace(string(eventsData))
	var lines []string
	if trimmed != "" {
		lines = strings.Split(trimmed, "\n")
	}
	if len(lines) != m.Integrity.EventCount {
		return fmt.Errorf("FAIL  event count mismatch: manifest says %d, found %d lines", m.Integrity.EventCount, len(lines))
	}
	if m.Integrity.EventCount == 0 {
		expected := sha256Hex([]byte("empty"))
		if m.Integrity.ChainHash != expected {
			return fmt.Errorf("FAIL  empty chain final hash mismatch")
		}
		fmt.Println("PASS  hash chain verified (0 events)")
		return nil
	}

	prevHash := sha256Hex([]byte("genesis"))
	var lastChainHash string
	for i, line := range lines {
		var ce chainEvent
		if err := json.Unmarshal([]byte(line), &ce); err != nil {
			return fmt.Errorf("FAIL  parse event line %d: %w", i+1, err)
		}
		if ce.Sequence != i+1 {
			return fmt.Errorf("FAIL  event %d: expected sequence %d, got %d", i+1, i+1, ce.Sequence)
		}
		if ce.PrevHash != prevHash {
			return fmt.Errorf("FAIL  event %d: prev_hash mismatch", i+1)
		}
		canonical, err := canonicaljson.MarshalEntry(ce.Event)
		if err != nil {
			return fmt.Errorf("FAIL  event %d: canonicalize: %w", i+1, err)
		}
		eventHash := sha256Hex(canonical)
		if ce.EventHash != eventHash {
			return fmt.Errorf("FAIL  event %d: event_hash mismatch (tampered?)", i+1)
		}
		h := sha256.New()
		h.Write(canonical)
		h.Write([]byte(ce.PrevHash))
		chainHash := hex.EncodeToString(h.Sum(nil))
		if ce.ChainHash != chainHash {
			return fmt.Errorf("FAIL  event %d: chain_hash mismatch (tampered?)", i+1)
		}
		prevHash = ce.ChainHash
		lastChainHash = ce.ChainHash
	}

	if lastChainHash != m.Integrity.ChainHash {
		return fmt.Errorf("FAIL  final chain hash mismatch: manifest %s vs computed %s", m.Integrity.ChainHash, lastChainHash)
	}
	fmt.Printf("PASS  hash chain integrity verified (%d events)\n", m.Integrity.EventCount)
	return nil
}

func verifySignature(zr *zip.ReadCloser, manifestData []byte, pubKeyHex string, signing *verifySigning) error {
	sigData, err := readZipEntry(zr, "manifest.sig")
	if err != nil {
		if pubKeyHex != "" {
			return fmt.Errorf("FAIL  --public-key provided but manifest.sig not found in archive")
		}
		fmt.Println("SKIP  no manifest.sig in archive (unsigned)")
		return nil
	}
	if pubKeyHex == "" {
		fmt.Println("WARN  manifest.sig present but no --public-key provided; skipping signature verification")
		return nil
	}
	pubKeyBytes, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return fmt.Errorf("FAIL  --public-key is not valid hex: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("FAIL  --public-key must be %d bytes (got %d)", ed25519.PublicKeySize, len(pubKeyBytes))
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)
	if !ed25519.Verify(pubKey, manifestData, sigData) {
		return fmt.Errorf("FAIL  Ed25519 signature verification failed (tampered manifest or wrong key?)")
	}
	fp := ""
	if signing != nil && signing.PublicKeyFingerprint != "" {
		fp = " (key: sha256:" + signing.PublicKeyFingerprint[:12] + "...)"
	}
	fmt.Printf("PASS  Ed25519 signature verified%s\n", fp)
	return nil
}

func runGenerateSigningKey() error {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := io.ReadFull(rand.Reader, seed); err != nil {
		return fmt.Errorf("generate random seed: %w", err)
	}
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	fmt.Println("# Audit evidence signing keypair (Ed25519)")
	fmt.Println("# Add signing_key to config.yaml under audit_evidence:")
	fmt.Printf("signing_key: %s\n", hex.EncodeToString(seed))
	fmt.Printf("public_key:  %s\n", hex.EncodeToString(pubKey))
	fmt.Println()
	fmt.Println("# Keep signing_key secret. Distribute public_key to auditors for verification.")
	return nil
}

func runExportPublicKey(args []string) error {
	fs := flag.NewFlagSet("audit export-public-key", flag.ContinueOnError)
	signingKeyHex := fs.String("signing-key", "", "hex-encoded Ed25519 signing key (seed)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(ptrext.Indirect(signingKeyHex)) == "" {
		return fmt.Errorf("usage: attune audit export-public-key --signing-key <hex>")
	}
	seed, err := hex.DecodeString(strings.TrimSpace(ptrext.Indirect(signingKeyHex)))
	if err != nil {
		return fmt.Errorf("--signing-key is not valid hex: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("--signing-key must be %d bytes (got %d)", ed25519.SeedSize, len(seed))
	}
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	fmt.Printf("public_key: %s\n", hex.EncodeToString(pubKey))
	return nil
}

const maxZipEntrySize = 512 << 20 // 512 MiB

func readZipEntry(zr *zip.ReadCloser, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			lr := io.LimitReader(rc, maxZipEntrySize+1)
			data, err := io.ReadAll(lr)
			if err != nil {
				return nil, err
			}
			if len(data) > maxZipEntrySize {
				return nil, fmt.Errorf("entry %q exceeds %d bytes", name, maxZipEntrySize)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("entry %q not found", name)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
