PREFIX ?= /usr
DESTDIR ?=

.PHONY: all mantle install

all: mantle

mantle:
	cd mantle && ./build ore kola kolet

install:
	install -D -t $(DESTDIR)$(PREFIX)/bin coreos-assembler
	install -D -t $(DESTDIR)$(PREFIX)/lib/coreos-assembler deps.txt $$(find src/ -maxdepth 1 -type f)
	install -D -t $(DESTDIR)$(PREFIX)/bin mantle/bin/{ore,kola}
	install -D -m 0755 -t $(DESTDIR)$(PREFIX)/lib/kola/amd64 mantle/bin/amd64/kolet
