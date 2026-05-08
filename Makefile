.PHONY: test coverage lint build cli install clean

GO ?= go
PKG ?= ./...
CMD ?= ./cmd/cli
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/cli
COVERAGE_FILE ?= coverage.out

test:
	$(GO) test $(PKG)

coverage:
	$(GO) test -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(PKG)
	$(GO) tool cover -func=$(COVERAGE_FILE)

lint:
	golangci-lint run

build: cli

cli:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) $(CMD)

install:
	$(GO) install $(CMD)

clean:
	rm -rf $(BIN_DIR) $(COVERAGE_FILE)
