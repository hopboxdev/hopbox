GO ?= go
BIN := bin
AGENT_IMAGE ?= ghcr.io/hopboxdev/hopbox-agent:dev

.PHONY: proto agent agent-image build test lint run-hopboxd

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
