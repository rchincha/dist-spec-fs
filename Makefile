BINARY_NAME=saor
ROOT_DIR=./data
PORT=8080

KIND_VERSION    := v0.32.0
KUBECTL_VERSION := v1.36.2

TOOLS_BIN := hack/tools/bin
OS        := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH      := $(shell uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

.PHONY: all build test clean run tools kind-up kind-demo kind-down kind-test

all: build

build:
	@echo "==> Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) .

test:
	@echo "==> Running tests..."
	bats test/bats/e2e.bats

clean:
	@echo "==> Cleaning up..."
	rm -f $(BINARY_NAME)
	rm -rf $(ROOT_DIR)

run: build
	@echo "==> Starting $(BINARY_NAME) on port $(PORT)..."
	./$(BINARY_NAME) --root $(ROOT_DIR) --port $(PORT)

tools: ## Download pinned kind/kubectl into hack/tools/bin
	@mkdir -p $(TOOLS_BIN)
	@if [ ! -f $(TOOLS_BIN)/kind ]; then \
	  echo "==> Downloading kind $(KIND_VERSION)..."; \
	  curl -fsSL "https://github.com/kubernetes-sigs/kind/releases/download/$(KIND_VERSION)/kind-$(OS)-$(ARCH)" -o $(TOOLS_BIN)/kind; \
	  chmod +x $(TOOLS_BIN)/kind; \
	else echo "==> kind already present"; fi
	@if [ ! -f $(TOOLS_BIN)/kubectl ]; then \
	  echo "==> Downloading kubectl $(KUBECTL_VERSION)..."; \
	  curl -fsSL "https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/$(OS)/$(ARCH)/kubectl" -o $(TOOLS_BIN)/kubectl; \
	  chmod +x $(TOOLS_BIN)/kubectl; \
	else echo "==> kubectl already present"; fi

kind-up: tools ## Build the image, start saor as a kind-wired registry, and create the kind cluster
	@test/scripts/setup-kind-registry.sh

kind-demo: kind-up ## Create files via WebDAV and launch them as a container in kind
	@test/scripts/webdav-demo.sh

kind-down: ## Tear down the kind cluster and registry container
	@test/scripts/teardown-kind-registry.sh

kind-test: tools ## Run the kind + WebDAV end-to-end bats test
	@RUN_KIND_TESTS=1 bats test/bats/kind-e2e.bats
