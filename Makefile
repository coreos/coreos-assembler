.PHONY: build test vendor
build:
	./build

test:
	./test

vendor:
	@go mod vendor
