# RasBeacon build commands. Run `make help` to list them.
#
# Everything a contributor needs day to day:
#   make test     run all unit tests (Go and web UI)
#   make lint     check formatting and common mistakes
#   make build    build the web UI and the three programs for this computer
#   make dev      run a simulated device at http://127.0.0.1:8080
#   make e2e      run the browser walkthrough against a simulated device
#   make pi       build the three programs for the Raspberry Pi (arm64)

# Version shown in the web UI; release builds pass VERSION=v0.1.0.
VERSION ?= dev
# REPOSITORY (owner/name on GitHub) is where the optional update check looks
# for new releases; empty turns the check off.
REPOSITORY ?=
LDFLAGS := -s -w -X rasbeacon/internal/version.Version=$(VERSION) -X rasbeacon/internal/version.Repository=$(REPOSITORY)

.PHONY: help ui test lint build dev e2e pi clean

help:
	@grep -E "^#   make" Makefile | sed "s/^#   //"

# Build the web UI into web/dist (embedded into rbweb by web/embed.go). The
# old build is removed first, except dist/.gitkeep.
ui:
	find web/dist -mindepth 1 ! -name .gitkeep -delete
	cd web && npm ci --no-audit --no-fund && npm run build

test:
	go test ./...
	cd web && npm test

lint:
	test -z "$$(gofmt -l .)" || (echo "Run gofmt -w on:"; gofmt -l .; exit 1)
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
	cd web && npm run typecheck && npm run lint && npm run format:check

build: ui
	mkdir -p build
	go build -trimpath -ldflags "$(LDFLAGS)" -o build/ ./cmd/rbd ./cmd/rbweb ./cmd/rbdoh

# Simulated device: no Raspberry Pi needed, nothing on this computer changes.
# DEV_BROWSER: a headless Chromium for the zero-leak login screen (optional;
# for example Playwright's chromium_headless_shell-*/chrome-headless-shell-linux64/
# chrome-headless-shell).
DEV_BROWSER ?=

dev: build
	@echo "Open http://127.0.0.1:8080 (press Ctrl+C to stop)"
	@echo "Simulated hotel login page: http://127.0.0.1:8099/require-login turns it on"
	rm -rf /tmp/rasbeacon-dev
	./build/rbd -dev /tmp/rasbeacon-dev -dev-hotel 127.0.0.1:8099 -dev-browser "$(DEV_BROWSER)" & \
	sleep 1; ./build/rbweb -listen 127.0.0.1:8080 -rbd-socket /tmp/rasbeacon-dev/run/rbd/api.sock; \
	kill %1

e2e:
	test/e2e/run.sh

# Cross-compile for the Raspberry Pi (64-bit ARM). Go needs no extra tools.
pi: ui
	mkdir -p build/arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o build/arm64/ ./cmd/rbd ./cmd/rbweb ./cmd/rbdoh

clean:
	rm -rf build web/dist-demo
	find web/dist -mindepth 1 ! -name .gitkeep -delete
