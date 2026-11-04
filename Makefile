GO ?= go
NPM ?= npm
BIN_DIR ?= bin
GO_TEST_FLAGS ?= -p 2

.PHONY: deps verify fmt-check vet test test-go test-web test-race check-web build build-web build-go run
.NOTPARALLEL: verify build

deps:
	$(GO) mod download
	$(NPM) --prefix web ci

verify: fmt-check vet test-go check-web build

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
