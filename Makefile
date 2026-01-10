.PHONY: help generate build test test-short install

GO ?= go
CGO_ENABLED ?= 0
BIN ?= bin/centaurx
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL ?= install
INSTALL_MODE ?= 0755

GOFLAGS ?=
BUILD_FLAGS ?= -trimpath
LD_FLAGS ?= -s -w
ANDROID_DIR ?= android
APK_PATH ?= $(ANDROID_DIR)/app/build/outputs/apk/release/app-release.apk
APK_OUT ?= $(dir $(BIN))centaurx.apk
ZIP_OUT ?= $(dir $(BIN))centaurx-$(shell go env GOOS)-$(shell go env GOARCH)-$(shell go run ./internal/tools/versiongen).zip

default: help

help:
	@echo "Targets:"
	@echo "  make            # show this help"
	@echo "  make generate   # run: go generate ./..."
	@echo "  make build      # build the binary"
	@echo "  make test       # run tests with coverage"
	@echo "  make test-short # run short tests with coverage"
	@echo "  make install    # install binary"
	@echo "  make release    # build binary + android apk"
	@echo ""
	@echo "Variables:"
	@echo "  GO=$(GO)"
	@echo "  CGO_ENABLED=$(CGO_ENABLED)"
	@echo "  BIN=$(BIN)"
	@echo "  PREFIX=$(PREFIX)"
	@echo "  BINDIR=$(BINDIR)"
	@echo "  INSTALL=$(INSTALL)"
	@echo "  INSTALL_MODE=$(INSTALL_MODE)"
	@echo "  GOFLAGS=$(GOFLAGS)"
	@echo "  BUILD_FLAGS=$(BUILD_FLAGS)"
	@echo "  LD_FLAGS=$(LD_FLAGS)"
	@echo "  ANDROID_DIR=$(ANDROID_DIR)"
	@echo "  APK_PATH=$(APK_PATH)"
	@echo "  APK_OUT=$(APK_OUT)"

generate:
	$(GO) generate ./...

build:
	@mkdir -p $(dir $(BIN))
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOFLAGS) $(BUILD_FLAGS) -ldflags="$(LD_FLAGS)" -o $(BIN) ./cmd/centaurx

test:
	$(GO) test -count=1 -cover ./...

test-short:
	$(GO) test -short -count=1 -cover ./...

install: build
	$(INSTALL) -m $(INSTALL_MODE) $(BIN) $(BINDIR)/centaurx

release: build
	@mkdir -p $(dir $(BIN))
	$(MAKE) -C $(ANDROID_DIR) release
	cp $(APK_PATH) $(APK_OUT)
	zip -j $(ZIP_OUT) $(BIN) $(APK_OUT)
