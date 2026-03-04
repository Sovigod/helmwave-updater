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

func TestLatestSemverTag(t *testing.T) {
	tests := []struct {
		name   string
		tags   []string
		want   string
		wantOK bool
	}{
		{
			name:   "selects highest semver with v-prefix",
			tags:   []string{"v0.9.0", "v1.2.3", "v1.10.0"},
			want:   "1.10.0",
			wantOK: true,
		},
		{
			name:   "ignores non-semver tags",
			tags:   []string{"latest", "main", "1.2.0"},
			want:   "1.2.0",
			wantOK: true,
		},
		{
			name:   "returns false when no semver tags",
			tags:   []string{"latest", "dev", "main"},
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := latestSemverTag(tt.tags)
			if ok != tt.wantOK {
				t.Fatalf("latestSemverTag() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("latestSemverTag() got = %q, want %q", got, tt.want)
			}
		})
	}
}
