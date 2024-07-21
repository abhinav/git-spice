SHELL = /bin/bash

MAKEFILE_DIR = $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
export GOBIN = $(MAKEFILE_DIR)/bin
export PATH := $(GOBIN):$(PATH)

GS = bin/gs
FREEZE = bin/freeze

# Non-test Go files.
GO_SRC_FILES = $(shell find .. \
	   -path '*/.*' -prune -o \
	   '(' -type f -a -name '*.go' -a -not -name '*_test.go' ')' -print)

TOOLS = bin/pikchr bin/freeze

CC ?= gcc

.PHONY: build
build: install generate
	pipenv run mkdocs build

.PHONY: serve
serve: install generate
	pipenv run mkdocs serve

.PHONY: install
install: .cache/.install $(TOOLS)

.PHONY: generate
generate: $(GS)
	$(GS) dumpmd \
		--ref includes/cli-reference.md \
		--shorthands includes/cli-shorthands.md

# Marker file for having run 'pipenv install'.
.cache/.install: Pipfile.lock
	pipenv install
	mkdir -p .cache
	touch .cache/.install

bin/pikchr: cmd/pikchr/pikchr.c
	mkdir -p $(dir $@)
	gcc -DPIKCHR_SHELL -o $@ $< -lm

bin/gs: $(GO_SRC_FILES)
	go install -tags dumpmd go.abhg.dev/gs

bin/freeze: go.mod
	go install github.com/charmbracelet/freeze
