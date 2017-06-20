.PHONY: vendor
vendor:
	@glide update --strip-vendor
	@glide-vc --use-lock-file --no-tests --only-code
