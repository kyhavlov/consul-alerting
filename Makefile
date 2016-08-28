NAME?=$(shell basename "${CURDIR}")
EXTERNAL_TOOLS=\
	github.com/mitchellh/gox \
	github.com/kardianos/govendor

dev: fmt vet
	@BUILD_DEV=1 sh -c "'$(PWD)/scripts/build.sh'"

bin:
	@sh -c "'$(PWD)/scripts/build.sh'"

fmt:
	@go fmt

vet:
	@go vet

test: fmt vet
	@go test -v -timeout 300s

bootstrap:
	@for tool in  $(EXTERNAL_TOOLS) ; do \
		echo "Installing $$tool" ; \
    go get -u $$tool; \
	done

.PHONY: bootstrap dev bin fmt test vet
