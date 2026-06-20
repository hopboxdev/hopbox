GO ?= go
BIN := bin
AGENT_IMAGE ?= ghcr.io/mesadev/mesa-agent:dev

.PHONY: proto agent agent-image build test lint run-mesad

proto:
	buf generate

agent:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(BIN)/mesa-agent-linux-amd64 ./cmd/mesa-agent

agent-image: agent
	docker build -f cmd/mesa-agent/Dockerfile -t $(AGENT_IMAGE) .

build: agent
	$(GO) build -tags docker -o $(BIN)/mesad ./cmd/mesad
	$(GO) build -o $(BIN)/mesa  ./cmd/mesa

test:
	$(GO) test ./...

lint:
	$(GO) run ./internal/core/internal/boundarycheck 2>/dev/null || true

run-mesad: build
	$(BIN)/mesad --db ./mesa.db --agent-bin ./$(BIN)/mesa-agent-linux-amd64
