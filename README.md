# helmwave-updater

A small utility to find the latest Helm chart versions from repository indexes and update `chart.version` fields in `helmwave.yml.tpl`.

## Features

- Parses `helmwave.yml.tpl` into Go structs and updates chart versions to the latest versions found in Helm repo indexes.
- Supports OCI charts (`oci://...`) by resolving and comparing registry tags.
- Preserves the original file formatting by performing line-oriented edits.
- Supports the `noupdate` tag on releases to skip updating specific releases.
- CLI: flags `-file`, `-inplace`, `-verbose`, `-no-repo-update`; subcommands `version`, `self-update`.

## Quick install (one-liners)

Install the latest release binary directly to /usr/local/bin using curl.

macOS (Apple Silicon / M-series, darwin/arm64):

```bash
sudo curl -sSL "https://github.com/Sovigod/helmwave-updater/releases/latest/download/helmwave-updater-darwin-arm64" -o /usr/local/bin/helmwave-updater && sudo chmod +x /usr/local/bin/helmwave-updater
```

Linux (x86_64 / linux/amd64):

```bash
sudo curl -sSL "https://github.com/Sovigod/helmwave-updater/releases/latest/download/helmwave-updater-linux-amd64" -o /usr/local/bin/helmwave-updater && sudo chmod +x /usr/local/bin/helmwave-updater
```

Note: these commands download the binary for the platform indicated and install it to `/usr/local/bin`. Adjust the URL or destination path if you need a different platform or install location.

## Building from source

### Requirements

- Go 1.21+ (the CI workflow uses Go 1.25, but the project is compatible with Go 1.21+)

### Quick build (local)

```bash
# regular build
make build

# minimal build (recommended for distribution)
make build-min
```

The resulting binary will be placed in the `bin/` directory.

The repository includes a `Makefile` with targets:

- `make build` — regular build
- `make build-min` — minimal build (CGO_DISABLED, -s -w, -trimpath, -buildvcs=false)
- `make cross` — example cross-build
- `make upx` — compress with upx (if installed)
- `make clean`, `make test`, `make fmt`

## Usage

Basic invocation:

```bash
bin/helmwave-updater -file path/to/helmwave.yml.tpl
```

To overwrite the original file in-place:

```bash
bin/helmwave-updater -file helmwave.yml.tpl -inplace
```

To skip `helm repo update` (useful in offline environments or CI where indexes are already fresh):

```bash
bin/helmwave-updater -no-repo-update -file helmwave.yml.tpl
```

### Self-update

Update the binary to the latest GitHub release:

```bash
helmwave-updater self-update
```

If the binary is installed in a system directory (e.g. `/usr/local/bin`), run with `sudo`:

```bash
sudo helmwave-updater self-update
```

## Contact

Author: Sovigod

License: MIT (unless otherwise specified in the repository)
