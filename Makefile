SHELL = /bin/bash

PROJECT_ROOT = $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

# Setting GOBIN and PATH ensures two things:
# - All 'go install' commands we run
#   only affect the current directory.
# - All installed tools are available on PATH
#   for commands like go generate.
export GOBIN = $(PROJECT_ROOT)/bin
export PATH := $(GOBIN):$(PATH)
export GOEXPERIMENT = rangefunc

GS = bin/gs
TEST_FLAGS ?= -race

# Non-test Go files.
GO_SRC_FILES = $(shell find . \
	   -path '*/.*' -prune -o \
	   '(' -type f -a -name '*.go' -a -not -name '*_test.go' ')' -print)

.PHONY: all
all: build lint test

.PHONY: build
build: $(GS)

.PHONY: lint
lint: golangci-lint tidy-lint generate-lint

.PHONY: test
test:
	go test $(TEST_FLAGS) ./...

.PHONY: cover
cover:
	go test $(TEST_FLAGS) -coverprofile=cover.out -coverpkg=./... ./...
	go tool cover -html=cover.out -o cover.html

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: golangci-lint
golangci-lint:
	golangci-lint run

.PHONY: tidy-lint
tidy-lint:
	@echo "[lint] go mod tidy"
	@go mod tidy && \
		git diff --exit-code -- go.mod go.sum || \
		(echo "'go mod tidy' changed files" && false)

.PHONY: generate
generate:
	go generate -x ./...

.PHONY: generate-lint
generate-lint:
	@echo "[lint] go generate"
	@go generate ./... && \
		git diff --exit-code || \
		(echo "'go generate' changed files" && false)

$(GS): $(GO_SRC_FILES)
	go install go.abhg.dev/gs
