.PHONY: help build build-cli run test clean release release-local docker-cleanup monitoring-up monitoring-down

# Default target — auto-generated from `## ` comments next to each target.
# To document a target, put `## <section>: <description>` on the target line.
help:
	@awk 'BEGIN { \
		FS = ":.*?## "; \
		print "Hopbox development commands"; \
		print ""; \
	} \
	/^[a-zA-Z0-9_-]+:.*?## / { \
		sep = index($$2, ": "); \
		section = substr($$2, 1, sep - 1); \
		desc = substr($$2, sep + 2); \
		targets[section] = targets[section] sprintf("  \033[36m%-20s\033[0m %s\n", $$1, desc); \
		if (!(section in seen)) { order[++n] = section; seen[section] = 1 } \
	} \
	END { \
		for (i = 1; i <= n; i++) { \
			print order[i] ":"; \
			printf "%s", targets[order[i]]; \
			print ""; \
		} \
	}' $(MAKEFILE_LIST)

# Development

build: ## Development: Build hopboxd binary
	go build -o hopboxd ./cmd/hopboxd/

build-cli: ## Development: Cross-compile in-container hopbox CLI for linux
	./scripts/build-cli.sh

run: build build-cli ## Development: Build and run hopboxd (uses config.toml if present)
	./hopboxd

test: ## Development: Run all tests
	go test ./...

clean: ## Development: Remove build artifacts
	rm -f hopboxd templates/hop
	rm -rf dist/

# Release

release: ## Release: Create and push git tag (triggers CI release). Usage: make release VERSION=v0.1.0
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release VERSION=v0.1.0"; exit 1; fi
	git tag $(VERSION)
	git push origin $(VERSION)
	@echo ""
	@echo "Tag $(VERSION) pushed. GitHub Actions will build the release."

release-local: ## Release: Build release tarball locally for testing. Usage: make release-local VERSION=v0.1.0
	@if [ -z "$(VERSION)" ]; then echo "Usage: make release-local VERSION=v0.1.0"; exit 1; fi
	./scripts/build-release.sh $(VERSION) linux $$(go env GOARCH)
	@echo ""
	@echo "Tarball: dist/hopbox-$(VERSION)-linux-$$(go env GOARCH).tar.gz"

# Local testing helpers

docker-cleanup: ## Local testing: Remove all hopbox containers and images
	-docker rm -f $$(docker ps -aq --filter "name=hopbox-") 2>/dev/null
	-docker rmi $$(docker images "hopbox-base:*" -q) 2>/dev/null
	-docker rmi $$(docker images "hopbox-*" -q) 2>/dev/null
	rm -rf data/users/

monitoring-up: ## Local testing: Start Prometheus + Grafana (deploy/monitoring)
	cd deploy/monitoring && docker compose up -d
	@echo ""
	@echo "Prometheus: http://localhost:9090"
	@echo "Grafana:    http://localhost:3000 (admin/admin)"

monitoring-down: ## Local testing: Stop Prometheus + Grafana
	cd deploy/monitoring && docker compose down
