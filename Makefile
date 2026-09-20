BINARY := gocamdet
CMD := ./cmd/gocamdet
BIN_DIR := bin

PREFIX := /usr/local
BINDIR := $(PREFIX)/bin
UNITDIR := /etc/systemd/system
CONFDIR := /etc/gocamdet

.PHONY: help build test run-tests fmt vet lint install-bin install uninstall clean

help:
	@echo "gocamdet — available targets:"
	@echo "  build       build the CLI into $(BIN_DIR)/$(BINARY)"
	@echo "  test        run the test suite"
	@echo "  run-tests   run the test suite (alias of test)"
	@echo "  fmt         format sources with gofmt"
	@echo "  vet         run go vet"
	@echo "  lint        run golangci-lint if installed"
	@echo "  install-bin install only the binary (needs root; run 'make build' first)"
	@echo "  install     install-bin plus the systemd unit and example hook (needs root)"
	@echo "  uninstall   remove installed binary, unit, and config (needs root)"
	@echo "  clean       remove build artifacts"
	@echo ""
	@echo "Deploy: run 'make build' as your user, then 'sudo make install' (or"
	@echo "'sudo make install-bin' to install just the binary and keep your own unit)."

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(CMD)

test: run-tests

run-tests:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; skipping"

# Install targets copy a pre-built binary; they never invoke go, so they work
# under sudo where root's PATH has no go. Build first: run 'make build' as your
# own user.
install-bin:
	@test -x $(BIN_DIR)/$(BINARY) || { echo "no built binary at $(BIN_DIR)/$(BINARY); run 'make build' first (as your user, not root)"; exit 1; }
	install -d $(BINDIR)
	install -m 0755 $(BIN_DIR)/$(BINARY) $(BINDIR)/$(BINARY)
	@echo "installed $(BINDIR)/$(BINARY)"

install: install-bin
	install -d $(CONFDIR)
	install -m 0644 systemd/gocamdet.service $(UNITDIR)/gocamdet.service
	install -m 0755 systemd/hook.sh.example $(CONFDIR)/hook.sh
	@echo "installed unit and example hook. enable with: systemctl daemon-reload && systemctl enable --now gocamdet"

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	rm -f $(UNITDIR)/gocamdet.service
	@echo "left $(CONFDIR) in place (may contain your hook); remove manually if desired"

clean:
	rm -rf $(BIN_DIR)
