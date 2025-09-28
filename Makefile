## Makefile for helmwave-updater
# Targets:
#   make           -> build (default)
#   make build     -> regular go build
#   make build-min -> minimal binary (CGO_DISABLED, -s -w, -trimpath, -buildvcs=false)
#   make cross     -> example cross-build (linux/amd64)
#   make upx       -> compress with upx (if installed)
#   make size      -> list built artifacts
#   make clean     -> remove build artifacts
#   make test      -> run go tests

.PHONY: all build build-min cross upx size clean test fmt

GO ?= go
BINARY ?= helmwave-updater
PKG ?= .
BUILD_DIR ?= bin

default: build

mkdirs:
	@mkdir -p $(BUILD_DIR)

build: mkdirs
	@echo "Building: $(BINARY)"
	$(GO) build -o $(BUILD_DIR)/$(BINARY) $(PKG)
	@ls -lh $(BUILD_DIR)/$(BINARY)

build-ldflags: mkdirs
	@echo "Building with -ldflags '-s -w'"
	$(GO) build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) $(PKG)
	@ls -lh $(BUILD_DIR)/$(BINARY)

build-min: mkdirs
	@echo "Building minimal binary: CGO_ENABLED=0, -s -w, -trimpath, -buildvcs=false"
	CGO_ENABLED=0 $(GO) build -ldflags="-s -w" -trimpath -buildvcs=false -o $(BUILD_DIR)/$(BINARY) $(PKG)
	@ls -lh $(BUILD_DIR)/$(BINARY)

cross: mkdirs
	@echo "Cross-building linux/amd64 (example)"
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags="-s -w" -trimpath -buildvcs=false -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(PKG)
	@ls -lh $(BUILD_DIR)/$(BINARY)-linux-amd64

upx:
	@which upx >/dev/null || (echo "install upx to use this target" && exit 1)
	upx -9 $(BUILD_DIR)/$(BINARY)
	@ls -lh $(BUILD_DIR)/$(BINARY)

size:
	@ls -lh $(BUILD_DIR) || true

clean:
	@rm -rf $(BUILD_DIR)
	@echo "cleaned"

test:
	$(GO) test ./...

fmt:
	$(GO) fmt ./...
