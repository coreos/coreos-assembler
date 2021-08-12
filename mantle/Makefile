PREFIX ?= /usr
DESTDIR ?=

ARCH:=$(shell uname -m)

.PHONY: build test vendor clean
build:
	./build cmd/*

schema-update:
	$(MAKE) -C ../tools schema

.PHONY: install
install: build
	install -D -t $(DESTDIR)$(PREFIX)/bin bin/{ore,kola,plume}
	for arch in s390x x86_64 aarch64 ppc64le; do \
		install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/$${arch} bin/$${arch}/kolet; \
	done

test:
	./test

check:
	./test

vendor:
	@go mod vendor

clean:
	@rm -rfv bin
