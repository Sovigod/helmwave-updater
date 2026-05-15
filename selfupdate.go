package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
)

const githubReleaseURL = "https://api.github.com/repos/Sovigod/helmwave-updater/releases/latest"

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runSelfUpdate(currentVersion string) {
	if err := selfUpdate(currentVersion); err != nil {
		log.Fatalf("self-update: %v", err)
	}
}

func selfUpdate(currentVersion string) error {
	log.Println("fetching latest release from GitHub...")
	release, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to fetch release info: %w", err)
	}

	latestTag := release.TagName
	log.Printf("current: %s, latest: %s", currentVersion, latestTag)

	if currentVersion != "dev" && strings.TrimPrefix(currentVersion, "v") == strings.TrimPrefix(latestTag, "v") {
		fmt.Println("already up to date")
		return nil
	}

	assetName := fmt.Sprintf("helmwave-updater-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL, err := findAssetURL(release, assetName)
	if err != nil {
		return fmt.Errorf("no binary for %s/%s in release %s: %w", runtime.GOOS, runtime.GOARCH, latestTag, err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	tmpPath := exePath + ".new"
	log.Printf("downloading %s...", latestTag)
	if err := downloadBinary(downloadURL, tmpPath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// copy permissions from existing binary
	info, err := os.Stat(exePath)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to stat current binary: %w", err)
	}
	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set permissions on downloaded binary: %w", err)
	}

	// atomic replace (same filesystem guaranteed — tmpPath is in the same dir)
	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		if isPermissionError(err) {
			return fmt.Errorf("permission denied replacing %s — try running with sudo", exePath)
		}
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	fmt.Printf("updated %s → %s\n", currentVersion, latestTag)
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, githubReleaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("empty tag_name in GitHub response")
	}
	return &release, nil
}

func findAssetURL(release *githubRelease, assetName string) (string, error) {
	for _, a := range release.Assets {
		if a.Name == assetName {
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset %q not found", assetName)
}

func downloadBinary(url, destPath string) error {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func isPermissionError(err error) bool {
	return strings.Contains(err.Error(), "permission denied") ||
		strings.Contains(err.Error(), "operation not permitted")
}
