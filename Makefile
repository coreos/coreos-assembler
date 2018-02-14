.PHONY: build test vendor
build:
	./build

test:
	./test

vendor:
	@glide update --strip-vendor
	@glide-vc --use-lock-file --no-tests --only-code
