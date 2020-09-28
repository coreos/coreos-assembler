my_dir = $(shell $$(dirname $$(realpath)))
version = $(shell date +%Y-%m-%d).$(shell git rev-parse --short HEAD)~$(shell test -n "`git status -s`" && echo dirty || echo clean)
cosa_dir = $(shell test -d /usr/lib/coreos-assembler && echo /usr/lib/coreos-assembler)
ldflags=-X main.version=${version} -X main.cosaDir=${cosa_dir}


PREFIX ?= /usr
DESTDIR ?=
ARCH:=$(shell uname -m)

.PHONY: build
build: clean
	@mkdir -p bin
	@find cmd -iname *.go -exec go build  -i -ldflags "${ldflags}" -mod vendor -o bin {} \;

.PHONY: clean
clean:
	@go clean .
	@rm -rfv bin

.PHONY: install
install: clean build
	install -v -D -t $(DESTDIR)$(PREFIX)/bin bin/entry
