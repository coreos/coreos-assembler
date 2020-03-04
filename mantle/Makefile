PREFIX ?= /usr
DESTDIR ?=

GOARCH:=$(shell uname -m)
# Copied from coreos-assembler Makefile
ifeq ($(GOARCH),x86_64)
	GOARCH=amd64
else ifeq ($(GOARCH),aarch64)
	GOARCH=arm64
endif

.PHONY: build test vendor clean
build: clean
	./build

.PHONY: install
install: bin/ore bin/kola bin/plume bin/kolet
	install -D -t $(DESTDIR)$(PREFIX)/bin bin/{ore,kola,plume}
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/$(GOARCH) bin/kolet

test:
	./test

vendor:
	@go mod vendor

clean:
	@rm -rfv bin
