PREFIX ?= /usr
DESTDIR ?=

ARCH:=$(shell uname -m)

BINS:=$(shell ls cmd)

bin/%:
	./build cmd/$*

.PHONY: build test vendor clean
build: $(patsubst %,bin/%,$(BINS))

schema-update:
	./build schema

.PHONY: install
install: bin/ore bin/kola bin/plume bin/kolet
	install -D -t $(DESTDIR)$(PREFIX)/bin bin/{ore,kola,plume}
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/$(ARCH) bin/kolet

test:
	./test

check:
	./test

vendor:
	@go mod vendor

clean:
	@rm -rfv bin
