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

	semver "github.com/Masterminds/semver/v3"
)

var filename string
var inplace bool
var verbose bool

// tag that disables updating for a release (case-insensitive)
const NoupdateTag = "noupdate"

// ANSI color codes for terminal output
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
)

// vlog and hasTag are provided by helpers.go

// loadIndexes loads helm repo index files from settings repository cache.
func loadIndexes(settings *cli.EnvSettings) (map[string]*repo.IndexFile, error) {
	indexes := make(map[string]*repo.IndexFile)
	repoFile := filepath.Join(settings.RepositoryConfig)
	vlog("loading repository config from %s", repoFile)
	f, err := repo.LoadFile(repoFile)
	if err != nil {
		return nil, err
	}
	vlog("found %d repositories in repo file", len(f.Repositories))
	for _, entry := range f.Repositories {
		idxPath := filepath.Join(settings.RepositoryCache, fmt.Sprintf("%s-index.yaml", entry.Name))
		vlog("loading index for repo %s from %s", entry.Name, idxPath)
		idx, err := repo.LoadIndexFile(idxPath)
		if err != nil {
			log.Printf("⚠️ failed to load index %s: %v", entry.Name, err)
			continue
		}
		indexes[entry.Name] = idx
		if idx != nil {
			vlog("loaded index for %s: %d entries", entry.Name, len(idx.Entries))
		}
	}
	return indexes, nil
}

// readHelmwave reads and unmarshals helmwave YAML file into structures.
func readHelmwave(filename string) ([]byte, Helmwave, error) {
	vlog("reading input file: %s", filename)
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, Helmwave{}, err
	}
	vlog("read %d bytes from %s", len(data), filename)
	var hw Helmwave
	if err := yaml.Unmarshal(data, &hw); err != nil {
		return nil, Helmwave{}, err
	}
	return data, hw, nil
}

// processReleases compares releases with repo indexes and updates in-memory versions.
func processReleases(hw *Helmwave, indexes map[string]*repo.IndexFile) {
	var helmwaveTags string
	for id, release := range hw.Releases {
		vlog("processing release[%d]: name=%q chart=%q version=%q", id, release.Name, release.Chart.Name, release.Chart.Version)

		if hasTag(release.Tags, NoupdateTag) {
			vlog("skipping release %s because it has tag '%s'", release.Name, NoupdateTag)
			continue
		}

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
		vlog("found %d entries for %s/%s", len(entries), repoName, chartName)

		lastVersion := entries[0].Version
		lastVersion = strings.TrimPrefix(lastVersion, "v")

		if release.Chart.Version == "" {
			log.Printf("release %s: chart version not specified, skipping comparison", release.Name)
			continue
		}

		if release.Chart.Version != lastVersion {
			fmt.Printf("\nRelease: %s, Chart: %s, Version: %s\n", release.Name, release.Chart.Name, release.Chart.Version)
			fmt.Printf("   Update available: %s -> %s \n", release.Chart.Version, lastVersion)
			checkAppVersion(release, entries)
			vlog("updating in-memory release %s: %s -> %s", release.Name, release.Chart.Version, lastVersion)
			hw.Releases[id].Chart.Version = lastVersion
			helmwaveTags += fmt.Sprintf("%s,", release.Tags[len(release.Tags)-1])
		} else {
			vlog("release %s is up-to-date (%s)", release.Name, release.Chart.Version)
		}
	}
	fmt.Printf("\nexport HELMWAVE_TAGS='%s'\n", strings.TrimRight(helmwaveTags, ","))
}

func checkAppVersion(release Release, versions []*repo.ChartVersion) {
	vlog("checking appVersion for release %s", release.Name)

	var currentAppVer string
	var latestAppVer string
	// find the entry matching the current chart version
	for _, v := range versions {
		if strings.TrimPrefix(v.Version, "v") == release.Chart.Version {
			currentAppVer = v.AppVersion
			break
		}
	}
	if len(versions) > 0 {
		latestAppVer = versions[0].AppVersion
	}

	if currentAppVer == "" {
		vlog("no matching appVersion found for release %s", release.Name)
		if latestAppVer != "" {
			// still print latest known appVersion
			fmt.Printf("   AppVersion: (unknown) -> %s\n", latestAppVer)
		}
		return
	}

	// print simple mapping
	fmt.Printf("   AppVersion: %s -> %s\n", currentAppVer, latestAppVer)

	// try to parse semantic versions for delta calculation
	cur, err1 := semver.NewVersion(normalizeSemVer(currentAppVer))
	lat, err2 := semver.NewVersion(normalizeSemVer(latestAppVer))

	if err1 != nil || err2 != nil {
		// could not parse semver — nothing more to do
		vlog("could not parse appVersion(s) for release %s: curErr=%v latErr=%v", release.Name, err1, err2)
		return
	}

	// compare major/minor/patch (compare directly without intermediate variables)
	var importanceColor string
	var importanceLabel string

	switch {
	case lat.Major() > cur.Major():
		importanceColor = colorRed
		importanceLabel = "major"
	case lat.Minor() > cur.Minor():
		importanceColor = colorYellow
		importanceLabel = "minor"
	case lat.Patch() > cur.Patch():
		importanceColor = colorGreen
		importanceLabel = "patch"
	default:
		importanceColor = colorGreen
		importanceLabel = "none"
	}

	// show delta with color
	fmt.Printf("   Update importance: %s%s%s (%s -> %s)\n", importanceColor, strings.ToUpper(importanceLabel), colorReset, cur.String(), lat.String())
}

