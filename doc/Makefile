SHELL = /bin/bash

MAKEFILE_DIR = $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
export GOBIN = $(MAKEFILE_DIR)/bin
export PATH := $(GOBIN):$(PATH)

# Non-test Go files.
GO_SRC_FILES = $(shell find .. \
	   -path '*/.*' -prune -o \
	   '(' -type f -a -name '*.go' -a -not -name '*_test.go' ')' -print)

TOOLS = bin/pikchr bin/freeze bin/gs

CC ?= gcc

.PHONY: build
build: install generate
	uv run mkdocs build

.PHONY: serve
serve: install generate
	uv run mkdocs serve

.PHONY: install
install: $(TOOLS)

.PHONY: generate
generate: bin/gs
	bin/gs dumpmd \
		--ref includes/cli-reference.md \
		--shorthands includes/cli-shorthands.md

bin/pikchr: cmd/pikchr/pikchr.c
	mkdir -p $(dir $@)
	gcc -DPIKCHR_SHELL -o $@ $< -lm

bin/gs: $(GO_SRC_FILES)
	go -C .. install -tags dumpmd go.abhg.dev/gs

bin/freeze: go.mod
	go install github.com/charmbracelet/freeze
