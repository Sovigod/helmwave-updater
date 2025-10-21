package main

import (
	"os"
	"strings"
	"testing"
)

// Basic integration-style test: read the example tpl and run update pipeline
func TestUpdateFileText_WithOptionsAnchor(t *testing.T) {
	filename := "helmwave.yml.tpl"
	data, hw, err := readHelmwave(filename)
	if err != nil {
		t.Fatalf("readHelmwave failed: %v", err)
	}

	// build maps
	versionMap := buildVersionMap(&hw)
	chartMap := buildChartVersionMap(&hw)

	out := updateFileText(data, versionMap, chartMap)

	// ensure output was produced and is not empty
	if len(out) == 0 {
		t.Fatalf("output is empty")
	}

	// write temp file for inspection if running locally
	_ = os.WriteFile("helmwave.yml.tpl.testoutput", []byte(out), 0644)

	// Simple sanity: for each release in versionMap the new version should appear in output
	for _, v := range versionMap {
		if v == "" {
			continue
		}
		if !contains(out, v) {
			t.Fatalf("expected version %s to be present in output", v)
		}
	}

	// For charts in chartMap ensure versions present
	for _, v := range chartMap {
		if v == "" {
			continue
		}
		if !contains(out, v) {
			t.Fatalf("expected chart version %s to be present in output", v)
		}
	}
}

// helper wrapper around strings.Contains
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
