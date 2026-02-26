DIST            := dist
BINARY_HOP         := $(DIST)/hop
BINARY_HELPER      := $(DIST)/hop-helper
BINARY_AGENT       := $(DIST)/hop-agent
BINARY_AGENT_L     := $(DIST)/hop-agent-linux
BINARY_AGENT_L_ARM := $(DIST)/hop-agent-linux-arm64
BINARY_HOSTD       := $(DIST)/hopbox-hostd

VERSION         := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT          := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE            := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS         := -s -w \
	-X github.com/hopboxdev/hopbox/internal/version.Version=$(VERSION) \
	-X github.com/hopboxdev/hopbox/internal/version.Commit=$(COMMIT) \
	-X github.com/hopboxdev/hopbox/internal/version.Date=$(DATE)

.DEFAULT_GOAL := build

# ── Build ─────────────────────────────────────────────────────────────────────

.PHONY: build
build: $(BINARY_HOP) $(BINARY_HELPER) $(BINARY_AGENT_L) $(BINARY_HOSTD)

$(DIST):
	mkdir -p $(DIST)

$(BINARY_HOP): $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hop

$(BINARY_HELPER): $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hop-helper

$(BINARY_AGENT_L): $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hop-agent

$(BINARY_AGENT_L_ARM): $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hop-agent

$(BINARY_HOSTD): $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hopbox-hostd/

.PHONY: build-agent-arm64
build-agent-arm64: $(BINARY_AGENT_L_ARM)

.PHONY: build-agent-native
build-agent-native: $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_AGENT) ./cmd/hop-agent

.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/hop

# ── Test ──────────────────────────────────────────────────────────────────────

.PHONY: test
test:
	go test ./...

.PHONY: test-verbose
test-verbose:
	go test -v ./...

.PHONY: test-e2e
test-e2e:
	go test -v -count=1 ./internal/e2e/...

.PHONY: test-race
test-race:
	go test -race ./...

# ── Lint ──────────────────────────────────────────────────────────────────────

.PHONY: lint
lint:
	golangci-lint run

.PHONY: lint-fix
lint-fix:
	golangci-lint run --fix

# ── Release ───────────────────────────────────────────────────────────────────

.PHONY: snapshot
snapshot:
	goreleaser build --snapshot --clean

.PHONY: release
release:
	goreleaser release --clean

# ── Codegen ───────────────────────────────────────────────────────────────────

.PHONY: proto
proto:
	buf generate

# ── Dev helpers ───────────────────────────────────────────────────────────────

.PHONY: clean
clean:
	rm -rf $(DIST)/

.PHONY: hooks
hooks:
	prek install

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: version
version:
	@echo $(VERSION)
