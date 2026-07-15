BINARY := gocamdet
CMD := ./cmd/gocamdet
BIN_DIR := bin

.PHONY: all build test vet lint clean

all: vet test build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

clean:
	rm -rf $(BIN_DIR)
