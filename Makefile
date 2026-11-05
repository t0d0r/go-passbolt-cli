.DEFAULT_GOAL := help

BIN    := bin/passbolt
PREFIX ?= $(HOME)/.local
ARGS   ?=

.PHONY: help build build-linux test lint fmt check run clean install uninstall gif

help: ## list the targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## build the CLI for this machine into bin/passbolt (cgo on macOS for the native Keychain)
	go build -trimpath -o $(BIN) .

build-linux: ## cross-build a static linux/amd64 binary into bin/passbolt-linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/passbolt-linux-amd64 .
	@shasum -a 256 bin/passbolt-linux-amd64

test: ## run unit tests (integration tests need a Passbolt server and are not run)
	go test -short -count=1 ./...

lint: ## gofmt check, go vet and golangci-lint
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else echo "golangci-lint not installed, skipped"; fi

fmt: ## format the source in place
	gofmt -w .

check: lint test ## lint and test; must pass before a commit

run: build ## build and run the CLI, pass arguments with ARGS="..."
	./$(BIN) $(ARGS)

clean: ## remove build output
	rm -rf bin coverage.out

gif: build ## record docs/tui.gif from docs/tui.tape (needs vhs; made-up demo data only)
	@command -v vhs >/dev/null || { echo "vhs not installed"; exit 1; }
	vhs docs/tui.tape

install: build ## install to $(PREFIX)/bin
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/passbolt

uninstall: ## remove from $(PREFIX)/bin
	rm -f $(PREFIX)/bin/passbolt
