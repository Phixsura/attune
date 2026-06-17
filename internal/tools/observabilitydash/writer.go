package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const (
	observabilityDir = "observability/dashboards"
	helmDir          = "deploy/helm/attune/dashboards"
)

func renderAll() (map[string][]byte, error) {
	out := make(map[string][]byte)
	for _, dash := range allDashboards() {
		body, err := renderDashboard(dash)
		if err != nil {
			return nil, err
		}
		out[dash.Filename] = body
	}
	return out, nil
}

func renderDashboard(d dashboard) ([]byte, error) {
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", d.Filename, err)
	}
	return append(body, '\n'), nil
}

func writeAll() error {
	rendered, err := renderAll()
	if err != nil {
		return err
	}
	for _, dir := range []string{observabilityDir, helmDir} {
		if err := writeDir(dir, rendered); err != nil {
			return err
		}
	}
	return nil
}

func writeDir(dir string, rendered map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := removeStaleDashboards(dir, rendered); err != nil {
		return err
	}
	for name, body := range rendered {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func removeStaleDashboards(dir string, rendered map[string][]byte) error {
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	for _, path := range entries {
		if _, ok := rendered[filepath.Base(path)]; ok {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale dashboard %s: %w", path, err)
		}
	}
	return nil
}

func sortedNames(rendered map[string][]byte) []string {
	names := make([]string, 0, len(rendered))
	for name := range rendered {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func equalJSON(a, b []byte) bool {
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}
