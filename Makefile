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

TEST_FLAGS ?=

GS = bin/gs
MOCKGEN = bin/mockgen
REQUIREDFIELD = bin/requiredfield
TOOLS = $(MOCKGEN) $(GS)

.PHONY: all
all: build lint test

.PHONY: build
build: $(GS)

.PHONY: lint
lint: golangci-lint requiredfield-lint tidy-lint generate-lint

.PHONY: generate
generate: $(TOOLS)
	go generate -x ./...
	make -C doc generate

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

.PHONY: requiredfield-lint
requiredfield-lint: $(REQUIREDFIELD)
	@echo "[lint] requiredfield"
	@go vet -vettool=$(REQUIREDFIELD) ./...

.PHONY: tidy-lint
tidy-lint:
	@echo "[lint] go mod tidy"
	@go mod tidy && \
		git diff --exit-code -- go.mod go.sum || \
		(echo "'go mod tidy' changed files" && false)

.PHONY: generate-lint
generate-lint: $(TOOLS)
	@echo "[lint] go generate"
	@go generate ./... && \
		git diff --exit-code || \
		(echo "'go generate' changed files" && false)

$(GS): _always
	go install go.abhg.dev/gs

.PHONY: _always
_always:

$(MOCKGEN): go.mod
	go install go.uber.org/mock/mockgen

$(REQUIREDFIELD): go.mod
	go install go.abhg.dev/requiredfield/cmd/requiredfield
