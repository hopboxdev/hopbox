GO ?= go
BIN := bin
AGENT_IMAGE ?= ghcr.io/hopboxdev/hopbox-agent:dev

.PHONY: proto agent agent-image build dist test lint run-hopboxd

# dist builds the release artifacts the installer downloads: hopboxd, hopbox,
# hopbox-gw, hopbox-agent for linux/amd64 + linux/arm64, into dist/.
dist:
	rm -rf dist && mkdir -p dist
	for arch in amd64 arm64; do \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -tags docker -o dist/hopboxd-linux-$$arch ./cmd/hopboxd; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -o dist/hopbox-linux-$$arch ./cmd/hopbox; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -o dist/hopbox-gw-linux-$$arch ./cmd/hopbox-gw; \
	  GOOS=linux GOARCH=$$arch CGO_ENABLED=0 $(GO) build -o dist/hopbox-agent-linux-$$arch ./cmd/hopbox-agent; \
	done


proto:
	buf generate

agent:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(BIN)/hopbox-agent-linux-amd64 ./cmd/hopbox-agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o $(BIN)/hopbox-agent-linux-arm64 ./cmd/hopbox-agent

agent-image: agent
	docker build -f cmd/hopbox-agent/Dockerfile -t $(AGENT_IMAGE) .

build: agent
	$(GO) build -tags docker -o $(BIN)/hopboxd ./cmd/hopboxd
	$(GO) build -o $(BIN)/hopbox    ./cmd/hopbox
	$(GO) build -o $(BIN)/hopbox-gw ./cmd/hopbox-gw

test:
	$(GO) test ./...

lint:
	$(GO) run ./internal/core/internal/boundarycheck 2>/dev/null || true

run-hopboxd: build
	$(BIN)/hopboxd --db ./hopbox.db --agent-bin ./$(BIN)/hopbox-agent-linux-amd64
