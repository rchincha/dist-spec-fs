BINARY_NAME=dist-spec-fs
ROOT_DIR=./data
PORT=8080

.PHONY: all build test clean run

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
