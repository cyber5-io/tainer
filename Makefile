.PHONY: build test clean

# Single source of truth for the binary version.
VERSION := 1.0.0-dev
LDFLAGS := -X main.version=$(VERSION)

BIN_DIR := bin

# Default target — builds the host CLI.
build: $(BIN_DIR)/tainer

$(BIN_DIR)/tainer:
	@mkdir -p $(BIN_DIR)
	go build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/tainer ./cmd/tainer

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR)