// normalizeSemVer attempts to coerce appVersion strings into a semver-compatible form
func normalizeSemVer(v string) string {
	// trim spaces and possible leading 'v'
	vv := strings.TrimSpace(v)
	vv = strings.TrimPrefix(vv, "v")
	// if version looks like '1' or '1.2', pad to three segments
	parts := strings.Split(vv, ".")
	if len(parts) == 1 {
		return vv + ".0.0"
	}
	if len(parts) == 2 {
		return vv + ".0"
	}
	return vv
}

// buildVersionMap prepares mapping release name -> version for file editing, skipping noupdate releases.
func buildVersionMap(hw *Helmwave) map[string]string {
	versionMap := make(map[string]string, len(hw.Releases))
	for _, r := range hw.Releases {
		if r.Name == "" {
			continue
		}
		if hasTag(r.Tags, NoupdateTag) {
			vlog("not including release %s in file edits because of '%s' tag", r.Name, NoupdateTag)
			continue
		}
		versionMap[r.Name] = r.Chart.Version
	}
	return versionMap
}

// updateFileText returns edited file content (string) with versions replaced according to versionMap.
func updateFileText(original []byte, versionMap map[string]string) string {
	text := string(original)
	lines := strings.Split(text, "\n")

	for relName, newVer := range versionMap {
		vlog("will update release %s -> %s in file text", relName, newVer)
		inRelease := false
		inChart := false
		var chartIndent int

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			indent := len(line) - len(strings.TrimLeft(line, " "))

			if strings.HasPrefix(trimmed, "- name:") {
				namePart := strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
				if idx := strings.Index(namePart, "#"); idx >= 0 {
					namePart = strings.TrimSpace(namePart[:idx])
				}
				namePart = strings.Trim(namePart, "'\"")
				if namePart == relName {
					inRelease = true
					inChart = false
					continue
				}
				if inRelease {
					inRelease = false
					inChart = false
				}
			}

			if !inRelease {
				continue
			}

			if strings.HasPrefix(trimmed, "chart:") {
				if strings.TrimSpace(trimmed) == "chart:" {
					inChart = true
					chartIndent = indent
					continue
				}
			}

			if inChart {
				if indent <= chartIndent && !strings.HasPrefix(trimmed, "version:") {
					inChart = false
					continue
				}

				if strings.HasPrefix(trimmed, "version:") {
					after := strings.TrimSpace(strings.TrimPrefix(trimmed, "version:"))
					comment := ""
					if idx := strings.Index(after, "#"); idx >= 0 {
						comment = " " + strings.TrimSpace(after[idx:])
					}
					origVal := strings.TrimSpace(after)
					origVal = strings.TrimRight(origVal, "# ")
					origVal = strings.Trim(origVal, "'\"")

					if origVal == newVer {
						vlog("existing version for release %s equals target %s; skipping file edit", relName, newVer)
						inChart = false
						inRelease = false
						// continue scanning for other occurrences of the same release later in the file
						continue
					}
					useQuotes := strings.Contains(after, "\"") || strings.Contains(after, "'")
					var valStr string
					if useQuotes {
						valStr = fmt.Sprintf("\"%s\"", newVer)
					} else {
						valStr = newVer
					}
					newLine := strings.Repeat(" ", indent) + "version: " + valStr + comment
					vlog("replacing line %d for release %s: %q -> %q", i+1, relName, lines[i], newLine)
					lines[i] = newLine
					inChart = false
					inRelease = false
					// continue scanning to update possible additional occurrences of the same release
					continue
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// writeOutput writes content to outFile and logs result.
func writeOutput(outFile, out string) error {
	if err := os.WriteFile(outFile, []byte(out), 0644); err != nil {
		return err
	}
	vlog("wrote %d bytes to %s", len(out), outFile)
	log.Printf("Wrote updated file: %s", outFile)
	return nil
}

func main() {
	// allow filename via flag or positional argument
	flag.StringVar(&filename, "file", "helmwave.yml.tpl", "path to helmwave yaml file")
	flag.BoolVar(&inplace, "inplace", false, "modify the original file instead of creating a .updated copy")
	flag.BoolVar(&verbose, "verbose", false, "enable verbose logging")
	flag.Parse()

	settings := cli.New()

	vlog("starting: file=%s inplace=%v verbose=%v", filename, inplace, verbose)
	vlog("helm settings: repo config=%s repo cache=%s namespace=%s", settings.RepositoryConfig, settings.RepositoryCache, settings.Namespace())

	indexes, err := loadIndexes(settings)
	if err != nil {
		log.Fatalf("failed to load repo file: %v", err)
	}

	data, hw, err := readHelmwave(filename)
	if err != nil {
		log.Fatalf("failed to read helmwave: %v", err)
	}

	processReleases(&hw, indexes)

	versionMap := buildVersionMap(&hw)

	out := updateFileText(data, versionMap)

	outFile := filename + ".updated"
	if inplace {
		outFile = filename
	}
	if err := writeOutput(outFile, out); err != nil {
		log.Fatalf("failed to write %s: %v", outFile, err)
	}
}
