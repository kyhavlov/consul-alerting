dev: fmt vet
	@BUILD_DEV=1 sh -c "'$(PWD)/scripts/build.sh'"

bin:
	@sh -c "'$(PWD)/scripts/build.sh'"

fmt:
	@go fmt

vet:
	@go vet

test: fmt vet
	@go test

.PHONY: dev bin fmt test vet
