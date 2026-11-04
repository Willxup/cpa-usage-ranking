GO ?= go
NPM ?= npm
BIN_DIR ?= bin
GO_TEST_FLAGS ?= -p 2

.PHONY: deps verify verify-all fmt-check vet test test-go test-web test-race test-browser browser-install check-web build build-web build-go run
.NOTPARALLEL: verify verify-all build test-browser

deps:
	$(GO) mod download
	$(NPM) --prefix web ci

verify: fmt-check vet test-go check-web build

verify-all: verify test-race test-browser

browser-install:
	$(NPM) --prefix web run browser:install

test-browser: build
	RANKING_TEST_BINARY="$(abspath $(BIN_DIR))/ranking" $(NPM) --prefix web run test:browser

fmt-check:
	@files="$$(gofmt -l cmd internal)" && { test -z "$$files" || { printf 'Run gofmt on:\n%s\n' "$$files"; exit 1; }; }

vet:
	$(GO) vet ./...

test: test-go test-web

test-go:
	$(GO) test $(GO_TEST_FLAGS) ./...

test-web:
	$(NPM) --prefix web test

test-race:
	$(GO) test -race -count=1 $(GO_TEST_FLAGS) ./...

check-web:
	$(NPM) --prefix web run check

build: build-web build-go

build-web:
	$(NPM) --prefix web run build

build-go:
	mkdir -p "$(BIN_DIR)"
	$(GO) build -o "$(BIN_DIR)/ranking" ./cmd/ranking

run: build
	RANKING_WEB_DIR="$(CURDIR)/web/dist" "$(BIN_DIR)/ranking"
