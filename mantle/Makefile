PREFIX ?= /usr
DESTDIR ?=


.PHONY: build
build:
	./build cmd/*

.PHONY: schema-update
schema-update:
	$(MAKE) -C ../tools schema

.PHONY: install
install: build
	install -D -t $(DESTDIR)$(PREFIX)/bin bin/{ore,kola,plume}
	for arch in s390x x86_64 aarch64 ppc64le; do \
		install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/$${arch} bin/$${arch}/kolet; \
	done

.PHONY: test
test:
	./test

.PHONY: check
check:
	./test

.PHONY: vendor
vendor:
	@go mod vendor

.PHONY: clean
clean:
	@rm -rfv bin
