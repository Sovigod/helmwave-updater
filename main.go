package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/repo"
)

func main() {
	// allow filename via flag or positional argument
	var filename string
	var inplace bool
	flag.StringVar(&filename, "file", "helmwave.yml.tpl", "path to helmwave yaml file")
	flag.BoolVar(&inplace, "inplace", false, "modify the original file instead of creating a .updated copy")
	flag.Parse()

	settings := cli.New()

	var indexes = make(map[string]*repo.IndexFile)
	repoFile := filepath.Join(settings.RepositoryConfig)
	file, err := repo.LoadFile(repoFile)
	if err != nil {
		log.Fatalf("failed to load %s: %v", repoFile, err)
	}
	for _, entry := range file.Repositories {
		indexes[entry.Name], err = repo.LoadIndexFile(filepath.Join(settings.RepositoryCache, fmt.Sprintf("%s-index.yaml", entry.Name)))
		if err != nil {
			log.Printf("⚠️ failed to load index %s: %v", entry.Name, err)
			continue
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	var helmwave Helmwave
	if err := yaml.Unmarshal(data, &helmwave); err != nil {
		panic(err)
	}

	for id, release := range helmwave.Releases {
		// Validate chart name
		if release.Chart.Name == "" {
			log.Printf("skipping release %q: empty chart.name", release.Name)
			continue
		}

		parts := strings.SplitN(release.Chart.Name, "/", 2)
		if len(parts) != 2 {
			log.Printf("skipping release %q: unexpected chart.name format=%q", release.Name, release.Chart.Name)
			continue
		}

		repoName, chartName := parts[0], parts[1]

		idx, ok := indexes[repoName]
		if !ok || idx == nil {
			log.Printf("no index for repo %q (release %s)", repoName, release.Name)
			continue
		}

		entries, ok := idx.Entries[chartName]
		if !ok || len(entries) == 0 {
			log.Printf("no entries for chart %q in repo %q (release %s)", chartName, repoName, release.Name)
			continue
		}

		lastVersion := entries[0].Version
		lastVersion = strings.TrimPrefix(lastVersion, "v")

		if release.Chart.Version == "" {
			log.Printf("release %s: chart version not specified, skipping comparison", release.Name)
			continue
		}

		if release.Chart.Version != lastVersion {
			fmt.Printf("Release: %s, Chart: %s, Version: %s\n", release.Name, release.Chart.Name, release.Chart.Version)
			fmt.Printf("   Update available: %s -> %s \n\n", release.Chart.Version, lastVersion)
			helmwave.Releases[id].Chart.Version = lastVersion
		}
	}

	// Prepare map of release name -> updated chart version
	versionMap := make(map[string]string, len(helmwave.Releases))
	for _, r := range helmwave.Releases {
		if r.Name == "" {
			continue
		}
		versionMap[r.Name] = r.Chart.Version
	}

	// We'll copy the original file and update only the version lines for
	// matched releases. This preserves original formatting for everything
	// else.
	text := string(data)
	lines := strings.Split(text, "\n")

	for relName, newVer := range versionMap {
		inRelease := false
		inChart := false
		var chartIndent int

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))

			// Detect start of a release sequence item: "- name: <name>"
			if strings.HasPrefix(trimmed, "- name:") {
				// extract name value (strip comments and quotes)
				namePart := strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
				if idx := strings.Index(namePart, "#"); idx >= 0 {
					namePart = strings.TrimSpace(namePart[:idx])
				}
				namePart = strings.Trim(namePart, "'\"")
				if namePart == relName {
					inRelease = true
					inChart = false
					// continue to next line to search for chart block
					continue
				}
				// encountering another release; if we were inside target release, leave it
				if inRelease {
					inRelease = false
					inChart = false
				}
			}

			if !inRelease {
				continue
			}

			// We are inside the release mapping for relName.
			// Detect chart: mapping start
			if strings.HasPrefix(trimmed, "chart:") {
				// Only treat as block-mapping if exactly "chart:" (not inline)
				if strings.TrimSpace(trimmed) == "chart:" {
					inChart = true
					chartIndent = indent
					continue
				}
			}

			if inChart {
				// If indentation is less than or equal to chartIndent, we've left chart block
				if indent <= chartIndent && !strings.HasPrefix(trimmed, "version:") {
					inChart = false
					continue
				}

				if strings.HasPrefix(trimmed, "version:") {
					// preserve inline comment and quoting
					// split after 'version:'
					after := strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
					comment := ""
					if idx := strings.Index(after, "#"); idx >= 0 {
						comment = " " + strings.TrimSpace(after[idx:])
					}
					// preserve whether original used quotes
					origVal := strings.TrimSpace(after)
					origVal = strings.TrimRight(origVal, "# ")
					origVal = strings.Trim(origVal, "'\"")
					// decide quoting: if original had quotes, keep double quotes
					useQuotes := strings.Contains(after, "\"") || strings.Contains(after, "'")
					var valStr string
					if useQuotes {
						valStr = fmt.Sprintf("\"%s\"", newVer)
					} else {
						valStr = newVer
					}
					newLine := strings.Repeat(" ", indent) + "version: " + valStr + comment
					lines[i] = newLine
					// we can stop searching in this release for further version lines
					inChart = false
					inRelease = false
					break
				}
			}
		}
	}

	outFile := filename + ".updated"
	if inplace {
		outFile = filename
	}
	out := strings.Join(lines, "\n")
	if err := os.WriteFile(outFile, []byte(out), 0644); err != nil {
		log.Fatalf("failed to write %s: %v", outFile, err)
	}

	log.Printf("Wrote updated file: %s", outFile)

}
