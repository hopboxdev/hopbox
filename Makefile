GO ?= go
BIN := bin
AGENT_IMAGE ?= ghcr.io/hopboxdev/hopbox-agent:dev

.PHONY: agent agent-image build dist test lint run microvm-rootfs

# The hopbox substrate binaries: the daemon (both compute backends compiled in),
# the AI-control plane client, the in-box metadata client, and the in-box agent.
build: agent
	$(GO) build -tags "docker firecracker" -o $(BIN)/hopboxd   ./cmd/hopboxd
	$(GO) build                            -o $(BIN)/hopbox-mcp ./cmd/hopbox-mcp
	$(GO) build                            -o $(BIN)/box-guest  ./cmd/box-guest

# dist builds the release artifacts the installer downloads, linux/{amd64,arm64}.
dist:
	rm -rf dist && mkdir -p dist
	for arch in amd64 arm64; do \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -tags "docker firecracker" -o dist/hopboxd-linux-$$arch   ./cmd/hopboxd; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build                            -o dist/hopbox-mcp-linux-$$arch ./cmd/hopbox-mcp; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build                            -o dist/box-guest-linux-$$arch  ./cmd/box-guest; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build                            -o dist/hopbox-agent-linux-$$arch ./cmd/hopbox-agent; \
	done

agent:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(BIN)/hopbox-agent-linux-amd64 ./cmd/hopbox-agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o $(BIN)/hopbox-agent-linux-arm64 ./cmd/hopbox-agent

agent-image: agent
	docker build -f cmd/hopbox-agent/Dockerfile -t $(AGENT_IMAGE) .

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...

run: build
	$(BIN)/hopboxd --compute docker --db ./hopboxd.db --agent-bin ./$(BIN)/hopbox-agent-linux-amd64

# Build the golden microVM agent rootfs. Run on Linux as root.
microvm-rootfs:
	sudo build/microvm/build-rootfs.sh
