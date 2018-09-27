.PHONY: build test vendor
build:
	./build

test:
	./test

vendor:
	@glide cc
	@glide -q update --strip-vendor
	@glide-vc --use-lock-file --no-tests --only-code --no-test-imports
