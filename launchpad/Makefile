# KindleLaunch — root Makefile (drives every leaf Go module via go.work).
# Verification gates mirror knowledge/kindlelaunch.frozen.kvx SECTION 17 + 20.
#
# Usage:
#   make build vet fmt lint test race cover cover-check   # full local gate
#   make ci                                               # what CI runs
#   make tidy sqlc                                         # maintenance

SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c
GO ?= go
GOFLAGS ?=

# All independent modules (each owns a go.mod). Order: foundations first.
MODULES := \
	shared \
	protocol \
	core/api \
	core/indexer \
	core/pnl-tracker \
	core/ranking-algo \
	core/stats-workers \
	core/trading-charts \
	media/gateway \
	media/livestream \
	media/metadata \
	media/social \
	media/user \


# Money / correctness-critical modules: 90% coverage gate (others 85%).
MONEY_MODULES := shared protocol core/indexer core/pnl-tracker core/trading-charts

.PHONY: all ci build vet fmt fmt-check lint test race cover cover-check tidy sqlc clean help

all: build vet fmt-check lint race cover-check ## Run the full local verification gate

ci: build vet fmt-check lint race cover-check ## Exactly what CI enforces on every PR

# has_pkgs <module> -> non-empty if the module currently contains Go packages.
# Lets the scaffold's empty modules be skipped cleanly until they ship code.
define has_pkgs
$$(cd $(1) && $(GO) list ./... 2>/dev/null)
endef

build: ## go build ./... in every module that has packages
	@for m in $(MODULES); do if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; echo ">> build $$m"; ( cd $$m && $(GO) build $(GOFLAGS) ./... ) || exit 1; done

vet: ## go vet ./... in every module that has packages
	@for m in $(MODULES); do if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; echo ">> vet $$m"; ( cd $$m && $(GO) vet ./... ) || exit 1; done

fmt: ## gofmt -w + goimports -w across the repo
	@gofmt -w $(MODULES)
	@command -v goimports >/dev/null && goimports -w $(MODULES) || true

fmt-check: ## fail if any file is not gofmt-clean
	@out=$$(gofmt -l $(MODULES)); if [ -n "$$out" ]; then echo "gofmt needs to run on:"; echo "$$out"; exit 1; fi

lint: ## golangci-lint run in every module (zero warnings, SECTION 17)
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not installed"; exit 1; }
	@for m in $(MODULES); do if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; echo ">> lint $$m"; ( cd $$m && golangci-lint run ) || exit 1; done

test: ## go test ./... in every module that has packages
	@for m in $(MODULES); do if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; echo ">> test $$m"; ( cd $$m && $(GO) test ./... ) || exit 1; done

race: ## go test -race ./... in every module (zero data races, SECTION 17)
	@for m in $(MODULES); do if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; echo ">> race $$m"; ( cd $$m && $(GO) test -race ./... ) || exit 1; done

cover: ## write per-module coverage profiles into .cover/
	@mkdir -p .cover
	@for m in $(MODULES); do \
		if [ -z "$(call has_pkgs,$$m)" ]; then echo ">> skip $$m (no packages)"; continue; fi; \
		name=$$(echo $$m | tr '/' '_'); \
		echo ">> cover $$m"; \
		( cd $$m && $(GO) test -covermode=atomic -coverprofile=$(CURDIR)/.cover/$$name.out ./... ) || exit 1; \
	done

cover-check: cover ## enforce the coverage gate (85% repo / 90% money modules)
	@MONEY="$(MONEY_MODULES)" tools/coverage-gate.sh

tidy: ## go mod tidy in every module
	@for m in $(MODULES); do echo ">> tidy $$m"; ( cd $$m && $(GO) mod tidy ); done

sqlc: ## regenerate type-safe DB code (sqlc) where a sqlc.yaml exists
	@command -v sqlc >/dev/null || { echo "sqlc not installed"; exit 1; }
	@for m in $(MODULES); do if [ -f $$m/sqlc.yaml ]; then echo ">> sqlc $$m"; ( cd $$m && sqlc generate ); fi; done

clean: ## remove build/coverage artifacts
	@rm -rf .cover
	@for m in $(MODULES); do ( cd $$m && $(GO) clean ./... ); done

help: ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
