PREFIX ?= /usr
DESTDIR ?=

.PHONY: all mantle install

all: mantle

check:
	./tests/check.sh

mantle:
	cd mantle && ./build ore kola kolet

install:
	install -d $(DESTDIR)$(PREFIX)/bin
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-assembler
	install -d $(DESTDIR)$(PREFIX)/lib/coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler $$(find src/ -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/bin mantle/bin/{ore,kola}
	install -d $(DESTDIR)$(PREFIX)/lib/kola/amd64
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/amd64 mantle/bin/amd64/kolet
