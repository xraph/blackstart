# Blackstart — one entrypoint for three toolchains.
#
# The Go module is rooted here, the Cargo workspace in rust/, the pnpm
# workspace in web/. Every target below works from the repository root.

GO      ?= go
CARGO   ?= cargo
PNPM    ?= pnpm
COMPOSE ?= docker compose

RUST_DIR := rust
WEB_DIR  := web

# Stamped into the binaries so that every result the platform produces can name
# the code that produced it. See a private design note.
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "unknown")
COMMIT    := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILDDATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -X github.com/xraph/blackstart/pkg/buildinfo.Version=$(VERSION) \
             -X github.com/xraph/blackstart/pkg/buildinfo.Commit=$(COMMIT) \
             -X github.com/xraph/blackstart/pkg/buildinfo.BuildDate=$(BUILDDATE)

BOLD := \033[1m
DIM  := \033[2m
OFF  := \033[0m

.DEFAULT_GOAL := help

## help: Show this message
.PHONY: help h
help h:
	@printf "$(BOLD)Blackstart$(OFF) $(DIM)$(VERSION)$(OFF)\n\n"
	@grep -E '^## [a-z-]+:' $(MAKEFILE_LIST) | sed 's/## //' | \
		awk -F': ' '{printf "  $(BOLD)%-20s$(OFF) %s\n", $$1, $$2}'
	@printf "\n"

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

## setup: Install git hooks and development tooling
.PHONY: setup
setup:
	@git config core.hooksPath .githooks
	@printf "git hooks installed (core.hooksPath = .githooks)\n"
	@cd $(WEB_DIR) && $(PNPM) install --frozen-lockfile

## dev-up: Start Postgres+PostGIS, Redis and FalkorDB
.PHONY: dev-up
dev-up:
	$(COMPOSE) -f deploy/docker-compose.dev.yml up -d
	@printf "waiting for services to become healthy...\n"
	@$(COMPOSE) -f deploy/docker-compose.dev.yml ps

## dev-down: Stop development services
.PHONY: dev-down
dev-down:
	$(COMPOSE) -f deploy/docker-compose.dev.yml down

## dev-reset: Stop development services and delete their volumes
.PHONY: dev-reset
dev-reset:
	$(COMPOSE) -f deploy/docker-compose.dev.yml down -v

# ---------------------------------------------------------------------------
# Lint and format
# ---------------------------------------------------------------------------

## lint: Run all linters (go, rust, web)
.PHONY: lint l
lint l: lint-go lint-rust lint-web

.PHONY: lint-go
lint-go:
	golangci-lint run ./...
	$(GO) vet ./...

# An empty Cargo workspace does not pass vacuously — `cargo fmt` reports
# "Failed to find targets" and `cargo clippy` exits 101 with "the manifest is
# virtual, and the workspace has no members". Guard until Phase 2 adds crates,
# and say why rather than skipping silently.
HAS_RUST_CRATES := $(shell grep -qE '^[[:space:]]*members[[:space:]]*=[[:space:]]*\[[[:space:]]*"' $(RUST_DIR)/Cargo.toml && echo yes)

.PHONY: lint-rust
lint-rust:
ifeq ($(HAS_RUST_CRATES),yes)
	cd $(RUST_DIR) && $(CARGO) fmt --all --check
	cd $(RUST_DIR) && $(CARGO) clippy --workspace --all-targets --all-features -- -D warnings
else
	@printf "lint-rust: skipped, the Cargo workspace has no members yet\n"
endif

.PHONY: lint-web
lint-web:
	cd $(WEB_DIR) && $(PNPM) run format:check
	cd $(WEB_DIR) && $(PNPM) run lint
	cd $(WEB_DIR) && $(PNPM) run typecheck

## fmt: Format all code
.PHONY: fmt f
fmt f:
	gofumpt -extra -w .
	goimports -w -local github.com/xraph/blackstart .
	cd $(RUST_DIR) && $(CARGO) fmt --all
	cd $(WEB_DIR) && $(PNPM) run format

## tidy: Tidy Go module files
.PHONY: tidy
tidy:
	$(GO) mod tidy
	cd tools && $(GO) mod tidy

# ---------------------------------------------------------------------------
# Test
# ---------------------------------------------------------------------------

## test: Run unit tests (no external services required)
.PHONY: test t
test t:
	$(GO) test -race -short ./...
ifeq ($(HAS_RUST_CRATES),yes)
	cd $(RUST_DIR) && $(CARGO) test --workspace
endif
	cd $(WEB_DIR) && $(PNPM) run test

## test-integration: Run integration tests (requires `make dev-up`)
.PHONY: test-integration
test-integration:
	$(GO) test -tags=integration -race -timeout 15m ./...

## test-golden: Check the solver against committed reference solutions
.PHONY: test-golden
test-golden:
	cd $(RUST_DIR) && $(CARGO) test --release -p bs-golden

## coverage: Run Go tests with a coverage report
.PHONY: coverage
coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

## build: Build all binaries
.PHONY: build b
build b: build-go build-rust build-web

.PHONY: build-go
build-go:
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/blackstart ./cmd/blackstart
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/bsctl ./cmd/bsctl

.PHONY: build-rust
build-rust:
	cd $(RUST_DIR) && $(CARGO) build --release

.PHONY: build-web
build-web:
	cd $(WEB_DIR) && $(PNPM) run build

## proto: Regenerate protobuf stubs and check they are committed
.PHONY: proto
proto:
	cd proto && buf lint
	cd proto && buf format --diff --exit-code
	cd proto && buf generate

## clean: Remove build output
.PHONY: clean
clean:
	rm -rf bin/ coverage.out coverage.html
	cd $(RUST_DIR) && $(CARGO) clean

## check: Everything CI runs, in CI's order
.PHONY: check c
check c: lint test
