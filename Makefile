.PHONY: help build build-cli run test clean release release-local docker-cleanup monitoring-up monitoring-down

# Default target
help:
	@echo "Hopbox development commands"
	@echo ""
	@echo "Development:"
	@echo "  make build            Build hopboxd binary"
	@echo "  make build-cli        Cross-compile in-container hopbox CLI for linux"
	@echo "  make run              Build and run hopboxd (uses config.toml if present)"
	@echo "  make test             Run all tests"
	@echo "  make clean            Remove build artifacts"
	@echo ""
	@echo "Release:"
	@echo "  make release VERSION=v0.1.0       Create and push git tag (triggers CI release)"
	@echo "  make release-local VERSION=v0.1.0 Build release tarball locally for testing"
	@echo ""
	@echo "Local testing:"
	@echo "  make docker-cleanup            Remove all hopbox containers and images"
	@echo "  make monitoring-up             Start Prometheus + Grafana (deploy/monitoring)"
	@echo "  make monitoring-down           Stop Prometheus + Grafana"

# Development

build:
	go build -o hopboxd ./cmd/hopboxd/

build-cli:
	./scripts/build-cli.sh

run: build build-cli
	./hopboxd

test:
	go test ./...

clean:
	rm -f hopboxd hopbox templates/hopbox
	rm -rf dist/

# Release

release:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=v0.1.0"; exit 1; fi
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo ""
	@echo "Tag $(VERSION) pushed. GitHub Actions will build the release."

release-local:
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release-local VERSION=v0.1.0"; exit 1; fi
	./scripts/build-release.sh $(VERSION) linux $$(go env GOARCH)
	@echo ""
	@echo "Tarball: dist/hopbox-$(VERSION)-linux-$$(go env GOARCH).tar.gz"

# Local testing helpers

docker-cleanup:
	-docker rm -f $$(docker ps -aq --filter "name=hopbox-") 2>/dev/null
	-docker rmi $$(docker images "hopbox-base:*" -q) 2>/dev/null
	-docker rmi $$(docker images "hopbox-*" -q) 2>/dev/null
	rm -rf data/users/

monitoring-up:
	cd deploy/monitoring && docker compose up -d
	@echo ""
	@echo "Prometheus: http://localhost:9090"
	@echo "Grafana:    http://localhost:3001 (admin/admin)"

monitoring-down:
	cd deploy/monitoring && docker compose down
