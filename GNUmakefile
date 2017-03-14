NAME ?= $(shell basename "$(CURDIR)")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)
SOURCE_FILES = $(shell find $(CURDIR) -type f -name '*.go')
PKG_FILES = bin/*
TAR=$(shell which 2>&1 /dev/null gnutar gtar tar | head -1)
EXTERNAL_TOOLS=\
	github.com/mitchellh/gox \
	github.com/kardianos/govendor

EFFECTIVE_LD_FLAGS ?= "-X main.GitCommit=$(GIT_COMMIT) $(LD_FLAGS)"

default: help

bin: bin/$(NAME) ## Build application binary

pkg: pkg/$(NAME).tar.gz ## Build application 'serviceball'

bin/$(NAME): $(SOURCE_FILES)
	go build -o "bin/$(NAME)" -ldflags $(EFFECTIVE_LD_FLAGS) .

pkg/$(NAME).tar.gz: bin/$(NAME)
	mkdir -p pkg/
	$(TAR) -czf pkg/$(NAME).tar.gz --xform='s,bin/,,' --xform='s,_build/,,' $(PKG_FILES)

.PHONY: bootstrap
bootstrap:
	@for tool in  $(EXTERNAL_TOOLS) ; do \
		echo "Installing $$tool" ; \
    go get -u $$tool; \
	done

.PHONY: fmt
fmt: ## Format the codebase
	@go fmt

.PHONY: vet
vet: ## Lint for errors
	@go vet

.PHONY: clean
clean: ## Clean build environment
	rm -r $(CURDIR)/bin
	rm -r $(CURDIR)/pkg

.PHONY: test
test: fmt vet ## Run tests, excluding forked dependencies
	go test -v -timeout 300s $(shell go list ./... | grep -v vendor/)
	#go test -v -race $(shell go list ./... | grep -v vendor/)

.PHONY: help
help:
	@echo "Valid targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
