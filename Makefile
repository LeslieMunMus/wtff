BINARY := wtff
CMD    := ./cmd/wtff

# Version is stamped from git so a built binary can say what it came from.
# A working tree with uncommitted changes is marked, because a binary that
# reports a clean tag it does not actually match is worse than no version.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/lesliemusengi/wtff/internal/cli.Version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this list
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[1m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build ./wtff in this directory
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) $(CMD)

.PHONY: install
install: check ## Install wtff onto your PATH, after checks pass
	go install -ldflags '$(LDFLAGS)' $(CMD)
	@echo "installed $(VERSION) to $$(go env GOPATH)/bin/$(BINARY)"

.PHONY: run
run: build ## Build and launch the shell
	./$(BINARY)

.PHONY: test
test: ## Run the full suite
	go test ./...

.PHONY: race
race: ## Run the full suite under the race detector
	go test -race ./...

.PHONY: fmt
fmt: ## Format every package
	gofmt -w ./cmd ./internal

.PHONY: check
check: ## Everything that must pass before a change is done
	@test -z "$$(gofmt -l ./cmd ./internal)" \
		|| { echo "gofmt needs to run on:"; gofmt -l ./cmd ./internal; exit 1; }
	go vet ./...
	go test ./...
	@$(MAKE) --no-print-directory dash-scan

.PHONY: dash-scan
dash-scan: ## Refuse em dashes and en dashes anywhere in the project
	go test ./internal/house-rules/ -run TestNoEmOrEnDashes -count=1

# Release builds. lipo is spelled out rather than found on PATH: several
# toolchains ship their own, and picking up the wrong one would produce an
# archive that fails to run on half the machines it is offered to.
LIPO    := /usr/bin/lipo
DISTDIR := dist

.PHONY: dist
dist: check ## Build a universal binary, archive and checksum, after checks pass
	@rm -rf $(DISTDIR) && mkdir -p $(DISTDIR)
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '-s -w $(LDFLAGS)' 		-o $(DISTDIR)/$(BINARY)-arm64 $(CMD)
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags '-s -w $(LDFLAGS)' 		-o $(DISTDIR)/$(BINARY)-amd64 $(CMD)
	$(LIPO) -create -output $(DISTDIR)/$(BINARY) 		$(DISTDIR)/$(BINARY)-arm64 $(DISTDIR)/$(BINARY)-amd64
	@rm -f $(DISTDIR)/$(BINARY)-arm64 $(DISTDIR)/$(BINARY)-amd64
	@$(LIPO) -info $(DISTDIR)/$(BINARY)
	cd $(DISTDIR) && tar czf $(BINARY)-$(VERSION)-macos-universal.tar.gz $(BINARY)
	cd $(DISTDIR) && shasum -a 256 *.tar.gz > checksums.txt
	@rm -f $(DISTDIR)/$(BINARY)
	@cat $(DISTDIR)/checksums.txt

.PHONY: clean
clean: ## Remove build output
	rm -f $(BINARY)
	rm -rf $(DISTDIR)
