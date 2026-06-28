// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/canonicaljson"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
)

type chainedEvent struct {
	Event     json.RawMessage `json:"event"`
	PrevHash  string          `json:"prev_hash"`
	EventHash string          `json:"event_hash"`
	ChainHash string          `json:"chain_hash"`
	Sequence  int             `json:"sequence"`
}

type manifestActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type manifestStats struct {
	TotalEvents  int            `json:"total_events"`
	FirstEventAt string         `json:"first_event_at,omitempty"`
	LastEventAt  string         `json:"last_event_at,omitempty"`
	ActionCounts map[string]int `json:"action_counts"`
}

type manifestFile struct {
	Name      string `json:"name"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type manifestIntegrity struct {
	Algorithm      string `json:"algorithm"`
	ChainAlgorithm string `json:"chain_algorithm"`
	ChainHash      string `json:"chain_hash"`
	EventCount     int    `json:"event_count"`
}

type manifestSigning struct {
	Algorithm            string `json:"algorithm"`
	PublicKeyFingerprint string `json:"public_key_fingerprint"`
}

type manifest struct {
	Version         string            `json:"version"`
	Format          string            `json:"format"`
	ExportID        string            `json:"export_id"`
	TenantID        string            `json:"tenant_id"`
	ExportedAt      time.Time         `json:"exported_at"`
	CreatedBy       manifestActor     `json:"created_by"`
	Filter          json.RawMessage   `json:"filter"`
	Stats           manifestStats     `json:"stats"`
	Files           []manifestFile    `json:"files"`
	Integrity       manifestIntegrity `json:"integrity"`
	Signing         *manifestSigning  `json:"signing,omitempty"`
	ExternalAnchors []any             `json:"external_anchors,omitempty"`
}

type archiveParams struct {
	jobID         string
	tenantID      string
	createdByType string
	createdBy     string
	filterJSON    json.RawMessage
	entries       []auditlogrepo.Entry
	signKey       []byte
}

type archiveResult struct {
	data        []byte
	totalEvents int
	filename    string
}

func buildArchive(p archiveParams) (*archiveResult, error) {
	now := time.Now().UTC()

	chained, finalHash, err := buildChain(p.entries)
	if err != nil {
		return nil, fmt.Errorf("build hash chain: %w", err)
	}

	var eventsJSONL bytes.Buffer
	for _, ce := range chained {
		line, err := json.Marshal(ce)
		if err != nil {
			return nil, fmt.Errorf("marshal chained event: %w", err)
		}
		eventsJSONL.Write(line)
		eventsJSONL.WriteByte('\n')
	}
	eventsData := eventsJSONL.Bytes()
	csvData := buildCSV(p.entries)

	var signing *manifestSigning
	var privKey ed25519.PrivateKey
	if len(p.signKey) >= ed25519.SeedSize {
		privKey = ed25519.NewKeyFromSeed(p.signKey[:ed25519.SeedSize])
		pubKey := privKey.Public().(ed25519.PublicKey)
		signing = ptrext.Of(manifestSigning{
			Algorithm:            "Ed25519",
			PublicKeyFingerprint: sha256Hex(pubKey),
		})
	}

	filterData := p.filterJSON
	if len(filterData) == 0 {
		filterData = json.RawMessage("{}")
	}

	m := manifest{
		Version:    "1",
		Format:     "attune-audit-evidence-v1",
		ExportID:   p.jobID,
		TenantID:   p.tenantID,
		ExportedAt: now,
		CreatedBy: manifestActor{
			Type: normalizeStoredActorType(p.createdByType, p.createdBy),
			ID:   p.createdBy,
		},
		Filter: filterData,
		Stats:  computeStats(p.entries),
		Files: []manifestFile{
			{Name: "events.jsonl", SizeBytes: len(eventsData), SHA256: sha256Hex(eventsData)},
			{Name: "events.csv", SizeBytes: len(csvData), SHA256: sha256Hex(csvData)},
		},
		Integrity: manifestIntegrity{
			Algorithm:      "SHA-256",
			ChainAlgorithm: "SHA-256(canonical_event || previous_chain_hash)",
			ChainHash:      finalHash,
			EventCount:     len(p.entries),
		},
		Signing: signing,
	}

	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf) // ptrext:allow write-target

	for _, f := range []struct {
		name string
		data []byte
	}{
		{"manifest.json", manifestJSON},
		{"events.jsonl", eventsData},
		{"events.csv", csvData},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, fmt.Errorf("create zip entry %s: %w", f.name, err)
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, fmt.Errorf("write zip entry %s: %w", f.name, err)
		}
	}

	if privKey != nil {
		sig := ed25519.Sign(privKey, manifestJSON)
		w, err := zw.Create("manifest.sig")
		if err != nil {
			return nil, fmt.Errorf("create zip entry manifest.sig: %w", err)
		}
		if _, err := w.Write(sig); err != nil {
			return nil, fmt.Errorf("write zip entry manifest.sig: %w", err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}

	filename := fmt.Sprintf("audit-evidence-%s.zip", now.Format("20060102-150405"))

	return ptrext.Of(archiveResult{
		data:        buf.Bytes(),
		totalEvents: len(p.entries),
		filename:    filename,
	}), nil
}

func computeStats(entries []auditlogrepo.Entry) manifestStats {
	stats := manifestStats{
		TotalEvents:  len(entries),
		ActionCounts: make(map[string]int),
	}
	if len(entries) > 0 {
		stats.FirstEventAt = entries[0].CreatedAt.UTC().Format(time.RFC3339Nano)
		stats.LastEventAt = entries[len(entries)-1].CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, e := range entries {
		stats.ActionCounts[e.Action]++
	}
	return stats
}

func buildChain(entries []auditlogrepo.Entry) ([]chainedEvent, string, error) {
	if len(entries) == 0 {
		return nil, sha256Hex([]byte("empty")), nil
	}

	result := make([]chainedEvent, 0, len(entries))
	prevHash := sha256Hex([]byte("genesis"))

	for i, entry := range entries {
		eventJSON, err := marshalEntry(entry)
		if err != nil {
			return nil, "", fmt.Errorf("marshal entry %d: %w", i, err)
		}
		canonical, err := canonicaljson.MarshalEntry(eventJSON)
		if err != nil {
			return nil, "", fmt.Errorf("canonicalize entry %d: %w", i, err)
		}
		eventHash := sha256Hex(canonical)

		h := sha256.New()
		h.Write(canonical)
		h.Write([]byte(prevHash))
		chainHash := hex.EncodeToString(h.Sum(nil))

		result = append(result, chainedEvent{
			Event:     eventJSON,
			PrevHash:  prevHash,
			EventHash: eventHash,
			ChainHash: chainHash,
			Sequence:  i + 1,
		})
		prevHash = chainHash
	}

	return result, prevHash, nil
}

func marshalEntry(e auditlogrepo.Entry) (json.RawMessage, error) {
	m := map[string]any{
		"id":         e.ID,
		"tenant_id":  e.TenantID,
		"action":     e.Action,
		"actor_type": e.ActorType,
		"actor_id":   e.ActorID,
		"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if e.ActorEmail != "" {
		m["actor_email"] = e.ActorEmail
	}
	if e.ActorIP != "" {
		m["actor_ip"] = e.ActorIP
	}
	if e.TargetType != "" {
		m["target_type"] = e.TargetType
	}
	if e.TargetID != "" {
		m["target_id"] = e.TargetID
	}
	if e.Summary != "" {
		m["summary"] = e.Summary
	}
	if len(e.BeforeJSON) > 0 {
		m["before"] = e.BeforeJSON
	}
	if len(e.AfterJSON) > 0 {
		m["after"] = e.AfterJSON
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func buildCSV(entries []auditlogrepo.Entry) []byte {
	var buf bytes.Buffer
	buf.WriteString("id,tenant_id,action,actor_type,actor_id,actor_email,target_type,target_id,summary,created_at\n")
	for _, e := range entries {
		fmt.Fprintf(&buf, "%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			e.ID,
			csvEscape(e.TenantID),
			csvEscape(e.Action),
			csvEscape(e.ActorType),
			csvEscape(e.ActorID),
			csvEscape(e.ActorEmail),
			csvEscape(e.TargetType),
			csvEscape(e.TargetID),
			csvEscape(e.Summary),
			e.CreatedAt.UTC().Format(time.RFC3339),
		)
	}
	return buf.Bytes()
}

func csvEscape(s string) string {
	if len(s) > 0 && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		s = "'" + s
	}
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
