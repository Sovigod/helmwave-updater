package main

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"
	repo "helm.sh/helm/v4/pkg/repo/v1"

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
	var ociClient *registry.Client
	var ociClientErr error
	var ociClientInitialized bool

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

		if strings.HasPrefix(release.Chart.Name, registry.OCIScheme+"://") {
			if !ociClientInitialized {
				ociClient, ociClientErr = registry.NewClient(registry.ClientOptEnableCache(true))
				ociClientInitialized = true
			}
			if ociClientErr != nil {
				log.Printf("failed to initialize OCI registry client (release %s): %v", release.Name, ociClientErr)
				continue
			}

			lastVersion, err := latestOCIVersion(ociClient, release.Chart.Name)
			if err != nil {
				log.Printf("failed to get OCI tags for %q (release %s): %v", release.Chart.Name, release.Name, err)
				continue
			}

			if release.Chart.Version == "" {
				log.Printf("release %s: chart version not specified, skipping comparison", release.Name)
				continue
			}

			if release.Chart.Version != lastVersion {
				currentAppVersion, latestAppVersion, appVersionErr := ociAppVersions(ociClient, release.Chart.Name, release.Chart.Version, lastVersion)
				if appVersionErr != nil {
					log.Printf("failed to get OCI appVersion for %q (release %s): %v", release.Chart.Name, release.Name, appVersionErr)
				}

				printReleaseUpdate(release, release.Chart.Version, lastVersion, currentAppVersion, latestAppVersion)
				vlog("updating in-memory OCI release %s: %s -> %s", release.Name, release.Chart.Version, lastVersion)
				hw.Releases[id].Chart.Version = lastVersion
				if len(release.Tags) > 0 {
					helmwaveTags = append(helmwaveTags, strings.TrimSpace(release.Tags[len(release.Tags)-1]))
				}
			} else {
				vlog("OCI release %s is up-to-date (%s)", release.Name, release.Chart.Version)
			}
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
			currentAppVersion, latestAppVersion := appVersionsFromRepoEntries(release.Chart.Version, entries)
			printReleaseUpdate(release, release.Chart.Version, lastVersion, currentAppVersion, latestAppVersion)
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

func printReleaseUpdate(release Release, currentVersion, latestVersion, currentAppVersion, latestAppVersion string) {
	fmt.Printf("\nRelease: %s, Chart: %s, Version: %s\n", release.Name, release.Chart.Name, currentVersion)
	fmt.Printf("   Update available: %s -> %s \n", currentVersion, latestVersion)
	printAppVersionUpdate(currentAppVersion, latestAppVersion)
}

func printAppVersionUpdate(currentAppVersion, latestAppVersion string) {
	currentAppVersion = strings.TrimSpace(currentAppVersion)
	latestAppVersion = strings.TrimSpace(latestAppVersion)

	if currentAppVersion == "" && latestAppVersion == "" {
		return
	}

	if currentAppVersion == "" {
		fmt.Printf("   AppVersion: (unknown) -> %s\n", latestAppVersion)
		return
	}

	if latestAppVersion == "" {
		fmt.Printf("   AppVersion: %s -> (unknown)\n", currentAppVersion)
		return
	}

	fmt.Printf("   AppVersion: %s -> %s\n", currentAppVersion, latestAppVersion)
	importanceColor, importanceLabel, currentNormalized, latestNormalized, ok := appUpdateImportance(currentAppVersion, latestAppVersion)
	if !ok {
		return
	}

	fmt.Printf("   Update importance: %s%s%s (%s -> %s)\n", importanceColor, strings.ToUpper(importanceLabel), colorReset, currentNormalized, latestNormalized)
}

func appUpdateImportance(currentAppVersion, latestAppVersion string) (string, string, string, string, bool) {
	cur, err1 := semver.NewVersion(normalizeSemVer(currentAppVersion))
	lat, err2 := semver.NewVersion(normalizeSemVer(latestAppVersion))
	if err1 != nil || err2 != nil {
		return "", "", "", "", false
	}

	switch {
	case lat.Major() > cur.Major():
		return colorRed, "major", cur.String(), lat.String(), true
	case lat.Minor() > cur.Minor():
		return colorYellow, "minor", cur.String(), lat.String(), true
	case lat.Patch() > cur.Patch():
		return colorGreen, "patch", cur.String(), lat.String(), true
	default:
		return colorGreen, "none", cur.String(), lat.String(), true
	}
}

func appVersionsFromRepoEntries(currentChartVersion string, versions []*repo.ChartVersion) (string, string) {
	var currentAppVersion string
	var latestAppVersion string

	for _, v := range versions {
		if strings.TrimPrefix(v.Version, "v") == strings.TrimPrefix(currentChartVersion, "v") {
			currentAppVersion = strings.TrimSpace(v.AppVersion)
			break
		}
	}

	if len(versions) > 0 {
		latestAppVersion = strings.TrimSpace(versions[0].AppVersion)
	}

	return currentAppVersion, latestAppVersion
}

func ociAppVersions(client *registry.Client, chartRef, currentChartVersion, latestChartVersion string) (string, string, error) {
	currentAppVersion, err := ociAppVersionByTag(client, chartRef, currentChartVersion)
	if err != nil {
		return "", "", fmt.Errorf("current chart version %s: %w", currentChartVersion, err)
	}

	latestAppVersion, err := ociAppVersionByTag(client, chartRef, latestChartVersion)
	if err != nil {
		return "", "", fmt.Errorf("latest chart version %s: %w", latestChartVersion, err)
	}

	return currentAppVersion, latestAppVersion, nil
}

func ociAppVersionByTag(client *registry.Client, chartRef, chartVersion string) (string, error) {
	tagCandidates := []string{strings.TrimSpace(chartVersion)}
	trimmed := strings.TrimPrefix(strings.TrimSpace(chartVersion), "v")
	if trimmed != "" {
		tagCandidates = append(tagCandidates, trimmed)
		vTagged := "v" + trimmed
		if vTagged != tagCandidates[0] {
			tagCandidates = append(tagCandidates, vTagged)
		}
	}

	refCandidates := []string{chartRef}
	if trimmedRef := strings.TrimPrefix(chartRef, registry.OCIScheme+"://"); trimmedRef != chartRef {
		refCandidates = append(refCandidates, trimmedRef)
	}

	var lastErr error
	for _, ref := range refCandidates {
		for _, tag := range tagCandidates {
			if tag == "" {
				continue
			}
			pullRef := fmt.Sprintf("%s:%s", ref, tag)
			pulled, err := client.Pull(pullRef, registry.PullOptWithChart(true))
			if err != nil {
				lastErr = err
				continue
			}
			if pulled == nil || pulled.Chart == nil || pulled.Chart.Meta == nil {
				lastErr = errors.New("pulled chart metadata is empty")
				continue
			}
			return strings.TrimSpace(pulled.Chart.Meta.AppVersion), nil
		}
	}

	if lastErr == nil {
		lastErr = errors.New("no OCI tags to query appVersion")
	}
	return "", lastErr
}

func latestOCIVersion(client *registry.Client, chartRef string) (string, error) {
	tags, err := client.Tags(chartRef)
	if err != nil {
		trimmedRef := strings.TrimPrefix(chartRef, registry.OCIScheme+"://")
		if trimmedRef == chartRef {
			return "", err
		}
		tags, err = client.Tags(trimmedRef)
		if err != nil {
			return "", err
		}
	}

	latest, ok := latestSemverTag(tags)
	if !ok {
		return "", errors.New("no semver-compatible OCI tags found")
	}

	return latest, nil
}

func latestSemverTag(tags []string) (string, bool) {
	var selectedVersion *semver.Version
	selectedRawTag := ""

	for _, tag := range tags {
		normalized := normalizeSemVer(tag)
		parsed, err := semver.NewVersion(normalized)
		if err != nil {
			continue
		}
		if selectedVersion == nil || parsed.GreaterThan(selectedVersion) {
			selectedVersion = parsed
			selectedRawTag = tag
		}
	}

	if selectedVersion == nil {
		return "", false
	}

	return strings.TrimPrefix(strings.TrimSpace(selectedRawTag), "v"), true
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
