package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/repo"

	semver "github.com/Masterminds/semver/v3"
)

var filename string
var inplace bool
var verbose bool
var showVersion bool

// version is populated at build time via -ldflags "-X main.version=..."
var version = "dev"

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

// processReleases compares releases with repo indexes and updates in-memory versions.
func processReleases(hw *Helmwave, indexes map[string]*repo.IndexFile) {
	var helmwaveTags []string
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
			// collect last tag for this release (trim spaces)
			if len(release.Tags) > 0 {
				helmwaveTags = append(helmwaveTags, strings.TrimSpace(release.Tags[len(release.Tags)-1]))
			}
		} else {
			vlog("release %s is up-to-date (%s)", release.Name, release.Chart.Version)
		}
	}
	// remove duplicates while preserving order
	unique := make([]string, 0, len(helmwaveTags))
	seen := make(map[string]bool, len(helmwaveTags))
	for _, t := range helmwaveTags {
		if t == "" {
			continue
		}
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	fmt.Printf("\nexport HELMWAVE_TAGS='%s'\n", strings.Join(unique, ","))
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

// buildChartVersionMap prepares mapping chart full name (repo/chart) -> version
// This is used to update top-level anchors like `.options: &options` that contain a `chart:` block.
func buildChartVersionMap(hw *Helmwave) map[string]string {
	chartMap := make(map[string]string, len(hw.Releases))
	for _, r := range hw.Releases {
		if r.Chart.Name == "" {
			continue
		}
		if hasTag(r.Tags, NoupdateTag) {
			// skip releases marked as noupdate
			continue
		}
		chartMap[r.Chart.Name] = r.Chart.Version
	}
	return chartMap
}
