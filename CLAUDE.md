# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build        # regular build -> bin/helmwave-updater
make build-min    # minimal binary (CGO_ENABLED=0, -s -w, -trimpath, -buildvcs=false)
make build-embed  # build with version string fetched from latest GitHub release
make test         # go test ./...
make fmt          # go fmt ./...
make cross        # cross-build linux/amd64 example
make clean        # remove bin/
```

Run a single test by name:
```bash
go test -run TestLatestSemverTag ./...
```

The version string is injected at build time via `-ldflags "-X main.version=<tag>"`. Without it, `version` defaults to `"dev"`.

## Architecture

The tool is a single `main` package with four source files:

- **[main.go](main.go)** — CLI flags (`-file`, `-inplace`, `-verbose`, `-version`), orchestration: reads file → loads indexes → processes releases → writes output.
- **[controller-helmwave.go](controller-helmwave.go)** — all business logic: loading Helm repo indexes, comparing semver versions, OCI tag resolution, building version maps, and performing line-oriented file edits.
- **[model-helmwave-yaml.go](model-helmwave-yaml.go)** — Go structs (`Helmwave`, `Release`, `Chart`) for unmarshalling `helmwave.yml.tpl`.
- **[helpers.go](helpers.go)** — `vlog` (verbose logger) and `hasTag` (case-insensitive tag check).

### Critical design: line-oriented file editing

The tool must preserve arbitrary Go-template expressions (e.g. `{{ env "VAR" }}`) in the file. Because of this, **it never roundtrips through YAML serialization**. Instead:

1. `removeTopLevelSection` strips `repositories:` and `registries:` blocks from the in-memory copy before YAML parsing (those sections contain template expressions that break strict YAML).
2. `updateFileText` performs two passes over the raw lines:
   - **Pass 1** — finds `- name: <releaseName>` blocks and updates their nested `chart.version` field.
   - **Pass 2** — finds top-level YAML anchors (lines starting with `.`, e.g. `.options: &options`) and updates their embedded `chart.version` by matching on `chart.name`.

### OCI vs. HTTP repo charts

- Charts with `oci://` prefix are resolved via `registry.Client.Tags()` and the latest semver tag is selected by `latestSemverTag`.
- Regular charts use the cached Helm repo index files at `~/.cache/helm/repository/*-index.yaml` (read via `helm.sh/helm/v4/pkg/repo/v1`).

### noupdate tag

Any release with a `tags:` entry equal to `noupdate` (case-insensitive) is skipped both in version comparison and in file editing.

### Output

After processing, the tool prints a `export HELMWAVE_TAGS='...'` line containing the deduplicated last tag of each updated release — intended to be eval'd in CI to selectively deploy only changed releases.
