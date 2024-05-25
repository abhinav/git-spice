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

TEST_FLAGS ?= -race
STITCHMD_FLAGS ?= -o README.md -preface doc/preface.txt doc/SUMMARY.md

GS = bin/gs
MOCKGEN = bin/mockgen
TOOLS = $(MOCKGEN) $(GS)

# Non-test Go files.
GO_SRC_FILES = $(shell find . \
	   -path '*/.*' -prune -o \
	   '(' -type f -a -name '*.go' -a -not -name '*_test.go' ')' -print)

DOC_MD_FILES = $(shell find doc -name '*.md')

.PHONY: all
all: build lint test

.PHONY: build
build: $(GS)

.PHONY: lint
lint: golangci-lint tidy-lint generate-lint

.PHONY: generate
generate: $(TOOLS)
	go generate -x ./...

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

README.md: $(DOC_MD_FILES)
	stitchmd $(STITCHMD_FLAGS)

.PHONY: golangci-lint
golangci-lint:
	golangci-lint run

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

# readme-lint depends on generate-lint
# because that updates doc/reference.md.
# In the future, that can be make-managed.
.PHONE: readme-lint
readme-lint: generate-lint
	@echo "[lint] readme"
	@DIFF=$$(stitchmd -diff $(STITCHMD_FLAGS)); \
	if [[ -n "$$DIFF" ]]; then \
		echo "stitchmd would change README:"; \
		echo "$$DIFF"; \
		false; \
	fi \

$(GS): $(GO_SRC_FILES) go.mod
	go install go.abhg.dev/gs

$(MOCKGEN): go.mod
	go install go.uber.org/mock/mockgen
