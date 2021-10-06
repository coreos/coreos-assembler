PREFIX ?= /usr
DESTDIR ?=

MANTLE_BINARIES := ore kola plume
KOLET_ARCHES := aarch64 ppc64le s390x x86_64

.PHONY: build
build: $(MANTLE_BINARIES) kolet

.PHONY: $(MANTLE_BINARIES) kolet
$(MANTLE_BINARIES) kolet:
	./build cmd/$(basename $@)

.PHONY: schema-update
schema-update:
	$(MAKE) -C ../tools schema

.PHONY: install
install:
	cd bin && install -D -t $(DESTDIR)$(PREFIX)/bin $(MANTLE_BINARIES)
	for arch in $(KOLET_ARCHES); do \
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
	@go mod tidy

.PHONY: clean
clean:
	@rm -rfv bin
